package uitree

import (
	"errors"
	"strings"
	"testing"
)

// settings is a screen as uiautomator dump writes it, cut down.
const settings = `<?xml version='1.0' encoding='UTF-8' standalone='yes' ?><hierarchy rotation="0"><node index="0" text="" resource-id="" class="android.widget.FrameLayout" package="com.android.settings" content-desc="" checkable="false" checked="false" clickable="false" enabled="true" focusable="false" focused="false" scrollable="false" long-clickable="false" password="false" selected="false" bounds="[0,0][1080,2400]"><node index="0" text="Settings" resource-id="com.android.settings:id/homepage_title" class="android.widget.TextView" package="com.android.settings" content-desc="" checkable="false" checked="false" clickable="false" enabled="true" focusable="false" focused="false" scrollable="false" long-clickable="false" password="false" selected="false" bounds="[63,252][1017,399]" /><node index="1" text="" resource-id="" class="android.widget.LinearLayout" package="com.android.settings" content-desc="" checkable="false" checked="false" clickable="true" enabled="true" focusable="true" focused="false" scrollable="false" long-clickable="false" password="false" selected="false" bounds="[0,500][1080,700]"><node index="0" text="Network &amp; internet" resource-id="android:id/title" class="android.widget.TextView" package="com.android.settings" content-desc="" checkable="false" checked="false" clickable="false" enabled="true" focusable="false" focused="false" scrollable="false" long-clickable="false" password="false" selected="false" bounds="[200,540][700,600]" /></node><node index="2" text="" resource-id="" class="android.widget.LinearLayout" package="com.android.settings" content-desc="" checkable="false" checked="false" clickable="true" enabled="false" focusable="true" focused="false" scrollable="false" long-clickable="false" password="false" selected="false" bounds="[0,700][1080,900]"><node index="0" text="Connected devices" resource-id="android:id/title" class="android.widget.TextView" package="com.android.settings" content-desc="" checkable="false" checked="true" clickable="false" enabled="true" focusable="false" focused="false" scrollable="false" long-clickable="false" password="false" selected="false" bounds="[200,740][700,800]" /></node><node index="3" text="" resource-id="" class="android.widget.ImageButton" package="com.android.settings" content-desc="Search settings" checkable="false" checked="false" clickable="true" enabled="true" focusable="true" focused="false" scrollable="false" long-clickable="false" password="false" selected="true" bounds="[900,100][1050,250]" /></node></hierarchy>`

func parse(t *testing.T) *Node {
	t.Helper()
	root, err := Parse([]byte(settings))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestParse(t *testing.T) {
	root := parse(t)
	if len(root.Children) != 1 {
		t.Fatalf("the screen has %d windows, want 1", len(root.Children))
	}
	frame := root.Children[0]
	if frame.Class != "android.widget.FrameLayout" || frame.Package != "com.android.settings" || frame.Bounds != (Rect{0, 0, 1080, 2400}) {
		t.Errorf("the frame is %+v", frame)
	}
	title := frame.Children[1].Children[0]
	if title.Text != "Network & internet" || title.ResourceID != "android:id/title" {
		t.Errorf("the entities were not read: %+v", title)
	}
	if !frame.Children[1].Clickable || frame.Children[2].Enabled {
		t.Errorf("the flags were not read")
	}
	search := frame.Children[3]
	if search.Desc != "Search settings" || !search.Selected {
		t.Errorf("the search button is %+v", search)
	}
}

func TestParseErrors(t *testing.T) {
	tests := map[string]string{
		"ERROR: could not get idle state.":               "not its XML",
		`<hierarchy><node bounds="[0,0]" /></hierarchy>`: `the bounds "[0,0]" are not`,
		`<other />`: "not its XML",
	}
	for in, want := range tests {
		_, err := Parse([]byte(in))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q) = %v, want an error that says %q", in, err, want)
		}
	}
}

func TestFind(t *testing.T) {
	root := parse(t)
	tests := []struct {
		name  string
		m     Match
		index int
		want  string
	}{
		{name: "text", m: Match{Text: "Network & internet"}, want: "Network & internet"},
		{name: "full id", m: Match{ID: "com.android.settings:id/homepage_title"}, want: "Settings"},
		{name: "id by its name", m: Match{ID: "homepage_title"}, want: "Settings"},
		{name: "id of the framework", m: Match{ID: "title"}, want: "Network & internet"},
		{name: "the second match", m: Match{ID: "title"}, index: 1, want: "Connected devices"},
		{name: "short class", m: Match{Class: "ImageButton"}, want: "desc:Search settings"},
		{name: "full class", m: Match{Class: "android.widget.ImageButton"}, want: "desc:Search settings"},
		{name: "desc", m: Match{Desc: "Search settings"}, want: "desc:Search settings"},
		{name: "class and text", m: Match{Class: "TextView", Text: "Connected devices"}, want: "Connected devices"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, err := Find(root, tt.m, tt.index)
			if err != nil {
				t.Fatal(err)
			}
			got := n.Text
			if got == "" {
				got = "desc:" + n.Desc
			}
			if got != tt.want {
				t.Errorf("Find = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFindNoMatch(t *testing.T) {
	root := parse(t)
	for _, tc := range []struct {
		m     Match
		index int
	}{
		{m: Match{Text: "Network"}},
		{m: Match{ID: "itle"}},
		{m: Match{Class: "Button"}},
		{m: Match{ID: "title"}, index: 2},
		{m: Match{Class: "TextView", Desc: "Search settings"}},
	} {
		if _, err := Find(root, tc.m, tc.index); !errors.Is(err, ErrNoMatch) {
			t.Errorf("Find(%+v, %d) = %v, want ErrNoMatch", tc.m, tc.index, err)
		}
	}
}

func TestFindAllWithNoMatchIsEveryNode(t *testing.T) {
	if got := len(FindAll(parse(t), Match{})); got != 7 {
		t.Errorf("an empty match found %d nodes, want all 7", got)
	}
}

func TestRect(t *testing.T) {
	r := Rect{200, 540, 700, 600}
	if x, y := r.Center(); x != 450 || y != 570 {
		t.Errorf("Center = %d,%d", x, y)
	}
	if r.Empty() || !(Rect{0, 0, 0, 10}).Empty() || !(Rect{10, 10, 5, 20}).Empty() {
		t.Errorf("Empty is wrong")
	}
}

func TestFormat(t *testing.T) {
	want := `FrameLayout [0,0][1080,2400]
  TextView id=com.android.settings:id/homepage_title text="Settings" [63,252][1017,399]
  LinearLayout [0,500][1080,700]
    TextView id=android:id/title text="Network & internet" [200,540][700,600]
  LinearLayout [0,700][1080,900] disabled
    TextView id=android:id/title text="Connected devices" [200,740][700,800] checked
  ImageButton desc="Search settings" [900,100][1050,250] selected
`
	if got := Format(parse(t)); got != want {
		t.Errorf("Format =\n%s\nwant\n%s", got, want)
	}
}
