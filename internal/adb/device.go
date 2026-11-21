package adb

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
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

// dumperJar is the dex of dumper/ProbeAndroidDump.java, which
// dumper/build.sh builds. It reads the screen as uiautomator dump does, but
// reads one that does not go idle, as one with a clock, rather than fail.
//
//go:embed dumper.jar
var dumperJar []byte

// dumperPath is where the jar goes on the device. It is named by its
// digest, so that a device keeps the jar of each version of the action
// apart, and is given the jar again only when it does not have it.
var dumperPath = fmt.Sprintf("/data/local/tmp/probe-android-dumper-%x.jar", sha256.Sum256(dumperJar))

// noDumper is what the device says when it does not have the jar.
const noDumper = "probe-android: no dumper"

// Dump returns the screen as uiautomator dump writes it. It reads it with
// the jar of the action, which it pushes to the device first when the device
// does not have it, and with uiautomator dump when the jar cannot.
func (d Device) Dump(ctx context.Context) ([]byte, error) {
	// The file is named by the process, so that steps that dump at once
	// do not read each other's.
	path := fmt.Sprintf("/data/local/tmp/probe-android-%d.xml", os.Getpid())
	jar := fmt.Sprintf("if [ -f %[1]s ]; then CLASSPATH=/system/framework/uiautomator.jar:%[1]s app_process /system/bin ProbeAndroidDump %[2]s && cat %[2]s; else echo %[3]q; fi; rm -f %[2]s", dumperPath, path, noDumper)
	out, err := d.run(ctx, "exec-out", jar)
	if err == nil && bytes.Contains(out, []byte(noDumper)) {
		if err := d.push(ctx, dumperJar, dumperPath); err != nil {
			return nil, err
		}
		out, err = d.run(ctx, "exec-out", jar)
	}
	if err != nil {
		return nil, err
	}
	if xml := xmlOf(out); xml != nil {
		return xml, nil
	}
	out, err = d.run(ctx, "exec-out", fmt.Sprintf("uiautomator dump %s && cat %s; rm -f %s", path, path, path))
	if err != nil {
		return nil, err
	}
	if xml := xmlOf(out); xml != nil {
		return xml, nil
	}
	return nil, &NotReadyError{Output: strings.TrimSpace(string(out))}
}

// xmlOf returns the XML in what a dump wrote, or nil when there is none.
func xmlOf(out []byte) []byte {
	i := bytes.Index(out, []byte("<?xml"))
	if i < 0 {
		i = bytes.Index(out, []byte("<hierarchy"))
	}
	if i < 0 {
		return nil
	}
	return out[i:]
}

// push writes b to path on the device.
func (d Device) push(ctx context.Context, b []byte, path string) error {
	local, err := os.CreateTemp("", "probe-android-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(local.Name()) }()
	if _, err := local.Write(b); err != nil {
		_ = local.Close()
		return err
	}
	if err := local.Close(); err != nil {
		return err
	}
	_, err = d.run(ctx, "push", local.Name(), path)
	return err
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
