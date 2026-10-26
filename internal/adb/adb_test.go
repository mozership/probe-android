package adb

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCommand(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "adb")
	script := "#!/bin/sh\nif [ \"$1\" = fail ]; then echo 'error: no devices' >&2; exit 1; fi\necho \"$@\"\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	run := Command(fake)
	out, err := run(context.Background(), "devices", "-l")
	if err != nil || string(out) != "devices -l\n" {
		t.Errorf("run = %q, %v", out, err)
	}
	_, err = run(context.Background(), "fail")
	if err == nil || !strings.Contains(err.Error(), "adb fail") || !strings.Contains(err.Error(), "error: no devices") {
		t.Errorf("a failed run returned %v, want an error with what adb wrote", err)
	}
}

func TestFind(t *testing.T) {
	notFound := func(string) (string, error) { return "", errors.New("not found") }
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	tests := []struct {
		name   string
		l      Locator
		want   string
		errors bool
	}{
		{
			name: "on PATH",
			l:    Locator{LookPath: func(string) (string, error) { return "/usr/bin/adb", nil }},
			want: "/usr/bin/adb",
		},
		{
			name: "ANDROID_HOME",
			l: Locator{LookPath: notFound, Getenv: env(map[string]string{"ANDROID_HOME": "/sdk", "ANDROID_SDK_ROOT": "/old"}),
				Exists: func(p string) bool { return p == "/sdk/platform-tools/adb" || p == "/old/platform-tools/adb" }},
			want: "/sdk/platform-tools/adb",
		},
		{
			name: "ANDROID_SDK_ROOT",
			l: Locator{LookPath: notFound, Getenv: env(map[string]string{"ANDROID_HOME": "/empty", "ANDROID_SDK_ROOT": "/old"}),
				Exists: func(p string) bool { return p == "/old/platform-tools/adb" }},
			want: "/old/platform-tools/adb",
		},
		{
			name: "Android Studio on macOS",
			l: Locator{LookPath: notFound, Getenv: env(nil), Home: "/Users/me", GOOS: "darwin",
				Exists: func(p string) bool { return p == "/Users/me/Library/Android/sdk/platform-tools/adb" }},
			want: "/Users/me/Library/Android/sdk/platform-tools/adb",
		},
		{
			name: "Android Studio on Linux",
			l: Locator{LookPath: notFound, Getenv: env(nil), Home: "/home/me", GOOS: "linux",
				Exists: func(p string) bool { return p == "/home/me/Android/Sdk/platform-tools/adb" }},
			want: "/home/me/Android/Sdk/platform-tools/adb",
		},
		{
			name:   "nowhere",
			l:      Locator{LookPath: notFound, Getenv: env(nil), Home: "/home/me", GOOS: "linux", Exists: func(string) bool { return false }},
			errors: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.l.Find()
			if tt.errors {
				if err == nil || !strings.Contains(err.Error(), "install the Android SDK Platform-Tools") {
					t.Errorf("Find = %q, %v, want an error that says how to install adb", got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("Find = %q, %v, want %q", got, err, tt.want)
			}
		})
	}
}

const devicesOut = `* daemon not running; starting now at tcp:5037
* daemon started successfully
List of devices attached
emulator-5554          device product:sdk_gphone64_x86_64 model:sdk_gphone64_x86_64 device:emu64xa transport_id:1
R58M123ABC             unauthorized usb:1-1 transport_id:2
emulator-5556          offline transport_id:3

`

func TestDevices(t *testing.T) {
	run := func(ctx context.Context, args ...string) ([]byte, error) {
		if !reflect.DeepEqual(args, []string{"devices", "-l"}) {
			t.Errorf("adb %v", args)
		}
		return []byte(devicesOut), nil
	}
	got, err := Devices(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}
	want := []Info{
		{Serial: "emulator-5554", State: "device", Model: "sdk_gphone64_x86_64"},
		{Serial: "R58M123ABC", State: "unauthorized"},
		{Serial: "emulator-5556", State: "offline"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Devices = %+v, want %+v", got, want)
	}

	_, err = Devices(context.Background(), func(context.Context, ...string) ([]byte, error) {
		return nil, errors.New("adb devices -l: exit status 1")
	})
	if err == nil {
		t.Error("Devices returned no error when adb failed")
	}
}

func TestResolve(t *testing.T) {
	list := parseDevices(devicesOut)
	tests := []struct {
		name string
		list []Info
		want string
		got  string
		err  string
	}{
		{name: "the one ready", list: list, got: "emulator-5554"},
		{name: "by serial", list: list, want: "emulator-5554", got: "emulator-5554"},
		{name: "by model", list: list, want: "sdk_gphone64_x86_64", got: "emulator-5554"},
		{name: "unauthorized", list: list, want: "R58M123ABC", err: "allow USB debugging"},
		{name: "offline", list: list, want: "emulator-5556", err: "wait-for-device"},
		{name: "other state", list: []Info{{Serial: "x", State: "recovery"}}, want: "x", err: "x is recovery"},
		{name: "unknown", list: list, want: "pixel", err: `no device "pixel"; the devices are emulator-5554 (sdk_gphone64_x86_64), R58M123ABC, emulator-5556`},
		{name: "none", err: "adb lists no device: start an emulator"},
		{name: "none ready", list: list[1:], err: "R58M123ABC is unauthorized"},
		{name: "two ready", list: []Info{{Serial: "a", State: "device"}, {Serial: "b", State: "device"}}, err: "adb lists 2 devices: give device, one of a, b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := Resolve(tt.list, tt.want)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Errorf("Resolve = %+v, %v, want an error that says %q", d, err, tt.err)
				}
				return
			}
			if err != nil || d.Serial != tt.got {
				t.Errorf("Resolve = %+v, %v, want %s", d, err, tt.got)
			}
		})
	}
}
