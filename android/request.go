package android

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mozership/probe-android/internal/adb"
	"github.com/mozership/probe-android/internal/uitree"
	"github.com/mozership/probe/actionrpc"
)

// DefaultTimeout bounds the actions of a step that sets no timeout.
const DefaultTimeout = 30 * time.Second

// Req is the with of a step.
type Req struct {
	// App is the package name of the app the actions drive.
	App string `map:"app"`
	// Device names the device by its serial or model; empty takes the one
	// device adb can drive.
	Device  string    `map:"device"`
	Actions []*Action `map:"actions"`
	// Timeout bounds the actions, from the first to the last.
	Timeout time.Duration
}

// Action is one entry of the actions of a step.
type Action struct {
	Name string `map:"name"`
	// ID is the key of what the action returns, in res.results or
	// res.filepaths; empty is the action's name.
	ID string `map:"id"`
	// Selector names the element an action on an element acts on. See
	// ParseSelector.
	Selector string `map:"selector"`
	// Value is the text type types.
	Value string `map:"value"`

	selector Selector
}

// kind is what an action needs and returns.
type kind struct {
	selector bool
	value    bool
	// result is where its result goes: "results", "filepaths" or none.
	result string
}

// kinds are the actions a step takes, by name.
var kinds = map[string]kind{
	"launch":     {},
	"activate":   {},
	"terminate":  {},
	"back":       {},
	"tap":        {selector: true},
	"type":       {selector: true, value: true},
	"wait":       {selector: true},
	"text":       {selector: true, result: "results"},
	"tree":       {result: "results"},
	"screenshot": {result: "filepaths"},
}

// Params returns the keys the action takes in with.
func Params() []string {
	return append(actionrpc.ParamNames(Req{}), "timeout")
}

// parseReq reads the with of a step, and checks every action before any
// runs, so that a mistake in the last one does not leave the app halfway.
func parseReq(with map[string]any) (*Req, error) {
	req := &Req{Timeout: DefaultTimeout}
	if err := checkActionKeys(with["actions"]); err != nil {
		return nil, err
	}
	if err := actionrpc.DecodeParams(with, req); err != nil {
		return nil, err
	}
	if v, ok := with["timeout"]; ok {
		d, err := actionrpc.ParseTimeout(v)
		if err != nil {
			return nil, err
		}
		req.Timeout = d
	}
	if req.App == "" {
		return nil, fmt.Errorf("app is required: the package name of the app to drive, such as com.android.settings")
	}
	if len(req.Actions) == 0 {
		return nil, fmt.Errorf("actions is required: the actions to take, in order")
	}
	for i, a := range req.Actions {
		if err := a.check(); err != nil {
			return nil, fmt.Errorf("actions[%d]: %w", i, err)
		}
	}
	return req, nil
}

// actionKeys are the keys an entry of actions takes.
var actionKeys = actionrpc.ParamNames(Action{})

// checkActionKeys refuses a key an entry of actions does not take, such as
// args of the ios action, which would otherwise go unread.
func checkActionKeys(v any) error {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	for i, e := range list {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		var unknown []string
		for k := range m {
			if !slices.Contains(actionKeys, k) {
				unknown = append(unknown, k)
			}
		}
		if len(unknown) > 0 {
			sort.Strings(unknown)
			return fmt.Errorf("actions[%d]: an action takes no %s; it takes %s", i, strings.Join(unknown, ", "), strings.Join(actionKeys, ", "))
		}
	}
	return nil
}

func (a *Action) check() error {
	k, ok := kinds[a.Name]
	if !ok {
		return fmt.Errorf("no action is named %q; the actions are %s", a.Name, strings.Join(names(), ", "))
	}
	if k.selector {
		if a.Selector == "" {
			return fmt.Errorf("%s needs a selector", a.Name)
		}
		s, err := ParseSelector(a.Selector)
		if err != nil {
			return fmt.Errorf("%s: %w", a.Name, err)
		}
		a.selector = s
	} else if a.Selector != "" {
		return fmt.Errorf("%s takes no selector", a.Name)
	}
	if k.value {
		if a.Value == "" {
			return fmt.Errorf("%s needs a value, the text to type", a.Name)
		}
		if _, err := adb.InputText(a.Value); err != nil {
			return fmt.Errorf("%s: %w", a.Name, err)
		}
	} else if a.Value != "" {
		return fmt.Errorf("%s takes no value", a.Name)
	}
	if k.result == "" && a.ID != "" {
		return fmt.Errorf("%s returns nothing for id to name", a.Name)
	}
	return nil
}

// key is where the action's result goes.
func (a *Action) key() string {
	if a.ID != "" {
		return a.ID
	}
	return a.Name
}

func names() []string {
	s := make([]string, 0, len(kinds))
	for n := range kinds {
		s = append(s, n)
	}
	sort.Strings(s)
	return s
}

// Selector names an element of the screen: the first of the elements that
// have all it gives, or the Index-th of them.
type Selector struct {
	uitree.Match
	Index int
}

// ParseSelector reads a selector written as key=value pairs, separated by
// spaces, with a value in double quotes when it has a space:
//
//	id=login
//	class=Button text="Sign in"
//	id=title index=2
//
// The keys are id, text, desc, class and index. A selector with no = at
// all is a resource id, so "login" is id=login.
func ParseSelector(s string) (Selector, error) {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "=") {
		if strings.ContainsAny(s, " \"") {
			return Selector{}, fmt.Errorf("selector %q is not key=value pairs nor a resource id", s)
		}
		return Selector{Match: uitree.Match{ID: s}}, nil
	}
	pairs, err := splitPairs(s)
	if err != nil {
		return Selector{}, fmt.Errorf("selector %q: %w", s, err)
	}
	var sel Selector
	seen := map[string]bool{}
	for _, p := range pairs {
		key := p[0]
		if seen[key] {
			return Selector{}, fmt.Errorf("selector %q gives %s twice", s, key)
		}
		seen[key] = true
		switch key {
		case "id":
			sel.ID = p[1]
		case "text":
			sel.Text = p[1]
		case "desc":
			sel.Desc = p[1]
		case "class":
			sel.Class = p[1]
		case "index":
			n, err := strconv.Atoi(p[1])
			if err != nil || n < 0 {
				return Selector{}, fmt.Errorf("selector %q: index must be a number from 0", s)
			}
			sel.Index = n
		default:
			return Selector{}, fmt.Errorf("selector %q: no key is named %q; the keys are id, text, desc, class and index", s, p[0])
		}
	}
	return sel, nil
}

// String is the selector as a step writes it, for a message.
func (s Selector) String() string {
	var parts []string
	add := func(k, v string) {
		if v == "" {
			return
		}
		if strings.ContainsAny(v, " \"\\") {
			v = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`
		}
		parts = append(parts, k+"="+v)
	}
	add("id", s.ID)
	add("text", s.Text)
	add("desc", s.Desc)
	add("class", s.Class)
	if s.Index > 0 {
		parts = append(parts, "index="+strconv.Itoa(s.Index))
	}
	return strings.Join(parts, " ")
}

// splitPairs splits key=value pairs separated by spaces, where a value in
// double quotes may hold spaces, and \" and \\ in it stand for " and \.
func splitPairs(s string) ([][2]string, error) {
	var pairs [][2]string
	i := 0
	for i < len(s) {
		if s[i] == ' ' {
			i++
			continue
		}
		eq := strings.IndexByte(s[i:], '=')
		if eq <= 0 {
			return nil, fmt.Errorf("%q is not key=value", strings.Fields(s[i:])[0])
		}
		key := s[i : i+eq]
		if strings.ContainsAny(key, " \"") {
			return nil, fmt.Errorf("%q is not key=value", key)
		}
		i += eq + 1
		var value strings.Builder
		if i < len(s) && s[i] == '"' {
			i++
			closed := false
			for i < len(s) {
				c := s[i]
				if c == '\\' && i+1 < len(s) && (s[i+1] == '"' || s[i+1] == '\\') {
					value.WriteByte(s[i+1])
					i += 2
					continue
				}
				if c == '"' {
					closed = true
					i++
					break
				}
				value.WriteByte(c)
				i++
			}
			if !closed {
				return nil, fmt.Errorf("the value of %s has no closing quote", key)
			}
			if i < len(s) && s[i] != ' ' {
				return nil, fmt.Errorf("the value of %s goes on after its closing quote", key)
			}
		} else {
			for i < len(s) && s[i] != ' ' {
				if s[i] == '"' {
					return nil, fmt.Errorf("the value of %s has a quote in it; quote the whole value", key)
				}
				value.WriteByte(s[i])
				i++
			}
		}
		pairs = append(pairs, [2]string{key, value.String()})
	}
	return pairs, nil
}
