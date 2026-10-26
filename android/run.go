package android

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mozership/probe-android/internal/adb"
	"github.com/mozership/probe-android/internal/uitree"
)

// device is what the actions take adb for.
type device interface {
	Dump(ctx context.Context) ([]byte, error)
	Tap(ctx context.Context, x, y int) error
	Type(ctx context.Context, text string) error
	Key(ctx context.Context, code string) error
	Screenshot(ctx context.Context) ([]byte, error)
	LauncherActivity(ctx context.Context, pkg string) (string, error)
	Start(ctx context.Context, activity string, fresh bool) error
	ForceStop(ctx context.Context, pkg string) error
	UseADBKeyBoard(ctx context.Context) (restore func(context.Context) error, err error)
	BroadcastText(ctx context.Context, text string) error
}

var _ device = adb.Device{}

// pollInterval is the pause between dumps of a screen that does not have
// the element yet. A dump itself takes most of a second.
var pollInterval = 250 * time.Millisecond

// imeSettle is how long a text ADBKeyBoard was sent is given to show in
// the focused element before it is sent again.
var imeSettle = 3 * time.Second

// restoreTimeout bounds putting the input method back, after the step's
// timeout may have run out.
const restoreTimeout = 10 * time.Second

// Res is what a step returns in res.
type Res struct {
	// Code is 0 when every action succeeded, and 1 otherwise.
	Code int `map:"code"`
	// Results are what text and tree returned, by the key of each.
	Results map[string]string `map:"results"`
	// FilePaths are the files screenshot wrote, by the key of each.
	FilePaths map[string]string `map:"filepaths"`
	// Device is the serial of the device the actions ran on.
	Device string `map:"device"`
}

// Result is the result of a step.
type Result struct {
	Req    Req           `map:"req"`
	Res    Res           `map:"res"`
	RT     time.Duration `map:"rt"`
	Status int           `map:"status"`
}

// runActions takes the actions of req in order on d, and stops at the
// first that fails. Each action that waits for an element waits for what
// is left of the step's timeout.
func runActions(ctx context.Context, d device, req *Req, res *Res) (err error) {
	ctx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	s := &session{d: d, app: req.App}
	defer func() {
		if s.restoreIME == nil {
			return
		}
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), restoreTimeout)
		defer cancel()
		if rerr := s.restoreIME(rctx); rerr != nil && err == nil {
			err = fmt.Errorf("putting the input method back after ADBKeyBoard: %w", rerr)
		}
	}()
	// A step that types text input text cannot switches to ADBKeyBoard
	// before its first action, not as it types, so that the input method
	// does not change under the screen the step is on.
	for i, a := range req.Actions {
		if a.Name != "type" || adb.CanInputText(a.Value) {
			continue
		}
		restore, err := d.UseADBKeyBoard(ctx)
		if err != nil {
			return fmt.Errorf("actions[%d] %s: input text types printable ASCII alone, so %q needs ADBKeyBoard: %w", i, a.Name, a.Value, err)
		}
		s.restoreIME = restore
		break
	}
	for i, a := range req.Actions {
		if err := s.run(ctx, a, res); err != nil {
			if ctx.Err() != nil && !errors.Is(err, errNoElement) {
				err = fmt.Errorf("the step's timeout ran out: %w", err)
			}
			return fmt.Errorf("actions[%d] %s: %w", i, a.Name, err)
		}
	}
	return nil
}

// session is what the actions of a step share.
type session struct {
	d   device
	app string
	// restoreIME puts the input method back once the step is over, when
	// the step switched it to ADBKeyBoard.
	restoreIME func(context.Context) error
}

func (s *session) run(ctx context.Context, a *Action, res *Res) error {
	d, app := s.d, s.app
	switch a.Name {
	case "launch", "activate":
		activity, err := d.LauncherActivity(ctx, app)
		if err != nil {
			return err
		}
		return d.Start(ctx, activity, a.Name == "launch")
	case "terminate":
		return d.ForceStop(ctx, app)
	case "back":
		return d.Key(ctx, "KEYCODE_BACK")
	case "tap", "type":
		n, err := find(ctx, d, a.selector)
		if err != nil {
			return err
		}
		if n.Bounds.Empty() {
			return fmt.Errorf("the element %s is not on the screen: its bounds are %s", a.selector, n.Bounds)
		}
		x, y := n.Bounds.Center()
		if err := d.Tap(ctx, x, y); err != nil {
			return err
		}
		if a.Name == "type" {
			return s.typeText(ctx, a.Value)
		}
		return nil
	case "wait":
		_, err := find(ctx, d, a.selector)
		return err
	case "text":
		n, err := find(ctx, d, a.selector)
		if err != nil {
			return err
		}
		text := n.Text
		if text == "" {
			text = n.Desc
		}
		res.Results[a.key()] = text
		return nil
	case "tree":
		root, err := dump(ctx, d)
		if err != nil {
			return err
		}
		res.Results[a.key()] = uitree.Format(root)
		return nil
	case "screenshot":
		png, err := d.Screenshot(ctx)
		if err != nil {
			return err
		}
		path, err := writeTemp("probe-android-screenshot-*.png", png)
		if err != nil {
			return err
		}
		res.FilePaths[a.key()] = path
		return nil
	}
	return fmt.Errorf("no action is named %q", a.Name)
}

// typeText types text into what has the focus: with input text, or with
// ADBKeyBoard when input text cannot type it.
func (s *session) typeText(ctx context.Context, text string) error {
	if adb.CanInputText(text) {
		return s.d.Type(ctx, text)
	}
	return broadcastText(ctx, s.d, text)
}

// broadcastText has ADBKeyBoard type text, and reads the screen until the
// focused element has it. ADBKeyBoard drops what it is sent before it is
// up and bound to the element, so the text is sent again when it does not
// show; it is typed whole or not at all, so it is never typed twice.
func broadcastText(ctx context.Context, d device, text string) error {
	root, err := dump(ctx, d)
	if err != nil {
		return err
	}
	before := ""
	if f := uitree.Focused(root); f != nil {
		if f.Password {
			// A password field shows no text to check.
			return d.BroadcastText(ctx, text)
		}
		before = f.Text
	}
	// focus is what had the focus at the last read, for the message when
	// the text does not show.
	focus := "no element"
	for {
		if err := d.BroadcastText(ctx, text); err != nil {
			return err
		}
		deadline := time.Now().Add(imeSettle)
		for time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				return fmt.Errorf("ADBKeyBoard did not type %q: %s had the focus", text, focus)
			case <-time.After(pollInterval):
			}
			root, err := dumpOnce(ctx, d)
			if err != nil {
				if isNotReady(err) || ctx.Err() != nil {
					continue
				}
				return err
			}
			f := uitree.Focused(root)
			if f == nil {
				focus = "no element"
				continue
			}
			focus = f.String()
			if f.Text != before && strings.Contains(f.Text, text) {
				return nil
			}
		}
	}
}

var errNoElement = errors.New("no element matches")

// find dumps the screen until it has the element s names, for what is left
// of ctx.
func find(ctx context.Context, d device, s Selector) (*uitree.Node, error) {
	start := time.Now()
	var last error
	for {
		root, err := dumpOnce(ctx, d)
		switch {
		case err == nil:
			n, err := uitree.Find(root, s.Match, s.Index)
			if err == nil {
				return n, nil
			}
			last = nil
		case ctx.Err() != nil:
		case isNotReady(err):
			last = err
		default:
			return nil, err
		}
		select {
		case <-ctx.Done():
			msg := fmt.Errorf("%w %s within %.1fs", errNoElement, s, time.Since(start).Seconds())
			if last != nil {
				return nil, fmt.Errorf("%w; the last dump failed: %v", msg, last)
			}
			return nil, msg
		case <-time.After(pollInterval):
		}
	}
}

// dump dumps the screen, again while uiautomator cannot, for what is left
// of ctx.
func dump(ctx context.Context, d device) (*uitree.Node, error) {
	var last error
	for {
		root, err := dumpOnce(ctx, d)
		switch {
		case err == nil:
			return root, nil
		case ctx.Err() != nil && last != nil:
			return nil, last
		case !isNotReady(err):
			return nil, err
		}
		last = err
		select {
		case <-ctx.Done():
			return nil, last
		case <-time.After(pollInterval):
		}
	}
}

func dumpOnce(ctx context.Context, d device) (*uitree.Node, error) {
	b, err := d.Dump(ctx)
	if err != nil {
		return nil, err
	}
	return uitree.Parse(b)
}

func isNotReady(err error) bool {
	var nr *adb.NotReadyError
	return errors.As(err, &nr)
}

func writeTemp(pattern string, b []byte) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return "", err
	}
	return f.Name(), f.Close()
}
