package adb

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Device is a device adb drives.
type Device struct {
	Serial string
	Run    Exec
}

func (d Device) run(ctx context.Context, args ...string) ([]byte, error) {
	return d.Run(ctx, append([]string{"-s", d.Serial}, args...)...)
}

// NotReadyError is a dump uiautomator could not take, such as while the
// screen is still changing. A later dump may succeed.
type NotReadyError struct {
	Output string
}

func (e *NotReadyError) Error() string {
	if e.Output == "" {
		return "uiautomator dumped nothing"
	}
	return "uiautomator could not dump the screen: " + e.Output
}

// Dump returns the screen as uiautomator dump writes it.
func (d Device) Dump(ctx context.Context) ([]byte, error) {
	// The file is named by the process, so that steps that dump at once
	// do not read each other's.
	path := fmt.Sprintf("/data/local/tmp/probe-android-%d.xml", os.Getpid())
	out, err := d.run(ctx, "exec-out", fmt.Sprintf("uiautomator dump %s && cat %s; rm -f %s", path, path, path))
	if err != nil {
		return nil, err
	}
	i := bytes.Index(out, []byte("<?xml"))
	if i < 0 {
		i = bytes.Index(out, []byte("<hierarchy"))
	}
	if i < 0 {
		return nil, &NotReadyError{Output: strings.TrimSpace(string(out))}
	}
	return out[i:], nil
}

// Tap taps the screen at x, y.
func (d Device) Tap(ctx context.Context, x, y int) error {
	_, err := d.run(ctx, "shell", "input", "tap", strconv.Itoa(x), strconv.Itoa(y))
	return err
}

// Type types text into what has the focus.
func (d Device) Type(ctx context.Context, text string) error {
	arg, err := InputText(text)
	if err != nil {
		return err
	}
	_, err = d.run(ctx, "shell", "input", "text", arg)
	return err
}

// InputText is text as an argument of input text, in the device's shell.
// input text takes ASCII alone, and reads %s as a space.
func InputText(text string) (string, error) {
	if strings.Contains(text, "%s") {
		return "", fmt.Errorf("input text reads %%s as a space, so %q cannot be typed", text)
	}
	for _, r := range text {
		if r < ' ' || r > '~' {
			return "", fmt.Errorf("input text types printable ASCII alone, and %q has %q", text, r)
		}
	}
	s := strings.ReplaceAll(text, " ", "%s")
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'", nil
}

// Key sends a key event, such as KEYCODE_BACK.
func (d Device) Key(ctx context.Context, code string) error {
	_, err := d.run(ctx, "shell", "input", "keyevent", code)
	return err
}

var pngMagic = []byte("\x89PNG\r\n\x1a\n")

// Screenshot returns the screen as PNG.
func (d Device) Screenshot(ctx context.Context) ([]byte, error) {
	out, err := d.run(ctx, "exec-out", "screencap", "-p")
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(out, pngMagic) {
		return nil, fmt.Errorf("screencap did not write a PNG: %q", truncate(string(out), 200))
	}
	return out, nil
}

// LauncherActivity returns the activity the launcher starts the app with,
// as package/class.
func (d Device) LauncherActivity(ctx context.Context, pkg string) (string, error) {
	out, err := d.run(ctx, "shell", "cmd", "package", "resolve-activity", "--brief",
		"-a", "android.intent.action.MAIN", "-c", "android.intent.category.LAUNCHER", pkg)
	if err != nil {
		return "", err
	}
	// It writes the activity last, after what matched, or says it found
	// none.
	fields := strings.Fields(string(out))
	if len(fields) > 0 {
		last := fields[len(fields)-1]
		if strings.HasPrefix(last, pkg+"/") {
			return last, nil
		}
	}
	return "", fmt.Errorf("%s has no activity the launcher starts: is it installed? adb -s %s install path/to/app.apk installs it", pkg, d.Serial)
}

// Start starts the activity, as the launcher does, and waits for it to
// be drawn. Fresh stops the app first; otherwise a running app is brought
// to the front as it was.
func (d Device) Start(ctx context.Context, activity string, fresh bool) error {
	args := []string{"shell", "am", "start", "-W"}
	if fresh {
		args = append(args, "-S")
	}
	args = append(args, "-a", "android.intent.action.MAIN", "-c", "android.intent.category.LAUNCHER", "-n", activity)
	out, err := d.run(ctx, args...)
	if err != nil {
		return err
	}
	// am start says Error and exits 0 on some versions of Android.
	for line := range strings.Lines(string(out)) {
		if strings.HasPrefix(line, "Error") {
			return fmt.Errorf("am start %s: %s", activity, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// ForceStop ends the app.
func (d Device) ForceStop(ctx context.Context, pkg string) error {
	_, err := d.run(ctx, "shell", "am", "force-stop", pkg)
	return err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
