package adb

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ADBKeyBoard is the input method of ADBKeyBoard
// (https://github.com/senzhk/ADBKeyBoard), which types the text a
// broadcast gives it, in any script, where input text types ASCII alone.
const ADBKeyBoard = "com.android.adbkeyboard/.AdbIME"

// ErrNoADBKeyBoard is what UseADBKeyBoard returns when the device does not
// have ADBKeyBoard.
var ErrNoADBKeyBoard = errors.New("ADBKeyBoard is not installed: download keyboardservice-debug.apk from https://github.com/senzhk/ADBKeyBoard/releases and install it with adb install, and the action switches to it to type such text")

// CanInputText reports whether input text types text as it is: printable
// ASCII with no %s, which it reads as a space.
func CanInputText(text string) bool {
	_, err := InputText(text)
	return err == nil
}

// UseADBKeyBoard makes ADBKeyBoard the input method, enabling it when it is
// not, and returns what puts the input method back as it was.
func (d Device) UseADBKeyBoard(ctx context.Context) (restore func(context.Context) error, err error) {
	all, err := d.imeList(ctx, true)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(all, ADBKeyBoard) {
		return nil, ErrNoADBKeyBoard
	}
	enabled, err := d.imeList(ctx, false)
	if err != nil {
		return nil, err
	}
	wasEnabled := slices.Contains(enabled, ADBKeyBoard)
	prev, err := d.currentIME(ctx)
	if err != nil {
		return nil, err
	}
	if prev == ADBKeyBoard {
		return func(context.Context) error { return nil }, nil
	}
	if !wasEnabled {
		if err := d.ime(ctx, "enable", ADBKeyBoard); err != nil {
			return nil, err
		}
	}
	restore = func(ctx context.Context) error {
		var errs []error
		if prev != "" {
			errs = append(errs, d.ime(ctx, "set", prev))
		} else {
			errs = append(errs, d.ime(ctx, "reset"))
		}
		if !wasEnabled {
			errs = append(errs, d.ime(ctx, "disable", ADBKeyBoard))
		}
		return errors.Join(errs...)
	}
	if err := d.ime(ctx, "set", ADBKeyBoard); err != nil {
		return nil, errors.Join(err, restore(ctx))
	}
	return restore, nil
}

// BroadcastText has ADBKeyBoard type text into what has the focus. It
// says nothing of whether it did: the broadcast goes nowhere until the
// input method is up and bound to the focused element.
func (d Device) BroadcastText(ctx context.Context, text string) error {
	msg := base64.StdEncoding.EncodeToString([]byte(text))
	_, err := d.run(ctx, "shell", "am", "broadcast", "-a", "ADB_INPUT_B64", "--es", "msg", msg)
	return err
}

func (d Device) imeList(ctx context.Context, all bool) ([]string, error) {
	args := []string{"shell", "ime", "list", "-s"}
	if all {
		args = append(args, "-a")
	}
	out, err := d.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

func (d Device) currentIME(ctx context.Context) (string, error) {
	out, err := d.run(ctx, "shell", "settings", "get", "secure", "default_input_method")
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(string(out))
	if s == "null" {
		return "", nil
	}
	return s, nil
}

// ime runs the ime command, which says what went wrong and exits 0 on some
// versions of Android.
func (d Device) ime(ctx context.Context, args ...string) error {
	out, err := d.run(ctx, append([]string{"shell", "ime"}, args...)...)
	if err != nil {
		return err
	}
	s := strings.TrimSpace(string(out))
	if strings.Contains(s, "Unknown") || strings.Contains(s, "cannot") || strings.HasPrefix(s, "Error") {
		return fmt.Errorf("ime %s: %s", strings.Join(args, " "), s)
	}
	return nil
}
