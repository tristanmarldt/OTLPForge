package main

import (
	"io"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// Single-row form controls.
//
// huh renders Select and MultiSelect as a title line plus one line per option,
// which is far too tall for the Settings tab: that tab has to fit inside the
// terminal because huh cannot scroll a form that overflows its group. These
// implement huh.Field directly so each control occupies one row:
//
//	Span kind        ‹server›  client  internal  producer  consumer
//	Signals          ✓ spans   ○ metrics   ✓ logs
//
// ←/→ move, space toggles (multi-select only), enter/tab hand control back to
// huh through the exported NextField / PrevField.

// ── shared base ───────────────────────────────────────────────────────────────

// inlineField holds the state and huh.Field boilerplate common to the controls
// below. huh calls the With* setters for their side effects and discards the
// return value (see group.go), so promoting them from an embedded struct is safe.
type inlineField struct {
	// self points at the embedding field so the promoted With* setters can
	// return the concrete control rather than this base, which does not
	// satisfy huh.Field on its own. Constructors must set it.
	self    huh.Field
	title   string
	width   int
	focused bool
	theme   *huh.Theme
}

func (f *inlineField) Init() tea.Cmd  { return nil }
func (f *inlineField) Blur() tea.Cmd  { f.focused = false; return nil }
func (f *inlineField) Focus() tea.Cmd { f.focused = true; return nil }
func (f *inlineField) Error() error   { return nil }
func (f *inlineField) Run() error     { return nil }
func (f *inlineField) Skip() bool     { return false }
func (f *inlineField) Zoom() bool     { return false }

func (f *inlineField) RunAccessible(io.Writer, io.Reader) error { return nil }

func (f *inlineField) WithWidth(w int) huh.Field                { f.width = w; return f.self }
func (f *inlineField) WithHeight(int) huh.Field                 { return f.self }
func (f *inlineField) WithAccessible(bool) huh.Field            { return f.self }
func (f *inlineField) WithKeyMap(*huh.KeyMap) huh.Field         { return f.self }
func (f *inlineField) WithPosition(huh.FieldPosition) huh.Field { return f.self }

func (f *inlineField) WithTheme(t *huh.Theme) huh.Field {
	if f.theme == nil {
		f.theme = t
	}
	return f.self
}

func (f *inlineField) KeyBinds() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←→", "change")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "next")),
	}
}

func (f *inlineField) styles() *huh.FieldStyles {
	theme := f.theme
	if theme == nil { // WithTheme is normally called by the group; be safe in tests
		theme = huh.ThemeCharm()
	}
	if f.focused {
		return &theme.Focused
	}
	return &theme.Blurred
}

// render lays the chips out on one row, falling back to compact when the full
// set would not fit the field width.
func (f *inlineField) render(chips []string, compact string) string {
	styles := f.styles()
	line := styles.Title.Render(f.title) + strings.Join(chips, " ")
	if f.width > 0 && lipgloss.Width(line) > f.width && compact != "" {
		line = styles.Title.Render(f.title) + compact
	}
	return styles.Base.Width(f.width).Render(line)
}

// chip renders one option. cursored wins over selected so the caret is always
// visible on the focused entry.
func chip(label string, selected, cursored bool) string {
	switch {
	case cursored:
		return sPrimaryBold.Render("‹" + label + "›")
	case selected:
		return sSuccess.Render(" " + label + " ")
	default:
		return sMuted.Render(" " + label + " ")
	}
}

// ── single choice ─────────────────────────────────────────────────────────────

// choiceField is a one-row single-select. Moving the cursor changes the value
// directly, the way a segmented control behaves.
type choiceField struct {
	inlineField
	key     string
	value   *string
	options []string
	cursor  int
}

func newChoiceField(name, title string, value *string, options []string) *choiceField {
	f := &choiceField{key: name, value: value, options: options}
	f.self, f.title = f, title
	f.syncCursor()
	return f
}

// syncCursor points the cursor at the current value, so reopening the form
// starts from what is actually selected.
func (f *choiceField) syncCursor() {
	for i, o := range f.options {
		if o == *f.value {
			f.cursor = i
			return
		}
	}
	f.cursor = 0
	if len(f.options) > 0 && *f.value == "" {
		*f.value = f.options[0]
	}
}

func (f *choiceField) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return f, nil
	}
	switch k.String() {
	case "left", "h":
		if f.cursor > 0 {
			f.cursor--
			*f.value = f.options[f.cursor]
		}
	case "right", "l":
		if f.cursor < len(f.options)-1 {
			f.cursor++
			*f.value = f.options[f.cursor]
		}
	case "enter", "tab", "down":
		return f, huh.NextField
	case "shift+tab", "up":
		return f, huh.PrevField
	}
	return f, nil
}

func (f *choiceField) View() string {
	chips := make([]string, 0, len(f.options))
	for _, opt := range f.options {
		selected := opt == *f.value
		chips = append(chips, chip(opt, selected, f.focused && selected))
	}
	return f.render(chips, chip(*f.value, true, f.focused))
}

func (f *choiceField) GetKey() string { return f.key }
func (f *choiceField) GetValue() any  { return *f.value }

// ── multiple choice ───────────────────────────────────────────────────────────

// signalOptions are the three OTLP signal kinds, in emit order.
var signalOptions = []string{"spans", "metrics", "logs"}

// spanKindOptions are the OTLP span kinds, ordered by how often they are used
// for a synthetic service. Must stay in sync with mapSpanKind in otlp.go and
// the validateConfig check in config.go.
var spanKindOptions = []string{"server", "client", "internal", "producer", "consumer"}

// signalsField is a one-row multi-select over the OTLP signal kinds.
type signalsField struct {
	inlineField
	value  *[]string
	cursor int
}

func newSignalsField(title string, value *[]string) *signalsField {
	f := &signalsField{value: value}
	f.self, f.title = f, title
	return f
}

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

func (f *signalsField) KeyBinds() []key.Binding {
	return append(f.inlineField.KeyBinds(),
		key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle")))
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

func (f *signalsField) View() string {
	chips := make([]string, 0, len(signalOptions))
	for i, opt := range signalOptions {
		mark := "○"
		if f.selected(opt) {
			mark = "✓"
		}
		chips = append(chips, chip(mark+" "+opt, f.selected(opt), f.focused && i == f.cursor))
	}

	compact := "none"
	if on := *f.value; len(on) > 0 {
		compact = strings.Join(on, ",")
	}
	return f.render(chips, sSuccess.Render(" "+compact+" "))
}

func (f *signalsField) GetKey() string { return "signals" }
func (f *signalsField) GetValue() any  { return *f.value }
