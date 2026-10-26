// Package adb runs adb: it finds the device a step drives, and reads its
// screen and acts on it.
package adb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Exec runs adb with args and returns what it wrote to its standard output.
type Exec func(ctx context.Context, args ...string) ([]byte, error)

// Command returns an Exec that runs the adb at path. An error carries what
// adb wrote to its standard error.
func Command(path string) Exec {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, path, args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		cmd.WaitDelay = 2 * time.Second
		out, err := cmd.Output()
		if err != nil {
			if msg := strings.TrimSpace(stderr.String()); msg != "" {
				return out, fmt.Errorf("adb %s: %w: %s", strings.Join(args, " "), err, msg)
			}
			return out, fmt.Errorf("adb %s: %w", strings.Join(args, " "), err)
		}
		return out, nil
	}
}

// Locator finds adb.
type Locator struct {
	LookPath func(string) (string, error)
	Getenv   func(string) string
	Exists   func(string) bool
	Home     string
	GOOS     string
}

// DefaultLocator looks where the SDK puts adb on this machine.
func DefaultLocator() Locator {
	home, _ := os.UserHomeDir()
	return Locator{
		LookPath: exec.LookPath,
		Getenv:   os.Getenv,
		Exists: func(p string) bool {
			fi, err := os.Stat(p)
			return err == nil && !fi.IsDir()
		},
		Home: home,
		GOOS: runtime.GOOS,
	}
}

// Find returns the path of adb: the one on PATH, or else the one in the
// platform-tools of the SDK that ANDROID_HOME or ANDROID_SDK_ROOT name, or
// that Android Studio installs.
func (l Locator) Find() (string, error) {
	if p, err := l.LookPath("adb"); err == nil {
		return p, nil
	}
	var sdks []string
	for _, env := range []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"} {
		if v := l.Getenv(env); v != "" {
			sdks = append(sdks, v)
		}
	}
	if l.Home != "" {
		if l.GOOS == "darwin" {
			sdks = append(sdks, filepath.Join(l.Home, "Library", "Android", "sdk"))
		} else {
			sdks = append(sdks, filepath.Join(l.Home, "Android", "Sdk"))
		}
	}
	for _, sdk := range sdks {
		p := filepath.Join(sdk, "platform-tools", "adb")
		if l.Exists(p) {
			return p, nil
		}
	}
	return "", errors.New("adb is not on PATH, nor in the platform-tools of ANDROID_HOME: install the Android SDK Platform-Tools, such as with sdkmanager platform-tools, and put it on PATH")
}

// Info is a device as adb devices lists it.
type Info struct {
	Serial string
	// State is device for one adb can drive, and offline, unauthorized
	// and so on for one it cannot.
	State string
	Model string
}

// Devices lists the devices adb knows.
func Devices(ctx context.Context, run Exec) ([]Info, error) {
	out, err := run(ctx, "devices", "-l")
	if err != nil {
		return nil, err
	}
	return parseDevices(string(out)), nil
}

func parseDevices(out string) []Info {
	var list []Info
	for line := range strings.Lines(out) {
		f := strings.Fields(line)
		if len(f) < 2 || strings.HasPrefix(line, "*") || strings.HasPrefix(line, "List of devices") {
			continue
		}
		d := Info{Serial: f[0], State: f[1]}
		for _, kv := range f[2:] {
			if v, ok := strings.CutPrefix(kv, "model:"); ok {
				d.Model = v
			}
		}
		list = append(list, d)
	}
	return list
}

// Resolve picks the device a step drives: the one named by its serial or
// model, or the one device adb can drive when want is empty.
func Resolve(list []Info, want string) (Info, error) {
	if want == "" {
		var ready []Info
		for _, d := range list {
			if d.State == "device" {
				ready = append(ready, d)
			}
		}
		switch len(ready) {
		case 0:
			if len(list) > 0 {
				return Info{}, notReady(list[0])
			}
			return Info{}, errors.New("adb lists no device: start an emulator, such as with emulator -avd <name>, or connect a device with USB debugging on")
		case 1:
			return ready[0], nil
		}
		return Info{}, fmt.Errorf("adb lists %d devices: give device, one of %s", len(ready), serials(ready))
	}
	for _, d := range list {
		if d.Serial == want || d.Model == want {
			if d.State != "device" {
				return Info{}, notReady(d)
			}
			return d, nil
		}
	}
	return Info{}, fmt.Errorf("adb lists no device %q; the devices are %s", want, serials(list))
}

func notReady(d Info) error {
	switch d.State {
	case "unauthorized":
		return fmt.Errorf("the device %s is unauthorized: allow USB debugging in the prompt on the device", d.Serial)
	case "offline":
		return fmt.Errorf("the device %s is offline: wait for it to boot, such as with adb -s %s wait-for-device", d.Serial, d.Serial)
	}
	return fmt.Errorf("the device %s is %s, not ready for adb", d.Serial, d.State)
}

func serials(list []Info) string {
	if len(list) == 0 {
		return "none"
	}
	s := make([]string, len(list))
	for i, d := range list {
		s[i] = d.Serial
		if d.Model != "" {
			s[i] += " (" + d.Model + ")"
		}
	}
	return strings.Join(s, ", ")
}
