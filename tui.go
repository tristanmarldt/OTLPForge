package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// ── styles ────────────────────────────────────────────────────────────────────

var (
	colPrimary = lipgloss.Color("#00aaff")
	colSuccess = lipgloss.Color("#00c875")
	colError   = lipgloss.Color("#ff5c5c")
	colMuted   = lipgloss.Color("#555555")

	sPrimary     = lipgloss.NewStyle().Foreground(colPrimary)
	sPrimaryBold = lipgloss.NewStyle().Foreground(colPrimary).Bold(true)
	sSuccess     = lipgloss.NewStyle().Foreground(colSuccess)
	sError       = lipgloss.NewStyle().Foreground(colError)
	sMuted       = lipgloss.NewStyle().Foreground(colMuted)
	sBold        = lipgloss.NewStyle().Bold(true)
	sHelp        = lipgloss.NewStyle().Foreground(colMuted)
)

// ── messages ──────────────────────────────────────────────────────────────────

type tickMsg struct{}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

// ── screen ────────────────────────────────────────────────────────────────────

type tuiScreen int

const (
	screenList tuiScreen = iota
	screenServiceEdit
	screenAttrsEdit
	screenGlobal
	screenConfirmDelete
)

// ── model ─────────────────────────────────────────────────────────────────────

// tui is the root Bubble Tea model. It uses a pointer receiver throughout so
// that huh form Value() pointers remain stable across Update calls.
type tui struct {
	app    *App
	width  int
	height int

	screen  tuiScreen
	cursor  int
	cfg     Config
	status  RuntimeStatus

	form    *huh.Form
	editIdx int // index in cfg.Services; -1 = new service

	// bound form fields – service editor
	fName            string
	fSpanKind        string
	fFailure         string
	fInterval        string
	fChildSpans      string
	fSignals         []string
	fEnabled         bool
	fAttrs           string
	fDeleteConfirmed bool

	// bound form fields – global settings
	gEndpoint string
	gToken    string

	flash    string
	flashErr bool
	flashEnd time.Time
}

func NewTUIModel(app *App) *tui {
	return &tui{
		app:     app,
		cfg:     app.GetConfig(),
		status:  app.GetStatus(),
		editIdx: -1,
	}
}

// ── tea.Model ─────────────────────────────────────────────────────────────────

func (m *tui) Init() tea.Cmd {
	return tickCmd()
}

func (m *tui) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		m.cfg = m.app.GetConfig()
		m.status = m.app.GetStatus()
		if !m.flashEnd.IsZero() && time.Now().After(m.flashEnd) {
			m.flash = ""
			m.flashEnd = time.Time{}
		}
		return m, tickCmd()
	}

	// delegate all messages to active form
	if m.screen != screenList {
		return m.updateForm(msg)
	}

	if k, ok := msg.(tea.KeyMsg); ok {
		return m.updateList(k)
	}
	return m, nil
}

// ── form delegation ───────────────────────────────────────────────────────────

func (m *tui) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.form == nil {
		m.screen = screenList
		return m, nil
	}
	// Esc always cancels and returns to the list without saving.
	if k, ok := msg.(tea.KeyMsg); ok && k.Type == tea.KeyEsc {
		m.screen = screenList
		m.form = nil
		return m, nil
	}
	newModel, cmd := m.form.Update(msg)
	if f, ok := newModel.(*huh.Form); ok {
		m.form = f
	}
	switch m.form.State {
	case huh.StateCompleted:
		return m.commitForm()
	case huh.StateAborted:
		m.screen = screenList
		m.form = nil
	}
	return m, cmd
}

func (m *tui) commitForm() (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenServiceEdit:
		return m.commitService()
	case screenAttrsEdit:
		return m.commitAttrs()
	case screenGlobal:
		return m.commitGlobal()
	case screenConfirmDelete:
		return m.commitDelete()
	}
	m.screen = screenList
	m.form = nil
	return m, nil
}

// ── list key handling ─────────────────────────────────────────────────────────

func (m *tui) updateList(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	svcs := m.cfg.Services
	switch k.String() {
	case "ctrl+c", "q":
		if m.status.Running {
			m.app.Stop()
		}
		return m, tea.Quit

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}

	case "down", "j":
		if m.cursor < len(svcs)-1 {
			m.cursor++
		}

	case "n":
		return m.openServiceForm(-1)

	case "enter":
		if len(svcs) > 0 {
			return m.openServiceForm(m.cursor)
		}

	case "a":
		if len(svcs) > 0 {
			return m.openAttrsForm(m.cursor)
		}

	case "d":
		if len(svcs) > 0 {
			return m.openDeleteConfirm()
		}

	case " ":
		if len(svcs) > 0 {
			return m.toggleService()
		}

	case "r":
		return m.toggleRunning()

	case "g":
		return m.openGlobalForm()
	}
	return m, nil
}

// ── service form ──────────────────────────────────────────────────────────────

func (m *tui) openServiceForm(idx int) (tea.Model, tea.Cmd) {
	m.editIdx = idx
	if idx == -1 {
		m.fName = ""
		m.fSpanKind = "server"
		m.fFailure = "5"
		m.fInterval = "5"
		m.fChildSpans = "0"
		m.fSignals = []string{"spans", "metrics", "logs"}
		m.fEnabled = true
		m.fAttrs = ""
	} else {
		svc := m.cfg.Services[idx]
		m.fName = svc.Name
		m.fSpanKind = svc.SpanKind
		m.fFailure = strconv.Itoa(svc.FailureRate)
		m.fInterval = strconv.Itoa(svc.Interval)
		m.fChildSpans = strconv.Itoa(svc.ChildSpans)
		if len(svc.Signals) == 0 {
			m.fSignals = []string{"spans", "metrics", "logs"}
		} else {
			m.fSignals = append([]string(nil), svc.Signals...)
		}
		m.fEnabled = svc.Enabled
		m.fAttrs = attrsToText(svc.Attributes)
	}

	w := m.formWidth()
	m.form = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Service name").
				Value(&m.fName).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("name is required")
					}
					return nil
				}),
			huh.NewSelect[string]().
				Title("Span kind").
				Options(
					huh.NewOption("server", "server"),
					huh.NewOption("client", "client"),
					huh.NewOption("internal", "internal"),
					huh.NewOption("producer", "producer"),
					huh.NewOption("consumer", "consumer"),
				).
				Value(&m.fSpanKind),
			huh.NewInput().
				Title("Failure rate (0–100 %)").
				Value(&m.fFailure).
				Validate(func(s string) error {
					n, err := strconv.Atoi(strings.TrimSpace(s))
					if err != nil || n < 0 || n > 100 {
						return fmt.Errorf("must be a number 0–100")
					}
					return nil
				}),
			huh.NewInput().
				Title("Interval (seconds)").
				Value(&m.fInterval).
				Validate(func(s string) error {
					n, err := strconv.Atoi(strings.TrimSpace(s))
					if err != nil || n < 1 {
						return fmt.Errorf("must be ≥ 1")
					}
					return nil
				}),
			huh.NewInput().
				Title("Child spans (0–10)").
				Description("Client spans nested under the root trace span").
				Value(&m.fChildSpans).
				Validate(func(s string) error {
					n, err := strconv.Atoi(strings.TrimSpace(s))
					if err != nil || n < 0 || n > 10 {
						return fmt.Errorf("must be 0–10")
					}
					return nil
				}),
			huh.NewMultiSelect[string]().
				Title("Signals").
				Options(
					huh.NewOption("spans", "spans"),
					huh.NewOption("metrics", "metrics"),
					huh.NewOption("logs", "logs"),
				).
				Value(&m.fSignals),
			huh.NewConfirm().
				Title("Enabled").
				Affirmative("Yes").
				Negative("No").
				Value(&m.fEnabled),
		),
		huh.NewGroup(
			huh.NewText().
				Title("Resource attributes").
				Description("key=value per line · true/false→bool · 42→int · 3.14→double · \"42\"→string · esc: cancel").
				Lines(8).
				Value(&m.fAttrs),
		),
	).WithWidth(w)

	m.screen = screenServiceEdit
	return m, m.form.Init()
}

func (m *tui) commitService() (tea.Model, tea.Cmd) {
	m.screen = screenList
	m.form = nil

	failRate, _ := strconv.Atoi(strings.TrimSpace(m.fFailure))
	interval, _ := strconv.Atoi(strings.TrimSpace(m.fInterval))
	childSpans, _ := strconv.Atoi(strings.TrimSpace(m.fChildSpans))
	signals := m.fSignals
	if len(signals) == 3 {
		signals = nil // all three selected = store as empty (= all enabled)
	}

	svc := Service{
		Name:        strings.TrimSpace(m.fName),
		SpanKind:    m.fSpanKind,
		FailureRate: failRate,
		Interval:    interval,
		ChildSpans:  childSpans,
		Signals:     signals,
		Enabled:     m.fEnabled,
		Attributes:  parseAttrs(m.fAttrs),
	}

	cfg := m.cfg
	if m.editIdx == -1 {
		cfg.Services = append(cfg.Services, svc)
	} else {
		cfg.Services[m.editIdx] = svc
	}

	if err := m.app.SetConfig(cfg); err != nil {
		m.setFlash("error: "+err.Error(), true)
	} else {
		m.cfg = cfg
		m.setFlash("saved", false)
	}
	return m, nil
}

// ── attributes quick-edit form ────────────────────────────────────────────────

func (m *tui) openAttrsForm(idx int) (tea.Model, tea.Cmd) {
	m.editIdx = idx
	svc := m.cfg.Services[idx]
	m.fAttrs = attrsToText(svc.Attributes)

	m.form = huh.NewForm(
		huh.NewGroup(
			huh.NewText().
				Title(fmt.Sprintf("Attributes — %s", svc.Name)).
				Description("key=value per line · true/false→bool · 42→int · 3.14→double · \"42\"→string · esc: cancel").
				Lines(12).
				Value(&m.fAttrs),
		),
	).WithWidth(m.formWidth())

	m.screen = screenAttrsEdit
	return m, m.form.Init()
}

func (m *tui) commitAttrs() (tea.Model, tea.Cmd) {
	m.screen = screenList
	m.form = nil

	svcs := make([]Service, len(m.cfg.Services))
	copy(svcs, m.cfg.Services)
	svcs[m.editIdx].Attributes = parseAttrs(m.fAttrs)

	cfg := m.cfg
	cfg.Services = svcs
	if err := m.app.SetConfig(cfg); err != nil {
		m.setFlash("error: "+err.Error(), true)
	} else {
		m.cfg = cfg
		m.setFlash("attributes saved", false)
	}
	return m, nil
}

// ── global settings form ──────────────────────────────────────────────────────

func (m *tui) openGlobalForm() (tea.Model, tea.Cmd) {
	m.gEndpoint = m.cfg.Endpoint
	m.gToken = ""

	tokenDesc := "Leave blank to keep current token"
	if !m.cfg.hasToken() {
		tokenDesc = "No token currently configured"
	}

	m.form = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("OTLP endpoint").
				Description("e.g. https://xxx.live.dynatrace.com/api/v2/otlp").
				Value(&m.gEndpoint),
			huh.NewInput().
				Title("API token").
				Description(tokenDesc).
				Password(true).
				Value(&m.gToken),
		),
	).WithWidth(m.formWidth())

	m.screen = screenGlobal
	return m, m.form.Init()
}

func (m *tui) commitGlobal() (tea.Model, tea.Cmd) {
	m.screen = screenList
	m.form = nil

	cfg := m.cfg
	cfg.Endpoint = strings.TrimSpace(m.gEndpoint)
	if tok := strings.TrimSpace(m.gToken); tok != "" {
		cfg.Token = tok
	}

	if err := m.app.SetConfig(cfg); err != nil {
		m.setFlash("error: "+err.Error(), true)
	} else {
		m.cfg = cfg
		m.setFlash("settings saved", false)
	}
	return m, nil
}

// ── delete confirm ────────────────────────────────────────────────────────────

func (m *tui) openDeleteConfirm() (tea.Model, tea.Cmd) {
	if m.cursor >= len(m.cfg.Services) {
		return m, nil
	}
	svcName := m.cfg.Services[m.cursor].Name
	m.fDeleteConfirmed = false
	m.form = huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Delete %q?", svcName)).
				Affirmative("Yes, delete").
				Negative("Cancel").
				Value(&m.fDeleteConfirmed),
		),
	).WithWidth(m.formWidth())
	m.screen = screenConfirmDelete
	return m, m.form.Init()
}

func (m *tui) commitDelete() (tea.Model, tea.Cmd) {
	m.screen = screenList
	m.form = nil

	if !m.fDeleteConfirmed {
		return m, nil
	}

	cfg := m.cfg
	if m.cursor < len(cfg.Services) {
		name := cfg.Services[m.cursor].Name
		cfg.Services = append(cfg.Services[:m.cursor], cfg.Services[m.cursor+1:]...)
		if m.cursor >= len(cfg.Services) && m.cursor > 0 {
			m.cursor--
		}
		if err := m.app.SetConfig(cfg); err != nil {
			m.setFlash("error: "+err.Error(), true)
		} else {
			m.cfg = cfg
			m.setFlash("deleted "+name, false)
		}
	}
	return m, nil
}

// ── toggles ───────────────────────────────────────────────────────────────────

func (m *tui) toggleService() (tea.Model, tea.Cmd) {
	if m.cursor >= len(m.cfg.Services) {
		return m, nil
	}
	svcs := make([]Service, len(m.cfg.Services))
	copy(svcs, m.cfg.Services)
	svcs[m.cursor].Enabled = !svcs[m.cursor].Enabled

	cfg := m.cfg
	cfg.Services = svcs
	if err := m.app.SetConfig(cfg); err != nil {
		m.setFlash("error: "+err.Error(), true)
	} else {
		m.cfg = cfg
	}
	return m, nil
}

func (m *tui) toggleRunning() (tea.Model, tea.Cmd) {
	if m.status.Running {
		m.app.Stop()
		m.setFlash("stopped", false)
	} else {
		if err := m.app.Start(); err != nil {
			m.setFlash("error: "+err.Error(), true)
		} else {
			m.setFlash("started", false)
		}
	}
	return m, nil
}

// ── flash ─────────────────────────────────────────────────────────────────────

func (m *tui) setFlash(msg string, isErr bool) {
	m.flash = msg
	m.flashErr = isErr
	m.flashEnd = time.Now().Add(3 * time.Second)
}

// ── view ──────────────────────────────────────────────────────────────────────

func (m *tui) View() string {
	if m.width == 0 {
		return "Loading…"
	}
	if m.form != nil && m.screen != screenList {
		return m.form.View()
	}
	return m.listView()
}

func (m *tui) listView() string {
	var rows []string

	rows = append(rows, m.renderHeader())
	rows = append(rows, "")

	if len(m.cfg.Services) == 0 {
		rows = append(rows, sMuted.Render("  No services. Press n to add one."))
	} else {
		for i, svc := range m.cfg.Services {
			rows = append(rows, m.renderService(i, svc))
		}
	}

	if m.flash != "" {
		rows = append(rows, "")
		if m.flashErr {
			rows = append(rows, sError.Render("  ✗ "+m.flash))
		} else {
			rows = append(rows, sSuccess.Render("  ✓ "+m.flash))
		}
	}

	body := strings.Join(rows, "\n")

	// pad so help line sits at the bottom of the terminal
	lineCount := strings.Count(body, "\n") + 1
	targetLine := m.height - 2
	if lineCount < targetLine {
		body += strings.Repeat("\n", targetLine-lineCount)
	}
	return body + "\n" + m.renderHelp()
}

func (m *tui) renderHeader() string {
	var indicator string
	if m.status.Running {
		indicator = sSuccess.Render("● running")
	} else {
		indicator = sMuted.Render("○ stopped")
	}
	title := sPrimaryBold.Render("otgen") + sMuted.Render(" v"+version)
	line1 := title + "  " + indicator

	ep := m.cfg.Endpoint
	if ep == "" {
		ep = sMuted.Italic(true).Render("no endpoint — press g to configure")
	} else {
		ep = sMuted.Render(ep)
	}
	line2 := "  " + ep

	sep := sMuted.Render(strings.Repeat("─", min(m.width, 72)))
	return line1 + "\n" + line2 + "\n" + sep
}

func (m *tui) renderService(i int, svc Service) string {
	cursor := "  "
	if i == m.cursor {
		cursor = sPrimary.Render("▶ ")
	}

	var dot string
	if svc.Enabled {
		dot = sSuccess.Render("●")
	} else {
		dot = sMuted.Render("○")
	}

	name := svc.Name
	if i == m.cursor {
		name = sBold.Render(name)
	}

	kind := sMuted.Render(svc.SpanKind)
	errRate := sMuted.Render(fmt.Sprintf("%d%% err", svc.FailureRate))
	interval := sMuted.Render(fmt.Sprintf("%ds", svc.Interval))
	var children string
	if svc.ChildSpans > 0 {
		children = "  " + sMuted.Render(fmt.Sprintf("+%d child", svc.ChildSpans))
	}
	row1 := fmt.Sprintf("%s%s %s  %s  %s  %s%s", cursor, dot, name, kind, errRate, interval, children)

	// live signal counters
	ss := m.status.Services[svc.Name]
	var parts []string
	if svc.hasSignal(signalSpans) {
		parts = append(parts, fmt.Sprintf("spans↑%d", ss.Spans.SentCount))
	}
	if svc.hasSignal(signalMetrics) {
		parts = append(parts, fmt.Sprintf("metrics↑%d", ss.Metrics.SentCount))
	}
	if svc.hasSignal(signalLogs) {
		parts = append(parts, fmt.Sprintf("logs↑%d", ss.Logs.SentCount))
	}
	// surface first non-empty error
	for _, s := range []SignalStatus{ss.Spans, ss.Metrics, ss.Logs} {
		if s.LastError != "" {
			parts = append(parts, sError.Render("! "+truncate(s.LastError, 48)))
			break
		}
	}

	row2 := "    " + sMuted.Render(strings.Join(parts, "  "))
	return row1 + "\n" + row2
}

func (m *tui) renderHelp() string {
	keys := []string{"n new", "↵ edit", "a attrs", "d delete", "␣ toggle", "r run/stop", "g settings", "q quit"}
	return sHelp.Render("  " + strings.Join(keys, "  ·  "))
}

func (m *tui) formWidth() int {
	w := m.width - 4
	if w > 80 {
		w = 80
	}
	if w < 40 {
		w = 40
	}
	return w
}

// ── attribute text helpers ────────────────────────────────────────────────────

// attrsToText serialises a map[string]AttrValue to a human-editable
// "key=value" text (one entry per line, sorted by key).
//
// Type encoding:
//   - bool   → true / false
//   - int    → bare integer, e.g. 42
//   - double → always includes a decimal point, e.g. 3.14 or 1.0
//   - string → value as-is, but quoted with "" when it would be
//     misidentified as another type on re-parse.
func attrsToText(attrs map[string]AttrValue) string {
	if len(attrs) == 0 {
		return ""
	}
	lines := make([]string, 0, len(attrs))
	for k, v := range attrs {
		var val string
		switch v.Type {
		case "bool":
			if v.Bool {
				val = "true"
			} else {
				val = "false"
			}
		case "int":
			val = strconv.FormatInt(v.Int, 10)
		case "double":
			val = strconv.FormatFloat(v.Double, 'f', -1, 64)
			if !strings.Contains(val, ".") {
				val += ".0" // ensure it round-trips as double, not int
			}
		default:
			val = v.Str
			if attrStrNeedsQuoting(val) {
				val = `"` + val + `"`
			}
		}
		lines = append(lines, k+"="+val)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// attrStrNeedsQuoting reports whether a string value must be wrapped in ""
// so that parseAttrs can distinguish it from bool/int/double.
func attrStrNeedsQuoting(s string) bool {
	if s == "" {
		return false
	}
	lower := strings.ToLower(s)
	if lower == "true" || lower == "false" {
		return true
	}
	// Looks like a plain integer?
	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		return true
	}
	// Looks like a float?
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return true
	}
	return false
}

// parseAttrs parses "key=value" lines back into a typed AttrValue map.
//
// Type detection order (per value):
//  1. true / false (case-insensitive) → bool
//  2. "…" (double-quoted) → string (quotes stripped)
//  3. All digits with optional leading - → int64
//  4. Digits + decimal point → double
//  5. Everything else → string
func parseAttrs(text string) map[string]AttrValue {
	attrs := make(map[string]AttrValue)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		k := strings.TrimSpace(line[:eq])
		v := strings.TrimSpace(line[eq+1:])
		if k == "" {
			continue
		}
		attrs[k] = parseAttrValue(v)
	}
	return attrs
}

func parseAttrValue(v string) AttrValue {
	// 1. Boolean
	switch strings.ToLower(v) {
	case "true":
		return boolAttrVal(true)
	case "false":
		return boolAttrVal(false)
	}
	// 2. Quoted string – strip the surrounding quotes.
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		return strAttrVal(v[1 : len(v)-1])
	}
	// 3 & 4. Number: walk the rune set to distinguish int from double.
	if looksNumeric(v) {
		if strings.ContainsAny(v, ".eE") {
			if fv, err := strconv.ParseFloat(v, 64); err == nil {
				return doubleAttrVal(fv)
			}
		} else {
			if iv, err := strconv.ParseInt(v, 10, 64); err == nil {
				return intAttrVal(iv)
			}
		}
	}
	// 5. Fallback: string
	return strAttrVal(v)
}

// looksNumeric returns true if s starts with an optional minus sign followed
// by at least one ASCII digit.
func looksNumeric(s string) bool {
	if s == "" {
		return false
	}
	i := 0
	if s[0] == '-' {
		i = 1
	}
	if i >= len(s) {
		return false
	}
	return unicode.IsDigit(rune(s[i]))
}

// ── misc ──────────────────────────────────────────────────────────────────────

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
