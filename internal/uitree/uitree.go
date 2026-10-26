// Package uitree reads the screen as uiautomator dump writes it, and finds
// the elements a selector names in it.
package uitree

import (
	"encoding/xml"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Node is an element of the screen.
type Node struct {
	// Class is the class of the view, such as android.widget.Button.
	Class string
	// ResourceID is the id of the view, as package:id/name.
	ResourceID string
	Text       string
	// Desc is the content description, which a screen reader reads for a
	// view with no text, such as an icon.
	Desc      string
	Package   string
	Bounds    Rect
	Enabled   bool
	Clickable bool
	Checked   bool
	Selected  bool
	// Focused is the element that takes what is typed.
	Focused  bool
	Password bool
	Children []*Node
}

// Rect is the bounds of a node on the screen, in pixels.
type Rect struct {
	Left, Top, Right, Bottom int
}

// Empty reports whether the rect has no area, as a node off the screen has.
func (r Rect) Empty() bool {
	return r.Right <= r.Left || r.Bottom <= r.Top
}

// Center is the point a tap on the node goes to.
func (r Rect) Center() (x, y int) {
	return (r.Left + r.Right) / 2, (r.Top + r.Bottom) / 2
}

func (r Rect) String() string {
	return fmt.Sprintf("[%d,%d][%d,%d]", r.Left, r.Top, r.Right, r.Bottom)
}

type xmlNode struct {
	Attrs    []xml.Attr `xml:",any,attr"`
	Children []xmlNode  `xml:"node"`
}

type xmlHierarchy struct {
	XMLName xml.Name  `xml:"hierarchy"`
	Nodes   []xmlNode `xml:"node"`
}

// Parse reads what uiautomator dump writes. The root it returns stands for
// the screen and holds the windows' nodes.
func Parse(b []byte) (*Node, error) {
	var h xmlHierarchy
	if err := xml.Unmarshal(b, &h); err != nil {
		return nil, fmt.Errorf("the screen uiautomator dumped is not its XML: %w", err)
	}
	root := &Node{Enabled: true}
	for _, x := range h.Nodes {
		n, err := convert(x)
		if err != nil {
			return nil, err
		}
		root.Children = append(root.Children, n)
	}
	return root, nil
}

func convert(x xmlNode) (*Node, error) {
	n := &Node{}
	for _, a := range x.Attrs {
		switch a.Name.Local {
		case "class":
			n.Class = a.Value
		case "resource-id":
			n.ResourceID = a.Value
		case "text":
			n.Text = a.Value
		case "content-desc":
			n.Desc = a.Value
		case "package":
			n.Package = a.Value
		case "enabled":
			n.Enabled = a.Value == "true"
		case "clickable":
			n.Clickable = a.Value == "true"
		case "checked":
			n.Checked = a.Value == "true"
		case "selected":
			n.Selected = a.Value == "true"
		case "focused":
			n.Focused = a.Value == "true"
		case "password":
			n.Password = a.Value == "true"
		case "bounds":
			r, err := parseBounds(a.Value)
			if err != nil {
				return nil, err
			}
			n.Bounds = r
		}
	}
	for _, c := range x.Children {
		child, err := convert(c)
		if err != nil {
			return nil, err
		}
		n.Children = append(n.Children, child)
	}
	return n, nil
}

var boundsRe = regexp.MustCompile(`^\[(-?\d+),(-?\d+)\]\[(-?\d+),(-?\d+)\]$`)

func parseBounds(s string) (Rect, error) {
	m := boundsRe.FindStringSubmatch(s)
	if m == nil {
		return Rect{}, fmt.Errorf("the bounds %q are not [left,top][right,bottom]", s)
	}
	var v [4]int
	for i := range v {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return Rect{}, fmt.Errorf("the bounds %q: %w", s, err)
		}
		v[i] = n
	}
	return Rect{Left: v[0], Top: v[1], Right: v[2], Bottom: v[3]}, nil
}

// Match is what a node has to have. An empty field matches any node.
type Match struct {
	// ID matches the resource id, whole or by its name after :id/, so that
	// "title" matches android:id/title and com.example:id/title.
	ID string
	// Text and Desc match the text and the content description, whole.
	Text string
	Desc string
	// Class matches the class, whole or by its name after the last dot, so
	// that "Button" matches android.widget.Button.
	Class string
}

// ErrNoMatch is what Find returns when no node matches.
var ErrNoMatch = errors.New("no element matches")

// Matches reports whether n has what m asks for.
func (m Match) Matches(n *Node) bool {
	if m.ID != "" && n.ResourceID != m.ID && !strings.HasSuffix(n.ResourceID, ":id/"+m.ID) {
		return false
	}
	if m.Text != "" && n.Text != m.Text {
		return false
	}
	if m.Desc != "" && n.Desc != m.Desc {
		return false
	}
	if m.Class != "" && n.Class != m.Class && !strings.HasSuffix(n.Class, "."+m.Class) {
		return false
	}
	return true
}

// FindAll returns the nodes under root that m matches, in the order the
// screen lists them, which is from the top of the tree down.
func FindAll(root *Node, m Match) []*Node {
	var found []*Node
	var walk func(*Node)
	walk = func(n *Node) {
		for _, c := range n.Children {
			if m.Matches(c) {
				found = append(found, c)
			}
			walk(c)
		}
	}
	walk(root)
	return found
}

// Find returns the index-th node under root that m matches.
func Find(root *Node, m Match, index int) (*Node, error) {
	found := FindAll(root, m)
	if index < len(found) {
		return found[index], nil
	}
	return nil, ErrNoMatch
}

// Focused returns the node that has the focus, or nil.
func Focused(root *Node) *Node {
	var found *Node
	var walk func(*Node)
	walk = func(n *Node) {
		for _, c := range n.Children {
			if found != nil {
				return
			}
			if c.Focused {
				found = c
				return
			}
			walk(c)
		}
	}
	walk(root)
	return found
}

// Format writes the nodes under root, one a line and indented by depth,
// with what a selector can name.
func Format(root *Node) string {
	var b strings.Builder
	var walk func(n *Node, depth int)
	walk = func(n *Node, depth int) {
		for _, c := range n.Children {
			b.WriteString(strings.Repeat("  ", depth))
			b.WriteString(c.String())
			b.WriteString("\n")
			walk(c, depth+1)
		}
	}
	walk(root, 0)
	return b.String()
}

// String is the node as Format writes it, on a line of its own.
func (n *Node) String() string {
	var b strings.Builder
	b.WriteString(shortClass(n.Class))
	if n.ResourceID != "" {
		fmt.Fprintf(&b, " id=%s", n.ResourceID)
	}
	if n.Text != "" {
		fmt.Fprintf(&b, " text=%q", n.Text)
	}
	if n.Desc != "" {
		fmt.Fprintf(&b, " desc=%q", n.Desc)
	}
	b.WriteString(" ")
	b.WriteString(n.Bounds.String())
	if !n.Enabled {
		b.WriteString(" disabled")
	}
	if n.Checked {
		b.WriteString(" checked")
	}
	if n.Selected {
		b.WriteString(" selected")
	}
	if n.Focused {
		b.WriteString(" focused")
	}
	return b.String()
}

func shortClass(c string) string {
	if c == "" {
		return "node"
	}
	if i := strings.LastIndexByte(c, '.'); i >= 0 {
		return c[i+1:]
	}
	return c
}
