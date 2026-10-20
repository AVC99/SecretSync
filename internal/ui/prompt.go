// Package ui implements the interactive terminal UI on top of tview.
//
// The UI is a single full-screen application that is started lazily by the
// first prompt and stays open until Close. Prompts are synchronous: each
// one replaces the body of the screen and blocks until the user answers.
// Output printed with Print/Warn while the UI is open is buffered and
// written to the terminal when it closes.
package ui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"golang.org/x/term"
)

// ErrAborted is returned when the user cancels (Esc / Ctrl+C).
var ErrAborted = errors.New("cancelled by the user")

// ErrNoTTY is returned when a prompt is needed but stdin is not a terminal.
var ErrNoTTY = errors.New("an interactive terminal is required (or pass --mode, --profile, --secret and --yes)")

// Regions offered when the profile does not define one.
var Regions = []string{
	"eu-west-1", "eu-west-2", "eu-west-3", "eu-central-1", "eu-central-2",
	"eu-south-1", "eu-south-2", "eu-north-1",
	"us-east-1", "us-east-2", "us-west-1", "us-west-2",
	"ca-central-1", "sa-east-1",
	"ap-south-1", "ap-southeast-1", "ap-southeast-2", "ap-northeast-1", "ap-northeast-2",
	"me-central-1", "af-south-1",
}

var (
	colAccent = tcell.NewHexColor(0x7aa2f7)
	colMuted  = tcell.NewHexColor(0x737aa2)
)

const (
	tagAccent = "[#7aa2f7]"
	tagGreen  = "[#9ece6a]"
	tagYellow = "[#e0af68]"
	tagRed    = "[#f7768e]"
	tagMuted  = "[#737aa2]"
	tagBold   = "[::b]"
	tagReset  = "[-:-:-]"
)

// Choice is an option of Select / MultiSelect.
type Choice struct {
	Label string
	Value string
	Hint  string // optional, shown dimmed after the label
}

type step struct{ label, value string }

type outLine struct {
	w    io.Writer
	text string
}

type tui struct {
	app      *tview.Application
	header   *tview.TextView
	body     *tview.Flex
	footer   *tview.TextView
	finished chan struct{}
	aborted  chan struct{}
	once     sync.Once
}

var (
	mu          sync.Mutex
	cur         *tui
	steps       []step
	pending     []outLine
	onInterrupt func()
)

// Interactive reports whether prompts can be shown.
func Interactive() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// Active reports whether the UI is currently on screen.
func Active() bool {
	mu.Lock()
	defer mu.Unlock()
	return cur != nil
}

// OnInterrupt registers f to be called when the user presses Ctrl+C or
// cancels a prompt while the UI is open.
func OnInterrupt(f func()) {
	mu.Lock()
	onInterrupt = f
	mu.Unlock()
}

// SetStep records a decision (e.g. "Perfil" = "dev") shown in the header.
func SetStep(label, value string) {
	mu.Lock()
	found := false
	for i := range steps {
		if steps[i].label == label {
			steps[i].value = value
			found = true
		}
	}
	if !found {
		steps = append(steps, step{label, value})
	}
	t := cur
	mu.Unlock()
	if t != nil {
		t.app.QueueUpdateDraw(t.renderHeader)
	}
}

// Print writes a line to stdout, or buffers it while the UI is open.
func Print(s string) { output(os.Stdout, s) }

// Warn writes a line to stderr, or buffers it while the UI is open.
func Warn(s string) { output(os.Stderr, s) }

func output(w io.Writer, s string) {
	mu.Lock()
	defer mu.Unlock()
	if cur != nil {
		pending = append(pending, outLine{w, s})
		return
	}
	fmt.Fprintln(w, s)
}

// Close stops the UI (if open) and flushes the buffered output.
func Close() {
	mu.Lock()
	t := cur
	cur = nil
	out := pending
	pending = nil
	mu.Unlock()
	if t != nil {
		t.app.Stop()
		<-t.finished
	}
	for _, l := range out {
		fmt.Fprintln(l.w, l.text)
	}
}

func applyTheme() {
	tview.Styles.PrimitiveBackgroundColor = tcell.ColorDefault
	tview.Styles.ContrastBackgroundColor = colAccent
	tview.Styles.MoreContrastBackgroundColor = colAccent
	tview.Styles.BorderColor = colMuted
	tview.Styles.TitleColor = colAccent
	tview.Styles.GraphicsColor = colMuted
	tview.Styles.PrimaryTextColor = tcell.ColorDefault
	tview.Styles.SecondaryTextColor = colAccent
	tview.Styles.TertiaryTextColor = colMuted
	tview.Styles.InverseTextColor = tcell.ColorBlack
	tview.Styles.ContrastSecondaryTextColor = tcell.ColorBlack

	// Single-line borders whether focused or not; rounded corners where
	// the font is known to support them.
	b := &tview.Borders
	b.HorizontalFocus, b.VerticalFocus = b.Horizontal, b.Vertical
	if runtime.GOOS != "windows" {
		b.TopLeft, b.TopRight, b.BottomLeft, b.BottomRight = '╭', '╮', '╰', '╯'
	}
	b.TopLeftFocus, b.TopRightFocus = b.TopLeft, b.TopRight
	b.BottomLeftFocus, b.BottomRightFocus = b.BottomLeft, b.BottomRight
}

// start opens the UI if it is not open yet.
//
// QueueUpdate/QueueUpdateDraw block until the UI goroutine runs the update,
// so they must never be called with mu held or from inside a UI callback.
func start() (*tui, error) {
	mu.Lock()
	if cur != nil {
		t := cur
		mu.Unlock()
		return t, nil
	}
	if !Interactive() {
		mu.Unlock()
		return nil, ErrNoTTY
	}
	applyTheme()

	t := &tui{
		app:      tview.NewApplication(),
		header:   tview.NewTextView().SetDynamicColors(true).SetWrap(false),
		body:     tview.NewFlex(),
		footer:   tview.NewTextView().SetDynamicColors(true),
		finished: make(chan struct{}),
		aborted:  make(chan struct{}),
	}
	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(t.header, 3, 0, false).
		AddItem(t.body, 0, 1, true).
		AddItem(t.footer, 1, 0, false)
	layout.SetBorderPadding(1, 0, 2, 2)
	t.app.SetRoot(layout, true)
	t.app.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyCtrlC {
			t.cancel()
			return nil
		}
		return ev
	})
	go func() {
		_ = t.app.Run()
		close(t.finished)
	}()
	cur = t
	mu.Unlock()
	t.app.QueueUpdateDraw(t.renderHeader)
	return t, nil
}

func (t *tui) cancel() {
	t.once.Do(func() {
		close(t.aborted)
		mu.Lock()
		f := onInterrupt
		mu.Unlock()
		if f != nil {
			f()
		}
	})
}

// renderHeader must run on the UI goroutine.
func (t *tui) renderHeader() {
	mu.Lock()
	s := append([]step(nil), steps...)
	mu.Unlock()

	var b strings.Builder
	b.WriteString(tagAccent + tagBold + "secretsync" + tagReset)
	b.WriteString(tagMuted + "  AWS Secrets Manager secrets → Bruno / .env" + tagReset + "\n")
	for i, st := range s {
		if i > 0 {
			b.WriteString(tagMuted + "  ›  " + tagReset)
		}
		b.WriteString(tagMuted + st.label + " " + tagReset + tagBold + tview.Escape(st.value) + tagReset)
	}
	t.header.SetText(b.String())
}

// maxWidth is the widest a panel gets, so lists stay readable on wide terminals.
const maxWidth = 100

// capped limits the size of a primitive to its content, so panels do not
// stretch over the whole screen.
type capped struct {
	tview.Primitive
	height int // 0 = no limit
}

func (c *capped) SetRect(x, y, width, height int) {
	if c.height > 0 {
		height = min(height, c.height)
	}
	c.Primitive.SetRect(x, y, min(width, maxWidth), height)
}

// show replaces the body with content, at most height rows tall (0 = fill).
// It must be called from the caller goroutine, never from a UI callback.
func (t *tui) show(content, focus tview.Primitive, help string, height int) {
	t.app.QueueUpdateDraw(func() {
		t.body.Clear()
		t.body.AddItem(&capped{Primitive: content, height: height}, 0, 1, true)
		t.footer.SetText(help)
		t.app.SetFocus(focus)
	})
}

func await[T any](t *tui, ch <-chan T) (T, error) {
	var zero T
	select {
	case v := <-ch:
		return v, nil
	case <-t.aborted:
		return zero, ErrAborted
	case <-t.finished:
		return zero, ErrAborted
	}
}

func keyHelp(pairs ...string) string {
	var b strings.Builder
	for i := 0; i+1 < len(pairs); i += 2 {
		if i > 0 {
			b.WriteString("   ")
		}
		b.WriteString(tagAccent + pairs[i] + tagReset + " " + tagMuted + pairs[i+1] + tagReset)
	}
	return b.String()
}

// panelChrome is the number of rows a panel uses for its border and padding.
const panelChrome = 4

func panel(title string) *tview.Flex {
	f := tview.NewFlex().SetDirection(tview.FlexRow)
	f.SetBorder(true).SetTitle(" "+title+" ").SetTitleAlign(tview.AlignLeft).SetBorderPadding(1, 1, 2, 2)
	return f
}

// lines estimates the rows s takes inside a panel of maxWidth, counting wrapping.
func lines(s string) int {
	width := maxWidth - panelChrome - 2
	n := 0
	for _, l := range strings.Split(strings.TrimSuffix(s, "\n"), "\n") {
		n += max(1, (tview.TaggedStringWidth(l)+width-1)/width)
	}
	return n
}

func text(s string) *tview.TextView {
	tv := tview.NewTextView().SetDynamicColors(true).SetWrap(true)
	tv.SetText(s)
	return tv
}

// Loading shows a status message while something slow happens. It does
// nothing if the UI is not open.
func Loading(msg string) {
	mu.Lock()
	t := cur
	mu.Unlock()
	if t == nil {
		return
	}
	p := panel("One moment")
	p.AddItem(text(tagYellow+"● "+tagReset+tview.Escape(msg)), 0, 1, false)
	t.show(p, p, keyHelp("ctrl+c", "cancel"), panelChrome+1)
}

// Select asks the user to pick one of choices. def is preselected.
func Select(title, description string, choices []Choice, def string) (string, error) {
	t, err := start()
	if err != nil {
		return "", err
	}
	res := make(chan []string, 1)
	p := newPicker(title, description, choices, false, def, func(v []string) { res <- v }, t.cancel)
	t.show(p.box, p.filter, keyHelp("↑↓", "move", "enter", "select", "type", "to filter", "esc", "cancel"), p.height())
	v, err := await(t, res)
	if err != nil {
		return "", err
	}
	return v[0], nil
}

// MultiSelect lets the user pick one or more choices.
func MultiSelect(title, description string, choices []Choice) ([]string, error) {
	t, err := start()
	if err != nil {
		return nil, err
	}
	res := make(chan []string, 1)
	p := newPicker(title, description, choices, true, "", func(v []string) { res <- v }, t.cancel)
	t.show(p.box, p.filter, keyHelp("↑↓", "move", "space", "mark", "ctrl+a", "all", "enter", "continue", "esc", "cancel"), p.height())
	return await(t, res)
}

// Input asks for a line of text, prefilled with def.
func Input(title, description, def string, validate func(string) error) (string, error) {
	t, err := start()
	if err != nil {
		return "", err
	}
	res := make(chan string, 1)
	field := tview.NewInputField().
		SetLabel("› ").
		SetText(def).
		SetLabelStyle(tcell.StyleDefault.Foreground(colAccent)).
		SetFieldStyle(tcell.StyleDefault.Underline(true))
	msg := text("")
	field.SetChangedFunc(func(string) { msg.SetText("") })
	field.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			v := strings.TrimSpace(field.GetText())
			if validate != nil {
				if err := validate(v); err != nil {
					msg.SetText(tagRed + "✗ " + tview.Escape(err.Error()) + tagReset)
					return
				}
			}
			res <- v
		case tcell.KeyEsc:
			t.cancel()
		}
	})
	p := panel(title)
	height := panelChrome + 3
	if description != "" {
		p.AddItem(text(tagMuted+tview.Escape(description)+tagReset), 2, 0, false)
		height += 2
	}
	p.AddItem(field, 1, 0, true).AddItem(nil, 1, 0, false).AddItem(msg, 0, 1, false)
	t.show(p, field, keyHelp("enter", "accept", "esc", "cancel"), height)
	return await(t, res)
}

// Confirm asks a yes/no question.
func Confirm(title, question string, def bool) (bool, error) {
	t, err := start()
	if err != nil {
		return false, err
	}
	res := make(chan bool, 1)
	form := buttons([]string{"Yes", "No"}, func(i int) { res <- i == 0 }, t.cancel)
	if !def {
		form.SetFocus(1)
	}
	form.SetInputCapture(chain(form.GetInputCapture(), func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Rune() {
		case 's', 'S', 'y', 'Y':
			res <- true
			return nil
		case 'n', 'N':
			res <- false
			return nil
		}
		return ev
	}))
	p := panel(title)
	p.AddItem(text(tview.Escape(question)), 0, 1, false).AddItem(form, 2, 0, true)
	t.show(p, form, keyHelp("←→", "choose", "enter", "accept", "y/n", "yes/no", "esc", "cancel"),
		panelChrome+lines(question)+2)
	return await(t, res)
}

// buttons returns a form with a centered row of buttons.
func buttons(labels []string, pressed func(int), cancel func()) *tview.Form {
	form := tview.NewForm().SetButtonsAlign(tview.AlignCenter)
	form.SetBorderPadding(1, 0, 0, 0)
	form.SetButtonStyle(tcell.StyleDefault.Foreground(tcell.ColorDefault).Background(tcell.NewHexColor(0x2a2f45)))
	form.SetButtonActivatedStyle(tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(colAccent).Bold(true))
	for i, l := range labels {
		form.AddButton(l, func() { pressed(i) })
	}
	form.SetCancelFunc(cancel)
	form.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyLeft:
			return tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone)
		case tcell.KeyRight:
			return tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
		}
		return ev
	})
	return form
}

func chain(first, second func(*tcell.EventKey) *tcell.EventKey) func(*tcell.EventKey) *tcell.EventKey {
	return func(ev *tcell.EventKey) *tcell.EventKey {
		if first != nil {
			if ev = first(ev); ev == nil {
				return nil
			}
		}
		return second(ev)
	}
}

// Change is one line of a review section.
type Change struct {
	Sign  byte // '+' new, '~' updated, '=' unchanged
	Key   string
	Value string
}

// Section groups the changes of one file.
type Section struct {
	Title   string
	Changes []Change
	Notes   []string
}

// ReviewData is what the review screen shows.
type ReviewData struct {
	Status   string // optional banner, e.g. "Everything is up to date"
	Sections []Section
	Warnings []string
	CanWrite bool // show "Write"/"Cancel" instead of "Close"
}

// Review shows the pending changes. It returns true if the user chose to
// write them.
func Review(d ReviewData) (bool, error) {
	t, err := start()
	if err != nil {
		return false, err
	}
	content := renderReview(d)
	tv := text(content)
	tv.SetScrollable(true)
	labels := []string{"Close"}
	if d.CanWrite {
		labels = []string{"Write changes", "Cancel"}
	}
	res := make(chan bool, 1)
	form := buttons(labels, func(i int) { res <- d.CanWrite && i == 0 }, t.cancel)
	if !d.CanWrite {
		form.SetCancelFunc(func() { res <- false })
	}
	form.SetInputCapture(chain(form.GetInputCapture(), func(ev *tcell.EventKey) *tcell.EventKey {
		row, _ := tv.GetScrollOffset()
		switch ev.Key() {
		case tcell.KeyUp:
			tv.ScrollTo(max(row-1, 0), 0)
		case tcell.KeyDown:
			tv.ScrollTo(row+1, 0)
		case tcell.KeyPgUp:
			tv.ScrollTo(max(row-10, 0), 0)
		case tcell.KeyPgDn:
			tv.ScrollTo(row+10, 0)
		default:
			return ev
		}
		return nil
	}))
	p := panel("Review changes")
	p.AddItem(tv, 0, 1, false).AddItem(form, 2, 0, true)
	help := keyHelp("↑↓", "scroll", "←→", "choose", "enter", "accept", "esc", "cancel")
	if !d.CanWrite {
		help = keyHelp("↑↓", "scroll", "enter", "close")
	}
	t.show(p, form, help, panelChrome+lines(content)+2)
	return await(t, res)
}

func renderReview(d ReviewData) string {
	var b strings.Builder
	if d.Status != "" {
		b.WriteString(tagGreen + tagBold + "✔ " + tview.Escape(d.Status) + tagReset + "\n\n")
	}
	for i, s := range d.Sections {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(tagAccent + tagBold + tview.Escape(s.Title) + tagReset + "\n")
		width := 0
		for _, c := range s.Changes {
			width = max(width, len(c.Key))
		}
		for _, c := range s.Changes {
			sign, value := tagGreen+"+", tview.Escape(c.Value)
			switch c.Sign {
			case '~':
				sign = tagYellow + "~"
			case '=':
				sign, value = tagMuted+"=", "unchanged"
			}
			fmt.Fprintf(&b, "  %s%s %-*s  %s%s%s\n", sign, tagReset, width, tview.Escape(c.Key), tagMuted, value, tagReset)
		}
		if len(s.Changes) == 0 {
			b.WriteString(tagMuted + "  (nothing to write)" + tagReset + "\n")
		}
		for _, n := range s.Notes {
			b.WriteString(tagMuted + "  " + tview.Escape(n) + tagReset + "\n")
		}
	}
	if len(d.Warnings) > 0 {
		b.WriteString("\n")
		for _, w := range d.Warnings {
			b.WriteString(tagYellow + "⚠ " + tagReset + tview.Escape(w) + "\n")
		}
	}
	return b.String()
}

// SelectProfile asks the user to pick one of profiles. def is preselected.
func SelectProfile(profiles []string, def string) (string, error) {
	choices := make([]Choice, len(profiles))
	for i, p := range profiles {
		choices[i] = Choice{Label: p, Value: p}
	}
	return Select("AWS profile", "Account to read the secrets from (~/.aws/credentials and ~/.aws/config).", choices, def)
}

// SelectRegion asks the user to pick a region.
func SelectRegion() (string, error) {
	choices := make([]Choice, len(Regions))
	for i, r := range Regions {
		choices[i] = Choice{Label: r, Value: r}
	}
	return Select("AWS region", "The profile does not define a region.", choices, Regions[0])
}
