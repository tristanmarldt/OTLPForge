package main

import (
	"io"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// signalOptions are the three OTLP signal kinds, in emit order.
var signalOptions = []string{"spans", "metrics", "logs"}

// signalsField is a one-line multi-select for the OTLP signal kinds.
//
// huh's MultiSelect always renders one option per line plus a title, costing
// four rows. The Settings tab has to fit a 24-row terminal (huh cannot scroll
// an oversized form), so this implements huh.Field directly to put the whole
// control on a single row:
//
//	Signals          ✓ spans   ✓ metrics   ○ logs
//
// ←/→ move between the toggles, space flips one, enter moves to the next field.
type signalsField struct {
	title   string
	value   *[]string
	cursor  int
	focused bool
	width   int
	theme   *huh.Theme
}

func newSignalsField(title string, value *[]string) *signalsField {
	return &signalsField{title: title, value: value}
}

// ── behaviour ─────────────────────────────────────────────────────────────────

func (f *signalsField) Init() tea.Cmd { return nil }

func (f *signalsField) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return f, nil
	}
	switch k.String() {
	case "left", "h":
		if f.cursor > 0 {
			f.cursor--
		}
	case "right", "l":
		if f.cursor < len(signalOptions)-1 {
			f.cursor++
		}
	case " ", "x":
		f.toggle(signalOptions[f.cursor])
	case "enter", "tab", "down":
		return f, huh.NextField
	case "shift+tab", "up":
		return f, huh.PrevField
	}
	return f, nil
}

func (f *signalsField) selected(sig string) bool {
	for _, s := range *f.value {
		if s == sig {
			return true
		}
	}
	return false
}

// toggle flips one signal. The slice is kept sorted either way so the value
// depends only on which signals are on, never on the order they were clicked —
// unsaved-change detection compares these slices.
func (f *signalsField) toggle(sig string) {
	cur := *f.value
	next := make([]string, 0, len(cur)+1)
	found := false
	for _, s := range cur {
		if s == sig {
			found = true
			continue
		}
		next = append(next, s)
	}
	if !found {
		next = append(next, sig)
	}
	sort.Strings(next)
	*f.value = next
}

// ── rendering ─────────────────────────────────────────────────────────────────

func (f *signalsField) View() string {
	theme := f.theme
	if theme == nil { // WithTheme is normally called by the group; be safe in tests
		theme = huh.ThemeCharm()
	}
	styles := theme.Blurred
	if f.focused {
		styles = theme.Focused
	}

	var parts []string
	for i, opt := range signalOptions {
		mark := "○"
		if f.selected(opt) {
			mark = "✓"
		}
		label := mark + " " + opt

		switch {
		case f.focused && i == f.cursor:
			parts = append(parts, sPrimaryBold.Render("‹"+label+"›"))
		case f.selected(opt):
			parts = append(parts, sSuccess.Render(" "+label+" "))
		default:
			parts = append(parts, sMuted.Render(" "+label+" "))
		}
	}

	line := styles.Title.Render(f.title) + strings.Join(parts, " ")
	return styles.Base.Width(f.width).Render(line)
}

// ── huh.Field plumbing ────────────────────────────────────────────────────────

func (f *signalsField) Blur() tea.Cmd  { f.focused = false; return nil }
func (f *signalsField) Focus() tea.Cmd { f.focused = true; return nil }
func (f *signalsField) Error() error   { return nil }
func (f *signalsField) Run() error     { return nil }
func (f *signalsField) Skip() bool     { return false }
func (f *signalsField) Zoom() bool     { return false }

func (f *signalsField) RunAccessible(io.Writer, io.Reader) error { return nil }

func (f *signalsField) KeyBinds() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←→", "move")),
		key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "next")),
	}
}

func (f *signalsField) WithTheme(t *huh.Theme) huh.Field {
	if f.theme == nil {
		f.theme = t
	}
	return f
}

func (f *signalsField) WithWidth(w int) huh.Field            { f.width = w; return f }
func (f *signalsField) WithHeight(int) huh.Field             { return f }
func (f *signalsField) WithAccessible(bool) huh.Field        { return f }
func (f *signalsField) WithKeyMap(*huh.KeyMap) huh.Field     { return f }
func (f *signalsField) WithPosition(huh.FieldPosition) huh.Field { return f }

func (f *signalsField) GetKey() string { return "signals" }
func (f *signalsField) GetValue() any  { return *f.value }
