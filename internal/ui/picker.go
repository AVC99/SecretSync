package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// picker is a list with an always-focused filter field on top. Arrow keys
// move through the list while typing filters it.
type picker struct {
	title   string
	choices []Choice
	multi   bool
	checked map[string]bool
	visible []int // indexes into choices
	done    func([]string)
	abort   func()

	box            *tview.Flex
	hasDescription bool
	filter         *tview.InputField
	list           *tview.List
}

func newPicker(title, description string, choices []Choice, multi bool, def string, done func([]string), abort func()) *picker {
	p := &picker{
		title:   title,
		choices: choices,
		multi:   multi,
		checked: map[string]bool{},
		done:    done,
		abort:   abort,
	}
	p.filter = tview.NewInputField().
		SetLabel("Filter ").
		SetPlaceholder("type to filter…").
		SetLabelStyle(tcell.StyleDefault.Foreground(colMuted)).
		SetFieldStyle(tcell.StyleDefault).
		SetPlaceholderStyle(tcell.StyleDefault.Foreground(colMuted).Italic(true))
	p.list = tview.NewList().
		ShowSecondaryText(false).
		SetHighlightFullLine(true).
		SetSelectedFocusOnly(false).
		SetWrapAround(true).
		SetMainTextStyle(tcell.StyleDefault).
		SetSelectedStyle(tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(colAccent))

	p.box = panel(title)
	if description != "" {
		p.hasDescription = true
		p.box.AddItem(text(tagMuted+tview.Escape(description)+tagReset), 2, 0, false)
	}
	p.box.AddItem(p.filter, 1, 0, true).
		AddItem(nil, 1, 0, false).
		AddItem(p.list, 0, 1, false)

	p.refresh(def)
	p.filter.SetChangedFunc(func(string) { p.refresh(p.current()) })
	p.filter.SetInputCapture(p.handleKey)
	return p
}

// height is the number of rows the picker needs to show every choice.
func (p *picker) height() int {
	h := panelChrome + 2 + max(len(p.choices), 1) // filter + gap + list
	if p.hasDescription {
		h += 2
	}
	return h
}

func (p *picker) handleKey(ev *tcell.EventKey) *tcell.EventKey {
	switch ev.Key() {
	case tcell.KeyUp:
		p.move(-1, true)
	case tcell.KeyDown:
		p.move(1, true)
	case tcell.KeyPgUp:
		p.move(-10, false)
	case tcell.KeyPgDn:
		p.move(10, false)
	case tcell.KeyEnter:
		p.submit()
	case tcell.KeyEsc:
		if p.filter.GetText() != "" {
			p.filter.SetText("")
		} else {
			p.abort()
		}
	case tcell.KeyCtrlA:
		if !p.multi {
			return ev
		}
		p.toggleAll()
	case tcell.KeyRune:
		if !p.multi || ev.Rune() != ' ' {
			return ev
		}
		p.toggle()
	default:
		return ev
	}
	return nil
}

// current returns the value under the cursor, or "".
func (p *picker) current() string {
	i := p.list.GetCurrentItem()
	if i < 0 || i >= len(p.visible) {
		return ""
	}
	return p.choices[p.visible[i]].Value
}

func (p *picker) refresh(keep string) {
	q := strings.ToLower(strings.TrimSpace(p.filter.GetText()))
	p.visible = p.visible[:0]
	p.list.Clear()
	sel := 0
	for i, c := range p.choices {
		if q != "" && !strings.Contains(strings.ToLower(c.Label+" "+c.Hint), q) {
			continue
		}
		if c.Value == keep {
			sel = len(p.visible)
		}
		p.visible = append(p.visible, i)
		p.list.AddItem(p.render(c), "", 0, nil)
	}
	if len(p.visible) == 0 {
		p.list.AddItem(tagMuted+"no matches"+tagReset, "", 0, nil)
	}
	p.list.SetCurrentItem(sel)
	p.updateTitle()
}

func (p *picker) render(c Choice) string {
	s := tview.Escape(c.Label)
	if c.Hint != "" {
		s += "  " + tagMuted + tview.Escape(c.Hint) + tagReset
	}
	if !p.multi {
		return " " + s
	}
	if p.checked[c.Value] {
		return " " + tagGreen + "●" + tagReset + " " + s
	}
	return " " + tagMuted + "○" + tagReset + " " + s
}

func (p *picker) updateTitle() {
	title := fmt.Sprintf(" %s  %d/%d ", p.title, len(p.visible), len(p.choices))
	if p.multi {
		title = fmt.Sprintf(" %s  %d/%d · %d marked ", p.title, len(p.visible), len(p.choices), p.numChecked())
	}
	p.box.SetTitle(title)
}

func (p *picker) numChecked() int {
	n := 0
	for _, v := range p.checked {
		if v {
			n++
		}
	}
	return n
}

func (p *picker) move(delta int, wrap bool) {
	n := len(p.visible)
	if n == 0 {
		return
	}
	i := p.list.GetCurrentItem() + delta
	if wrap {
		i = ((i % n) + n) % n
	} else {
		i = min(max(i, 0), n-1)
	}
	p.list.SetCurrentItem(i)
}

func (p *picker) toggle() {
	i := p.list.GetCurrentItem()
	if i < 0 || i >= len(p.visible) {
		return
	}
	c := p.choices[p.visible[i]]
	p.checked[c.Value] = !p.checked[c.Value]
	p.list.SetItemText(i, p.render(c), "")
	p.updateTitle()
}

func (p *picker) toggleAll() {
	all := true
	for _, i := range p.visible {
		if !p.checked[p.choices[i].Value] {
			all = false
			break
		}
	}
	for _, i := range p.visible {
		p.checked[p.choices[i].Value] = !all
	}
	p.refresh(p.current())
}

func (p *picker) submit() {
	if len(p.visible) == 0 {
		return
	}
	if !p.multi {
		p.done([]string{p.current()})
		return
	}
	if p.numChecked() == 0 {
		p.toggle()
	}
	var out []string
	for _, c := range p.choices {
		if p.checked[c.Value] {
			out = append(out, c.Value)
		}
	}
	p.done(out)
}
