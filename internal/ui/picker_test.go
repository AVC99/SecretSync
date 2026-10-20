package ui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

var fruits = []Choice{
	{Label: "apple", Value: "a", Hint: "red"},
	{Label: "banana", Value: "b"},
	{Label: "cherry", Value: "c", Hint: "red"},
}

type pickerResult struct {
	done    []string
	aborted bool
}

func newTestPicker(multi bool, def string) (*picker, *pickerResult) {
	r := &pickerResult{}
	p := newPicker("Fruit", "", fruits, multi, def,
		func(v []string) { r.done = v },
		func() { r.aborted = true })
	return p, r
}

func key(k tcell.Key) *tcell.EventKey { return tcell.NewEventKey(k, 0, tcell.ModNone) }
func char(r rune) *tcell.EventKey     { return tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone) }

func TestPickerSelectDefaultAndWrap(t *testing.T) {
	p, r := newTestPicker(false, "b")
	if got := p.current(); got != "b" {
		t.Fatalf("default = %q, want b", got)
	}
	p.handleKey(key(tcell.KeyDown))
	p.handleKey(key(tcell.KeyDown)) // wraps to the first item
	p.handleKey(key(tcell.KeyEnter))
	if !reflect.DeepEqual(r.done, []string{"a"}) {
		t.Errorf("done = %v, want [a]", r.done)
	}
}

func TestPickerFilterMatchesLabelAndHint(t *testing.T) {
	p, _ := newTestPicker(false, "")
	p.filter.SetText("RED")
	var got []string
	for _, i := range p.visible {
		got = append(got, p.choices[i].Value)
	}
	if !reflect.DeepEqual(got, []string{"a", "c"}) {
		t.Errorf("visible = %v, want [a c]", got)
	}

	p.filter.SetText("zzz")
	if len(p.visible) != 0 || p.current() != "" {
		t.Errorf("no match: visible = %v, current = %q", p.visible, p.current())
	}
}

func TestPickerEscClearsFilterThenAborts(t *testing.T) {
	p, r := newTestPicker(false, "")
	p.filter.SetText("ban")
	p.handleKey(key(tcell.KeyEsc))
	if p.filter.GetText() != "" || r.aborted {
		t.Fatalf("first esc: filter = %q, aborted = %v", p.filter.GetText(), r.aborted)
	}
	p.handleKey(key(tcell.KeyEsc))
	if !r.aborted {
		t.Error("second esc should abort")
	}
}

func TestPickerSpaceIsTextInSingleMode(t *testing.T) {
	p, _ := newTestPicker(false, "")
	if p.handleKey(char(' ')) == nil {
		t.Error("space should reach the filter field in single mode")
	}
}

func TestPickerMultiToggle(t *testing.T) {
	p, r := newTestPicker(true, "")
	p.handleKey(char(' ')) // apple
	p.handleKey(key(tcell.KeyDown))
	p.handleKey(key(tcell.KeyDown))
	p.handleKey(char(' ')) // cherry
	if !strings.Contains(p.box.GetTitle(), "2 marked") {
		t.Errorf("title = %q", p.box.GetTitle())
	}
	p.handleKey(key(tcell.KeyEnter))
	if !reflect.DeepEqual(r.done, []string{"a", "c"}) {
		t.Errorf("done = %v, want [a c]", r.done)
	}
}

func TestPickerMultiSubmitWithoutMarksTakesCurrent(t *testing.T) {
	p, r := newTestPicker(true, "")
	p.handleKey(key(tcell.KeyDown))
	p.handleKey(key(tcell.KeyEnter))
	if !reflect.DeepEqual(r.done, []string{"b"}) {
		t.Errorf("done = %v, want [b]", r.done)
	}
}

func TestPickerToggleAllOnlyVisible(t *testing.T) {
	p, r := newTestPicker(true, "")
	p.filter.SetText("red")
	p.handleKey(key(tcell.KeyCtrlA))
	p.filter.SetText("")
	p.handleKey(key(tcell.KeyEnter))
	if !reflect.DeepEqual(r.done, []string{"a", "c"}) {
		t.Errorf("done = %v, want [a c]", r.done)
	}

	// A second ctrl+a with everything visible already marked unmarks them.
	p.filter.SetText("red")
	p.handleKey(key(tcell.KeyCtrlA))
	if p.numChecked() != 0 {
		t.Errorf("checked = %d after toggling all off", p.numChecked())
	}
}

func TestPickerHeight(t *testing.T) {
	p, _ := newTestPicker(false, "")
	if got, want := p.height(), panelChrome+2+len(fruits); got != want {
		t.Errorf("height = %d, want %d", got, want)
	}
	withDesc := newPicker("x", "description", fruits, false, "", func([]string) {}, func() {})
	if got, want := withDesc.height(), panelChrome+4+len(fruits); got != want {
		t.Errorf("height with description = %d, want %d", got, want)
	}
}

func TestRenderReview(t *testing.T) {
	out := renderReview(ReviewData{
		Status: "Everything is up to date",
		Sections: []Section{
			{Title: ".env", Changes: []Change{
				{Sign: '+', Key: "NEW", Value: "ab****"},
				{Sign: '~', Key: "UPDATED", Value: "cd****"},
				{Sign: '=', Key: "SAME", Value: "secret"},
			}, Notes: []string{"a note"}},
			{Title: "empty.env"},
		},
		Warnings: []string{"careful [red]"},
	})
	for _, want := range []string{"Everything is up to date", "NEW", "ab****", "UPDATED", "unchanged",
		"a note", "(nothing to write)", "careful [red[]"} {
		if !strings.Contains(out, want) {
			t.Errorf("review missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "secret") {
		t.Error("unchanged values must not be shown")
	}
}

func TestLinesCountsWrapping(t *testing.T) {
	width := maxWidth - panelChrome - 2
	cases := map[string]int{
		"one":                             1,
		"one\ntwo\n":                      2,
		"a\n\nb":                          3,
		strings.Repeat("x", width+1):      2,
		tagMuted + "tagged" + tagReset:    1,
		strings.Repeat("x", 2*width) + "": 2,
	}
	for s, want := range cases {
		if got := lines(s); got != want {
			t.Errorf("lines(%.20q…) = %d, want %d", s, got, want)
		}
	}
}
