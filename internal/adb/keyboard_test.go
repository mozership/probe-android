package adb

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

const gboard = "com.google.android.inputmethod.latin/com.android.inputmethod.latin.LatinIME"

func calls(f *fakeAdb) []string {
	s := make([]string, len(f.calls))
	for i, c := range f.calls {
		s[i] = strings.Join(c[2:], " ")
	}
	return s
}

func TestCanInputText(t *testing.T) {
	for text, want := range map[string]bool{
		"hello world": true,
		"it's":        true,
		"日本語":         false,
		"100%sure":    false,
		"a\tb":        false,
	} {
		if got := CanInputText(text); got != want {
			t.Errorf("CanInputText(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestUseADBKeyBoard(t *testing.T) {
	f := &fakeAdb{replies: map[string]string{
		"shell ime list -s -a":                           gboard + "\n" + ADBKeyBoard + "\n",
		"shell ime list -s":                              gboard + "\n",
		"shell settings get secure default_input_method": gboard + "\n",
		"shell ime enable":                               "Input method com.android.adbkeyboard/.AdbIME: now enabled for user #0\n",
		"shell ime set":                                  "Input method com.android.adbkeyboard/.AdbIME selected for user #0\n",
		"shell ime disable":                              "Input method com.android.adbkeyboard/.AdbIME: now disabled for user #0\n",
	}}
	restore, err := device(f).UseADBKeyBoard(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"shell ime list -s -a",
		"shell ime list -s",
		"shell settings get secure default_input_method",
		"shell ime enable " + ADBKeyBoard,
		"shell ime set " + ADBKeyBoard,
	}
	if got := calls(f); !reflect.DeepEqual(got, want) {
		t.Errorf("calls =\n%q\nwant\n%q", got, want)
	}
	f.calls = nil
	if err := restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	want = []string{"shell ime set " + gboard, "shell ime disable " + ADBKeyBoard}
	if got := calls(f); !reflect.DeepEqual(got, want) {
		t.Errorf("restore ran %q, want %q", got, want)
	}
}

func TestUseADBKeyBoardEnabledAlready(t *testing.T) {
	f := &fakeAdb{replies: map[string]string{
		"shell ime list -s -a":                           gboard + "\n" + ADBKeyBoard + "\n",
		"shell ime list -s":                              gboard + "\n" + ADBKeyBoard + "\n",
		"shell settings get secure default_input_method": "null\n",
	}}
	restore, err := device(f).UseADBKeyBoard(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range calls(f) {
		if strings.HasPrefix(c, "shell ime enable") {
			t.Errorf("an enabled ADBKeyBoard was enabled again: %s", c)
		}
	}
	f.calls = nil
	if err := restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := calls(f); !reflect.DeepEqual(got, []string{"shell ime reset"}) {
		t.Errorf("restore ran %q, want a reset of an input method that was none, and ADBKeyBoard left enabled", got)
	}
}

func TestUseADBKeyBoardInUseAlready(t *testing.T) {
	f := &fakeAdb{replies: map[string]string{
		"shell ime list -s": ADBKeyBoard + "\n",
		"shell settings get secure default_input_method": ADBKeyBoard + "\n",
	}}
	restore, err := device(f).UseADBKeyBoard(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, c := range calls(f) {
		if strings.HasPrefix(c, "shell ime set") || strings.HasPrefix(c, "shell ime enable") {
			t.Errorf("the input method in use was set again: %s", c)
		}
	}
}

func TestUseADBKeyBoardNotInstalled(t *testing.T) {
	f := &fakeAdb{replies: map[string]string{"shell ime list -s": gboard + "\n"}}
	_, err := device(f).UseADBKeyBoard(context.Background())
	if !errors.Is(err, ErrNoADBKeyBoard) || !strings.Contains(err.Error(), "github.com/senzhk/ADBKeyBoard/releases") {
		t.Errorf("UseADBKeyBoard = %v, want ErrNoADBKeyBoard, which says where to get it", err)
	}
	if len(f.calls) != 1 {
		t.Errorf("calls = %q, want nothing changed", calls(f))
	}
}

func TestUseADBKeyBoardSetFails(t *testing.T) {
	f := &fakeAdb{replies: map[string]string{
		"shell ime list -s -a":                           gboard + "\n" + ADBKeyBoard + "\n",
		"shell ime list -s":                              gboard + "\n",
		"shell settings get secure default_input_method": gboard + "\n",
		"shell ime set " + ADBKeyBoard:                   "Unknown input method com.android.adbkeyboard/.AdbIME cannot be selected for user #0\n",
	}}
	_, err := device(f).UseADBKeyBoard(context.Background())
	if err == nil || !strings.Contains(err.Error(), "cannot be selected") {
		t.Fatalf("UseADBKeyBoard = %v", err)
	}
	got := calls(f)
	if last := got[len(got)-2:]; !reflect.DeepEqual(last, []string{"shell ime set " + gboard, "shell ime disable " + ADBKeyBoard}) {
		t.Errorf("a failed switch left the input methods as %q, want them put back", got)
	}
}

func TestBroadcastText(t *testing.T) {
	f := &fakeAdb{}
	if err := device(f).BroadcastText(context.Background(), "日本語 it's"); err != nil {
		t.Fatal(err)
	}
	want := []string{"-s", "emulator-5554", "shell", "am", "broadcast", "-a", "ADB_INPUT_B64", "--es", "msg", "5pel5pys6KqeIGl0J3M="}
	if !reflect.DeepEqual(f.calls[0], want) {
		t.Errorf("adb %q, want %q", f.calls[0], want)
	}
}
