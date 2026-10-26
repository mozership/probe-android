package adb

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// fakeAdb answers adb with what replies holds for the arguments after the
// serial, by their first words, and keeps the calls.
type fakeAdb struct {
	calls   [][]string
	replies map[string]string
	errs    map[string]error
}

func (f *fakeAdb) run(ctx context.Context, args ...string) ([]byte, error) {
	f.calls = append(f.calls, args)
	key := strings.Join(args[2:], " ")
	for prefix, err := range f.errs {
		if strings.HasPrefix(key, prefix) {
			return nil, err
		}
	}
	// The longest prefix wins, so that ime list -s -a and ime list -s
	// answer apart.
	best := ""
	for prefix := range f.replies {
		if strings.HasPrefix(key, prefix) && len(prefix) > len(best) {
			best = prefix
		}
	}
	if best != "" {
		return []byte(f.replies[best]), nil
	}
	return nil, nil
}

func device(f *fakeAdb) Device {
	return Device{Serial: "emulator-5554", Run: f.run}
}

func TestDump(t *testing.T) {
	xml := `<?xml version='1.0' encoding='UTF-8' standalone='yes' ?><hierarchy rotation="0"></hierarchy>`
	f := &fakeAdb{replies: map[string]string{"exec-out uiautomator dump": "UI hierchary dumped to: /data/local/tmp/x.xml\n" + xml}}
	got, err := device(f).Dump(context.Background())
	if err != nil || string(got) != xml {
		t.Errorf("Dump = %q, %v", got, err)
	}
	call := f.calls[0]
	if call[0] != "-s" || call[1] != "emulator-5554" || call[2] != "exec-out" {
		t.Errorf("adb %v", call)
	}
	if !strings.Contains(call[3], "&& cat ") || !strings.Contains(call[3], "rm -f ") {
		t.Errorf("the dump does not read the file and remove it: %q", call[3])
	}
}

func TestDumpNotReady(t *testing.T) {
	f := &fakeAdb{replies: map[string]string{"exec-out uiautomator dump": "ERROR: could not get idle state.\n"}}
	_, err := device(f).Dump(context.Background())
	var nr *NotReadyError
	if !errors.As(err, &nr) || nr.Output != "ERROR: could not get idle state." {
		t.Errorf("Dump = %v, want a NotReadyError with what uiautomator said", err)
	}
	if (&NotReadyError{}).Error() != "uiautomator dumped nothing" {
		t.Error("an empty dump says nothing")
	}

	f = &fakeAdb{errs: map[string]error{"exec-out": errors.New("adb: device offline")}}
	if _, err := device(f).Dump(context.Background()); err == nil || errors.As(err, &nr) {
		t.Errorf("Dump = %v, want adb's error as it is", err)
	}
}

func TestInputText(t *testing.T) {
	tests := map[string]string{
		"hello":         "'hello'",
		"Wi-Fi network": "'Wi-Fi%snetwork'",
		"it's":          `'it'\''s'`,
		`a"b$c;d&e|f`:   `'a"b$c;d&e|f'`,
		"":              "''",
	}
	for in, want := range tests {
		got, err := InputText(in)
		if err != nil || got != want {
			t.Errorf("InputText(%q) = %q, %v, want %q", in, got, err, want)
		}
	}
	for in, want := range map[string]string{
		"100%sure": "reads %s as a space",
		"こんにちは":    "printable ASCII alone",
		"a\nb":     "printable ASCII alone",
	} {
		if _, err := InputText(in); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("InputText(%q) = %v, want an error that says %q", in, err, want)
		}
	}
}

func TestActions(t *testing.T) {
	ctx := context.Background()
	f := &fakeAdb{}
	d := device(f)
	if err := d.Tap(ctx, 450, 570); err != nil {
		t.Fatal(err)
	}
	if err := d.Type(ctx, "a b"); err != nil {
		t.Fatal(err)
	}
	if err := d.Key(ctx, "KEYCODE_BACK"); err != nil {
		t.Fatal(err)
	}
	if err := d.ForceStop(ctx, "com.example"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"-s", "emulator-5554", "shell", "input", "tap", "450", "570"},
		{"-s", "emulator-5554", "shell", "input", "text", "'a%sb'"},
		{"-s", "emulator-5554", "shell", "input", "keyevent", "KEYCODE_BACK"},
		{"-s", "emulator-5554", "shell", "am", "force-stop", "com.example"},
	}
	if !reflect.DeepEqual(f.calls, want) {
		t.Errorf("calls = %v, want %v", f.calls, want)
	}
	if err := d.Type(ctx, "日本"); err == nil || len(f.calls) != 4 {
		t.Errorf("Type of text input text cannot type = %v, after %d calls", err, len(f.calls))
	}
}

func TestScreenshot(t *testing.T) {
	png := "\x89PNG\r\n\x1a\nrest"
	f := &fakeAdb{replies: map[string]string{"exec-out screencap -p": png}}
	got, err := device(f).Screenshot(context.Background())
	if err != nil || string(got) != png {
		t.Errorf("Screenshot = %q, %v", got, err)
	}
	f = &fakeAdb{replies: map[string]string{"exec-out screencap -p": "error: no display"}}
	if _, err := device(f).Screenshot(context.Background()); err == nil || !strings.Contains(err.Error(), "error: no display") {
		t.Errorf("Screenshot of what is not a PNG = %v", err)
	}
}

func TestLauncherActivity(t *testing.T) {
	f := &fakeAdb{replies: map[string]string{"shell cmd package resolve-activity": "priority=0 preferredOrder=0 match=0x108000 specificIndex=-1 isDefault=false\ncom.android.settings/.Settings\n"}}
	got, err := device(f).LauncherActivity(context.Background(), "com.android.settings")
	if err != nil || got != "com.android.settings/.Settings" {
		t.Errorf("LauncherActivity = %q, %v", got, err)
	}
	want := []string{"-s", "emulator-5554", "shell", "cmd", "package", "resolve-activity", "--brief",
		"-a", "android.intent.action.MAIN", "-c", "android.intent.category.LAUNCHER", "com.android.settings"}
	if !reflect.DeepEqual(f.calls[0], want) {
		t.Errorf("adb %v", f.calls[0])
	}

	f = &fakeAdb{replies: map[string]string{"shell cmd package resolve-activity": "No activity found\n"}}
	if _, err := device(f).LauncherActivity(context.Background(), "com.example"); err == nil || !strings.Contains(err.Error(), "is it installed?") {
		t.Errorf("LauncherActivity of an app not installed = %v", err)
	}
}

func TestStart(t *testing.T) {
	ctx := context.Background()
	f := &fakeAdb{replies: map[string]string{"shell am start": "Starting: Intent { ... }\nStatus: ok\n"}}
	d := device(f)
	if err := d.Start(ctx, "com.example/.Main", true); err != nil {
		t.Fatal(err)
	}
	if err := d.Start(ctx, "com.example/.Main", false); err != nil {
		t.Fatal(err)
	}
	fresh := strings.Join(f.calls[0][2:], " ")
	if fresh != "shell am start -W -S -a android.intent.action.MAIN -c android.intent.category.LAUNCHER -n com.example/.Main" {
		t.Errorf("a fresh start ran adb %s", fresh)
	}
	if again := strings.Join(f.calls[1][2:], " "); strings.Contains(again, "-S") {
		t.Errorf("a start of a running app stopped it: adb %s", again)
	}

	f = &fakeAdb{replies: map[string]string{"shell am start": "Starting: Intent { ... }\nError: Activity class {com.example/.Nope} does not exist.\n"}}
	if err := device(f).Start(ctx, "com.example/.Nope", true); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("Start = %v, want the error am printed", err)
	}
}
