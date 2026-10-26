package android

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mozership/probe-android/internal/adb"
)

func init() {
	pollInterval = time.Millisecond
	imeSettle = 20 * time.Millisecond
}

const screenXML = `<?xml version='1.0' encoding='UTF-8' standalone='yes' ?><hierarchy rotation="0"><node class="android.widget.FrameLayout" package="com.android.settings" enabled="true" bounds="[0,0][1080,2400]"><node text="Network &amp; internet" resource-id="android:id/title" class="android.widget.TextView" enabled="true" bounds="[200,540][700,600]" /><node text="" resource-id="com.android.settings:id/search" class="android.widget.EditText" content-desc="Search settings" enabled="true" bounds="[0,100][1080,200]" /><node text="Hidden" resource-id="android:id/title" class="android.widget.TextView" enabled="true" bounds="[0,0][0,0]" /></node></hierarchy>`

// fakeDevice answers as adb would, and keeps what the actions did.
type fakeDevice struct {
	calls []string
	// dumps are returned in turn, the last of them again and again; an
	// entry of notReady is a dump uiautomator could not take.
	dumps   []string
	dumpErr error
	png     []byte
	fail    map[string]error

	// field, when edit is set, is the text of the one element of the
	// screen, an edit text that has the focus; password makes it one.
	edit     bool
	field    string
	password bool
	// noKeyboard is a device without ADBKeyBoard, and drop the number of
	// broadcasts it drops before it is up.
	noKeyboard bool
	drop       int
	restoreErr error
}

const notReady = "<not ready>"

func (f *fakeDevice) record(call string) error {
	f.calls = append(f.calls, call)
	name, _, _ := strings.Cut(call, " ")
	return f.fail[name]
}

func (f *fakeDevice) Dump(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.calls = append(f.calls, "dump")
	if f.dumpErr != nil {
		return nil, f.dumpErr
	}
	if f.edit {
		return fmt.Appendf(nil, `<hierarchy><node class="android.widget.EditText" resource-id="com.example:id/name" text=%q password="%v" focused="true" enabled="true" bounds="[0,100][1080,200]" /></hierarchy>`, f.field, f.password), nil
	}
	d := f.dumps[0]
	if len(f.dumps) > 1 {
		f.dumps = f.dumps[1:]
	}
	if d == notReady {
		return nil, &adb.NotReadyError{Output: "ERROR: could not get idle state."}
	}
	return []byte(d), nil
}

func (f *fakeDevice) Tap(ctx context.Context, x, y int) error {
	return f.record(fmt.Sprintf("tap %d %d", x, y))
}

func (f *fakeDevice) Type(ctx context.Context, text string) error {
	if err := f.record("type " + text); err != nil {
		return err
	}
	f.field += text
	return nil
}

func (f *fakeDevice) Key(ctx context.Context, code string) error {
	return f.record("key " + code)
}

func (f *fakeDevice) Screenshot(ctx context.Context) ([]byte, error) {
	return f.png, f.record("screenshot")
}

func (f *fakeDevice) LauncherActivity(ctx context.Context, pkg string) (string, error) {
	return pkg + "/.Main", f.record("resolve " + pkg)
}

func (f *fakeDevice) Start(ctx context.Context, activity string, fresh bool) error {
	return f.record(fmt.Sprintf("start %s fresh=%v", activity, fresh))
}

func (f *fakeDevice) ForceStop(ctx context.Context, pkg string) error {
	return f.record("force-stop " + pkg)
}

func (f *fakeDevice) UseADBKeyBoard(ctx context.Context) (func(context.Context) error, error) {
	if f.noKeyboard {
		return nil, adb.ErrNoADBKeyBoard
	}
	if err := f.record("use-adbkeyboard"); err != nil {
		return nil, err
	}
	return func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		f.calls = append(f.calls, "restore-ime")
		return f.restoreErr
	}, nil
}

func (f *fakeDevice) BroadcastText(ctx context.Context, text string) error {
	if err := f.record("broadcast " + text); err != nil {
		return err
	}
	if f.drop > 0 {
		f.drop--
		return nil
	}
	f.field += text
	return nil
}

func newRes() *Res {
	return &Res{Results: map[string]string{}, FilePaths: map[string]string{}}
}

func req(t *testing.T, timeout string, actions ...map[string]any) *Req {
	t.Helper()
	list := make([]any, len(actions))
	for i, a := range actions {
		list[i] = a
	}
	with := map[string]any{"app": "com.android.settings", "actions": list}
	if timeout != "" {
		with["timeout"] = timeout
	}
	r, err := parseReq(with)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRunActions(t *testing.T) {
	f := &fakeDevice{dumps: []string{screenXML}, png: []byte("\x89PNG fake")}
	r := req(t, "",
		map[string]any{"name": "launch"},
		map[string]any{"name": "activate"},
		map[string]any{"name": "tap", "selector": `text="Network & internet"`},
		map[string]any{"name": "type", "selector": "search", "value": "wifi"},
		map[string]any{"name": "back"},
		map[string]any{"name": "wait", "selector": "class=EditText"},
		map[string]any{"name": "text", "id": "title", "selector": "id=title"},
		map[string]any{"name": "text", "id": "search", "selector": "id=search"},
		map[string]any{"name": "tree"},
		map[string]any{"name": "screenshot", "id": "shot"},
		map[string]any{"name": "terminate"},
	)
	res := newRes()
	if err := runActions(context.Background(), f, r, res); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"resolve com.android.settings", "start com.android.settings/.Main fresh=true",
		"resolve com.android.settings", "start com.android.settings/.Main fresh=false",
		"dump", "tap 450 570",
		"dump", "tap 540 150", "type wifi",
		"key KEYCODE_BACK",
		"dump",
		"dump",
		"dump",
		"dump",
		"screenshot",
		"force-stop com.android.settings",
	}
	if !reflect.DeepEqual(f.calls, want) {
		t.Errorf("calls =\n%q\nwant\n%q", f.calls, want)
	}
	if got := res.Results["title"]; got != "Network & internet" {
		t.Errorf("results.title = %q", got)
	}
	if got := res.Results["search"]; got != "Search settings" {
		t.Errorf("results.search = %q, want the desc of an element with no text", got)
	}
	if got := res.Results["tree"]; !strings.Contains(got, `TextView id=android:id/title text="Network & internet" [200,540][700,600]`) {
		t.Errorf("results.tree = %q", got)
	}
	path := res.FilePaths["shot"]
	t.Cleanup(func() { _ = os.Remove(path) })
	if b, err := os.ReadFile(path); err != nil || string(b) != "\x89PNG fake" {
		t.Errorf("the screenshot at %q holds %q, %v", path, b, err)
	}
}

func TestFindWaitsForTheElement(t *testing.T) {
	empty := `<hierarchy><node class="android.widget.FrameLayout" bounds="[0,0][1080,2400]" /></hierarchy>`
	f := &fakeDevice{dumps: []string{notReady, empty, notReady, screenXML}}
	res := newRes()
	err := runActions(context.Background(), f, req(t, "", map[string]any{"name": "tap", "selector": "id=title"}), res)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.calls, ","); got != "dump,dump,dump,dump,tap 450 570" {
		t.Errorf("calls = %s, want dumps until the element is there", got)
	}
}

func TestFindRunsOutOfTime(t *testing.T) {
	f := &fakeDevice{dumps: []string{screenXML}}
	err := runActions(context.Background(), f, req(t, "100ms",
		map[string]any{"name": "launch"},
		map[string]any{"name": "tap", "selector": `text="No Such Row"`},
	), newRes())
	if err == nil || !strings.HasPrefix(err.Error(), `actions[1] tap: no element matches text="No Such Row" within 0.1s`) {
		t.Errorf("runActions = %v", err)
	}
	if strings.Contains(err.Error(), "timeout ran out") || strings.Contains(err.Error(), "last dump") {
		t.Errorf("the error says more than that the element is not there: %v", err)
	}
}

func TestFindSaysTheLastDumpFailed(t *testing.T) {
	f := &fakeDevice{dumps: []string{notReady}}
	err := runActions(context.Background(), f, req(t, "50ms", map[string]any{"name": "wait", "selector": "x"}), newRes())
	if err == nil || !strings.Contains(err.Error(), "no element matches id=x within") ||
		!strings.Contains(err.Error(), "the last dump failed: uiautomator could not dump the screen: ERROR: could not get idle state.") {
		t.Errorf("runActions = %v", err)
	}
}

func TestFindStopsAtAnAdbError(t *testing.T) {
	f := &fakeDevice{dumpErr: errors.New("adb: device offline")}
	start := time.Now()
	err := runActions(context.Background(), f, req(t, "10s", map[string]any{"name": "wait", "selector": "x"}), newRes())
	if err == nil || err.Error() != "actions[0] wait: adb: device offline" {
		t.Errorf("runActions = %v", err)
	}
	if time.Since(start) > time.Second {
		t.Error("an adb error was waited out")
	}
}

func TestTapOffTheScreen(t *testing.T) {
	f := &fakeDevice{dumps: []string{screenXML}}
	err := runActions(context.Background(), f, req(t, "", map[string]any{"name": "tap", "selector": "text=Hidden"}), newRes())
	if err == nil || !strings.Contains(err.Error(), "the element text=Hidden is not on the screen: its bounds are [0,0][0,0]") {
		t.Errorf("runActions = %v", err)
	}
}

func TestTreeWaitsForADump(t *testing.T) {
	f := &fakeDevice{dumps: []string{notReady, screenXML}}
	res := newRes()
	if err := runActions(context.Background(), f, req(t, "", map[string]any{"name": "tree", "id": "t"}), res); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Results["t"], "FrameLayout [0,0][1080,2400]\n") {
		t.Errorf("results.t = %q", res.Results["t"])
	}

	f = &fakeDevice{dumps: []string{notReady}}
	err := runActions(context.Background(), f, req(t, "50ms", map[string]any{"name": "tree"}), newRes())
	if err == nil || !strings.Contains(err.Error(), "the step's timeout ran out: uiautomator could not dump the screen") {
		t.Errorf("runActions = %v", err)
	}
}

func TestRunActionsStopsAtTheFirstFailure(t *testing.T) {
	f := &fakeDevice{dumps: []string{screenXML}, fail: map[string]error{"resolve": errors.New("com.example has no activity the launcher starts")}}
	err := runActions(context.Background(), f, req(t, "",
		map[string]any{"name": "launch"},
		map[string]any{"name": "terminate"},
	), newRes())
	if err == nil || err.Error() != "actions[0] launch: com.example has no activity the launcher starts" {
		t.Errorf("runActions = %v", err)
	}
	if len(f.calls) != 1 {
		t.Errorf("calls = %q, want none after the failure", f.calls)
	}
}

// typeCalls are the calls but the dumps, which come as often as the
// screen is read.
func typeCalls(f *fakeDevice) []string {
	var s []string
	for _, c := range f.calls {
		if c != "dump" {
			s = append(s, c)
		}
	}
	return s
}

func TestTypeWithADBKeyBoard(t *testing.T) {
	f := &fakeDevice{edit: true, field: "Wi-Fi ", drop: 2}
	res := newRes()
	err := runActions(context.Background(), f, req(t, "",
		map[string]any{"name": "type", "selector": "name", "value": "日本"},
		map[string]any{"name": "type", "selector": "name", "value": " ok"},
		map[string]any{"name": "type", "selector": "name", "value": "語"},
		map[string]any{"name": "text", "id": "name", "selector": "name"},
	), res)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"use-adbkeyboard",
		"tap 540 150", "broadcast 日本", "broadcast 日本", "broadcast 日本",
		"tap 540 150", "type  ok",
		"tap 540 150", "broadcast 語",
		"restore-ime",
	}
	if got := typeCalls(f); !reflect.DeepEqual(got, want) {
		t.Errorf("calls =\n%q\nwant\n%q", got, want)
	}
	if got := res.Results["name"]; got != "Wi-Fi 日本 ok語" {
		t.Errorf("the field has %q, want each text typed once", got)
	}
}

func TestTypeASCIIDoesNotSwitch(t *testing.T) {
	f := &fakeDevice{edit: true}
	if err := runActions(context.Background(), f, req(t, "", map[string]any{"name": "type", "selector": "name", "value": "hello"}), newRes()); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.calls {
		if c == "use-adbkeyboard" || c == "restore-ime" {
			t.Errorf("ASCII text switched the input method: %q", f.calls)
		}
	}
}

func TestTypeWithoutADBKeyBoard(t *testing.T) {
	f := &fakeDevice{edit: true, noKeyboard: true}
	err := runActions(context.Background(), f, req(t, "",
		map[string]any{"name": "launch"},
		map[string]any{"name": "type", "selector": "name", "value": "hello"},
		map[string]any{"name": "type", "selector": "name", "value": "100%sure"},
	), newRes())
	if err == nil || !errors.Is(err, adb.ErrNoADBKeyBoard) ||
		!strings.HasPrefix(err.Error(), `actions[2] type: input text types printable ASCII alone, so "100%sure" needs ADBKeyBoard: ADBKeyBoard is not installed`) {
		t.Errorf("runActions = %v", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("calls = %q, want the step to fail before its first action", f.calls)
	}
}

func TestSwitchToADBKeyBoardBeforeTheFirstAction(t *testing.T) {
	f := &fakeDevice{edit: true}
	err := runActions(context.Background(), f, req(t, "",
		map[string]any{"name": "launch"},
		map[string]any{"name": "type", "selector": "name", "value": "日本"},
		map[string]any{"name": "type", "selector": "name", "value": "語"},
	), newRes())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"use-adbkeyboard",
		"resolve com.android.settings", "start com.android.settings/.Main fresh=true",
		"tap 540 150", "broadcast 日本",
		"tap 540 150", "broadcast 語",
		"restore-ime",
	}
	if got := typeCalls(f); !reflect.DeepEqual(got, want) {
		t.Errorf("calls =\n%q\nwant\n%q", got, want)
	}
}

func TestTypeWithADBKeyBoardThatNeverTypes(t *testing.T) {
	f := &fakeDevice{edit: true, field: "x", drop: 1 << 30}
	err := runActions(context.Background(), f, req(t, "100ms", map[string]any{"name": "type", "selector": "name", "value": "日本"}), newRes())
	if err == nil || !strings.Contains(err.Error(), `ADBKeyBoard did not type "日本": EditText id=com.example:id/name text="x" [0,100][1080,200] focused had the focus`) {
		t.Errorf("runActions = %v", err)
	}
	if got := typeCalls(f); got[len(got)-1] != "restore-ime" {
		t.Errorf("the input method was not put back after the failure: %q", got)
	}
}

func TestTypePasswordWithADBKeyBoard(t *testing.T) {
	f := &fakeDevice{edit: true, password: true}
	if err := runActions(context.Background(), f, req(t, "", map[string]any{"name": "type", "selector": "name", "value": "パス"}), newRes()); err != nil {
		t.Fatal(err)
	}
	want := []string{"use-adbkeyboard", "tap 540 150", "broadcast パス", "restore-ime"}
	if got := typeCalls(f); !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %q, want one broadcast, as a password field shows nothing to check", got)
	}
}

func TestRestoreIMEError(t *testing.T) {
	f := &fakeDevice{edit: true, restoreErr: errors.New("ime set: Unknown input method")}
	err := runActions(context.Background(), f, req(t, "", map[string]any{"name": "type", "selector": "name", "value": "日本"}), newRes())
	if err == nil || err.Error() != "putting the input method back after ADBKeyBoard: ime set: Unknown input method" {
		t.Errorf("runActions = %v", err)
	}

	// The error of the action that failed comes first.
	f = &fakeDevice{edit: true, restoreErr: errors.New("ime set: Unknown input method"), fail: map[string]error{"tap": errors.New("adb: device offline")}}
	err = runActions(context.Background(), f, req(t, "",
		map[string]any{"name": "type", "selector": "name", "value": "日本"},
		map[string]any{"name": "type", "selector": "name", "value": "語"},
	), newRes())
	if err == nil || !strings.Contains(err.Error(), "device offline") {
		t.Errorf("runActions = %v", err)
	}
}

func TestRestoreIMEAfterTheTimeout(t *testing.T) {
	f := &fakeDevice{edit: true, drop: 1 << 30}
	_ = runActions(context.Background(), f, req(t, "50ms", map[string]any{"name": "type", "selector": "name", "value": "日本"}), newRes())
	if got := typeCalls(f); got[len(got)-1] != "restore-ime" {
		t.Errorf("the input method was not put back once the step's timeout ran out: %q", got)
	}
}
