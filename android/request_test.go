package android

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mozership/probe-android/internal/uitree"
)

func TestParseSelector(t *testing.T) {
	tests := []struct {
		in   string
		want Selector
	}{
		{in: "login", want: Selector{Match: uitree.Match{ID: "login"}}},
		{in: "id=com.example:id/login", want: Selector{Match: uitree.Match{ID: "com.example:id/login"}}},
		{in: `class=Button text="Sign in"`, want: Selector{Match: uitree.Match{Class: "Button", Text: "Sign in"}}},
		{in: `desc="Search settings"`, want: Selector{Match: uitree.Match{Desc: "Search settings"}}},
		{in: "id=title index=2", want: Selector{Match: uitree.Match{ID: "title"}, Index: 2}},
		{in: `text="say \"hi\" \\ bye"`, want: Selector{Match: uitree.Match{Text: `say "hi" \ bye`}}},
		{in: `  text=Wi-Fi   class=TextView  `, want: Selector{Match: uitree.Match{Text: "Wi-Fi", Class: "TextView"}}},
		{in: `text=""`, want: Selector{}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseSelector(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseSelector = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseSelectorErrors(t *testing.T) {
	tests := map[string]string{
		"Sign in":          "not key=value pairs nor a resource id",
		"label=Sign":       `no key is named "label"`,
		"index=-1":         "index must be a number from 0",
		"index=first":      "index must be a number from 0",
		"id=a id=b":        "gives id twice",
		`text="Sign in`:    "no closing quote",
		`text="Sign"in`:    "goes on after its closing quote",
		`text=Sign"in"`:    "quote the whole value",
		"class=Button =x":  `"=x" is not key=value`,
		"class=Button foo": `"foo" is not key=value`,
	}
	for in, want := range tests {
		t.Run(in, func(t *testing.T) {
			_, err := ParseSelector(in)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("ParseSelector(%q) = %v, want an error that says %q", in, err, want)
			}
		})
	}
}

func TestSelectorString(t *testing.T) {
	tests := map[string]string{
		"login":                                  "id=login",
		`class=Button text="Sign in" index=2`:    `text="Sign in" class=Button index=2`,
		`desc="a \"b\" \\"`:                      `desc="a \"b\" \\"`,
		"id=title index=0":                       "id=title",
		`text="Network & internet" class=Button`: `text="Network & internet" class=Button`,
	}
	for in, want := range tests {
		s, err := ParseSelector(in)
		if err != nil {
			t.Fatal(err)
		}
		if got := s.String(); got != want {
			t.Errorf("String of %q = %q, want %q", in, got, want)
		}
		// What String writes reads back as the same selector.
		back, err := ParseSelector(s.String())
		if err != nil || !reflect.DeepEqual(back, s) {
			t.Errorf("%q reads back as %+v, %v", s.String(), back, err)
		}
	}
}

func TestParseReq(t *testing.T) {
	req, err := parseReq(map[string]any{
		"app":     "com.android.settings",
		"device":  "emulator-5554",
		"timeout": "1m",
		"actions": []any{
			map[string]any{"name": "launch"},
			map[string]any{"name": "tap", "selector": `text="Network & internet"`},
			map[string]any{"name": "type", "selector": "class=EditText", "value": "hello world"},
			map[string]any{"name": "text", "id": "title", "selector": "id=title"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if req.App != "com.android.settings" || req.Device != "emulator-5554" || req.Timeout != time.Minute {
		t.Errorf("parseReq = %+v", req)
	}
	if got := req.Actions[1].selector; got.Text != "Network & internet" {
		t.Errorf("the selector was not parsed: %+v", got)
	}
	if got := req.Actions[3].key(); got != "title" {
		t.Errorf("key = %q, want the id", got)
	}
	if got := req.Actions[0].key(); got != "launch" {
		t.Errorf("key = %q, want the name", got)
	}

	def, err := parseReq(map[string]any{"app": "a", "actions": []any{map[string]any{"name": "launch"}}})
	if err != nil {
		t.Fatal(err)
	}
	if def.Timeout != DefaultTimeout {
		t.Errorf("the timeout is %s without one given, want %s", def.Timeout, DefaultTimeout)
	}
}

func TestParseReqErrors(t *testing.T) {
	action := func(a map[string]any) map[string]any {
		return map[string]any{"app": "com.example", "actions": []any{a}}
	}
	tests := []struct {
		name string
		with map[string]any
		want string
	}{
		{name: "no app", with: map[string]any{"actions": []any{map[string]any{"name": "launch"}}}, want: "app is required"},
		{name: "no actions", with: map[string]any{"app": "com.example"}, want: "actions is required"},
		{name: "bad timeout", with: map[string]any{"app": "a", "timeout": "soon", "actions": []any{map[string]any{"name": "launch"}}}, want: "timeout"},
		{name: "unknown action", with: action(map[string]any{"name": "swipe"}), want: `actions[0]: no action is named "swipe"; the actions are activate, back, launch`},
		{name: "tap without selector", with: action(map[string]any{"name": "tap"}), want: "tap needs a selector"},
		{name: "back with selector", with: action(map[string]any{"name": "back", "selector": "x"}), want: "back takes no selector"},
		{name: "type without value", with: action(map[string]any{"name": "type", "selector": "x"}), want: "type needs a value"},
		{name: "tap with value", with: action(map[string]any{"name": "tap", "selector": "x", "value": "y"}), want: "tap takes no value"},
		{name: "tap with id", with: action(map[string]any{"name": "tap", "selector": "x", "id": "t"}), want: "returns nothing for id"},
		{name: "bad selector", with: action(map[string]any{"name": "tap", "selector": "label=x"}), want: "tap: selector"},
		{name: "launch with args", with: action(map[string]any{"name": "launch", "args": []any{"-a"}}), want: "actions[0]: an action takes no args; it takes name, id, selector, value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseReq(tt.with)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("parseReq = %v, want an error that says %q", err, tt.want)
			}
		})
	}
}

func TestParams(t *testing.T) {
	want := []string{"app", "device", "actions", "timeout"}
	if got := Params(); !reflect.DeepEqual(got, want) {
		t.Errorf("Params = %v, want %v", got, want)
	}
}
