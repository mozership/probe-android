package android

import (
	"context"
	"errors"
	"fmt"
	"os"
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
}

var _ device = adb.Device{}

// pollInterval is the pause between dumps of a screen that does not have
// the element yet. A dump itself takes most of a second.
var pollInterval = 250 * time.Millisecond

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
func runActions(ctx context.Context, d device, req *Req, res *Res) error {
	ctx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	for i, a := range req.Actions {
		if err := runAction(ctx, d, req.App, a, res); err != nil {
			if ctx.Err() != nil && !errors.Is(err, errNoElement) {
				err = fmt.Errorf("the step's timeout ran out: %w", err)
			}
			return fmt.Errorf("actions[%d] %s: %w", i, a.Name, err)
		}
	}
	return nil
}

func runAction(ctx context.Context, d device, app string, a *Action, res *Res) error {
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
			return d.Type(ctx, a.Value)
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
