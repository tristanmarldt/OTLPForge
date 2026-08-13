package main

import (
	"fmt"
	"reflect"
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
	colWarn    = lipgloss.Color("#e6a700")
	colMuted   = lipgloss.Color("#555555")

	sPrimary     = lipgloss.NewStyle().Foreground(colPrimary)
	sPrimaryBold = lipgloss.NewStyle().Foreground(colPrimary).Bold(true)
	sSuccess     = lipgloss.NewStyle().Foreground(colSuccess)
	sError       = lipgloss.NewStyle().Foreground(colError)
	sWarn        = lipgloss.NewStyle().Foreground(colWarn)
	sMuted       = lipgloss.NewStyle().Foreground(colMuted)
	sBold        = lipgloss.NewStyle().Bold(true)
	sHelp        = lipgloss.NewStyle().Foreground(colMuted)
)

// ── messages ──────────────────────────────────────────────────────────────────

type tickMsg struct{}

type testResultMsg struct{ err error }

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

func testConnCmd(app *App) tea.Cmd {
	return func() tea.Msg { return testResultMsg{err: app.TestConnection()} }
}

// ── screens ───────────────────────────────────────────────────────────────────

type tuiScreen int

const (
	screenList tuiScreen = iota
	screenServiceEdit
	screenGlobal
	screenConfirmDelete
	screenConfirmDiscard
	screenHelp
)

// editMode determines what happens when a service-editor tab is completed.
type editMode int

const (
	// modeQuick: opened with 1-5 from the list — completing saves and returns
	// straight to the list.
	modeQuick editMode = iota
	// modeSelector: opened from the tab selector — completing saves and returns
	// to the selector so more tabs can be edited.
	modeSelector
	// modeWizard: new service — completing advances to the next tab; the last
	// tab saves.
	modeWizard
)

var serviceTabNames = []string{"Settings", "Span template", "Infra template", "Resource attrs", "Span attrs"}

var globalTabNames = []string{"Connection", "Global attributes"}

// ── model ─────────────────────────────────────────────────────────────────────

// tui is the root Bubble Tea model. It uses a pointer receiver throughout so
// that huh form Value() pointers remain stable across Update calls.
type tui struct {
	app    *App
	width  int
	height int

	screen tuiScreen
	cursor int
	cfg    Config
	status RuntimeStatus

	form *huh.Form

	// service editor state
	editIdx   int  // index in cfg.Services; -1 = new service
	editTab   int  // active tab (0–4)
	tabActive bool // true = tab selector focused; false = huh form focused
	mode      editMode
	origSvc   Service // snapshot used for unsaved-change detection

	// bound form fields – service editor
	fName          string
	fTemplate      string
	fInfraTemplate string
	fSpanKind      string
	fFailure       string
	fInterval      string
	fChildSpans    string
	fSignals       []string
	fEnabled       bool
	fAttrs         string
	fSpanAttrs     string

	// The exact text auto-seeded from a template. When the textarea still
	// matches its seed the value is considered "untouched", so it is saved as
	// an empty map and the service keeps following the template.
	fAttrsSeed     string
	fSpanAttrsSeed string

	fDeleteConfirmed  bool
	fDiscardConfirmed bool

	// global config state
	globalIdx    int
	globalActive bool // true = global selector focused
	gEndpoint    string
	gToken       string
	gAttrs       string

	testing bool

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

	case testResultMsg:
		m.testing = false
		if msg.err != nil {
			m.setFlash("connection failed: "+msg.err.Error(), true)
		} else {
			m.setFlash("connection OK — endpoint accepted a test span", false)
		}
		return m, nil
	}

	switch m.screen {
	case screenHelp:
		if _, ok := msg.(tea.KeyMsg); ok {
			m.screen = screenList
		}
		return m, nil

	case screenServiceEdit:
		if m.tabActive {
			return m.updateServiceSelector(msg)
		}
		return m.updateForm(msg)

	case screenGlobal:
		if m.globalActive {
			return m.updateGlobalSelector(msg)
		}
		return m.updateForm(msg)

	case screenList:
		if k, ok := msg.(tea.KeyMsg); ok {
			return m.updateList(k)
		}
		return m, nil
	}

	return m.updateForm(msg)
}

// ── form delegation ───────────────────────────────────────────────────────────

func (m *tui) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.form == nil {
		m.screen = screenList
		return m, nil
	}

	if k, ok := msg.(tea.KeyMsg); ok {
		// Esc leaves the form. Inside a tabbed editor it returns to that
		// editor's selector (field values survive); elsewhere to the list.
		if k.Type == tea.KeyEsc {
			return m.leaveForm()
		}
		// Switch tabs without leaving the form.
		//
		// ctrl+<digit> is deliberately not used: terminals cannot transmit it
		// (ctrl+1 sends nothing, ctrl+3 arrives as Esc) and bubbletea has no
		// key for it. ctrl+o is free — huh and bubbles between them claim
		// ctrl+a/b/c/d/e/f/h/j/k/m/n/p/t/u/v/w — and F-keys work wherever the
		// terminal sends them.
		if tabs := m.currentTabs(); tabs != nil {
			switch k.String() {
			case "ctrl+o":
				return m.switchTab((m.currentTab() + 1) % len(tabs))
			case "ctrl+r":
				return m.switchTab((m.currentTab() - 1 + len(tabs)) % len(tabs))
			case "f1", "f2", "f3", "f4", "f5":
				if i := int(k.String()[1] - '1'); i < len(tabs) {
					return m.switchTab(i)
				}
				return m, nil
			}
		}
	}

	newModel, cmd := m.form.Update(msg)
	if f, ok := newModel.(*huh.Form); ok {
		m.form = f
	}
	switch m.form.State {
	case huh.StateCompleted:
		return m.commitForm()
	case huh.StateAborted:
		return m.leaveForm()
	}
	return m, cmd
}

// currentTabs returns the tab set of the screen currently showing a form,
// or nil when the screen is not tabbed.
func (m *tui) currentTabs() []string {
	switch m.screen {
	case screenServiceEdit:
		return serviceTabNames
	case screenGlobal:
		return globalTabNames
	}
	return nil
}

func (m *tui) currentTab() int {
	if m.screen == screenGlobal {
		return m.globalIdx
	}
	return m.editTab
}

// switchTab jumps straight to another tab of the open editor, keeping the
// field values entered so far.
func (m *tui) switchTab(idx int) (tea.Model, tea.Cmd) {
	if m.screen == screenGlobal {
		m.globalIdx = idx
		m.form = m.makeGlobalTabForm(idx)
		return m, m.form.Init()
	}
	m.editTab = idx
	m.openServiceTab(idx)
	return m, m.form.Init()
}

// leaveForm handles Esc / abort out of an open form.
func (m *tui) leaveForm() (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenServiceEdit:
		m.tabActive = true
		m.form = nil
	case screenGlobal:
		m.globalActive = true
		m.form = nil
	default:
		m.screen = screenList
		m.form = nil
	}
	return m, nil
}

func (m *tui) commitForm() (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenServiceEdit:
		switch m.mode {
		case modeWizard:
			if m.editTab < len(serviceTabNames)-1 {
				m.editTab++
				m.openServiceTab(m.editTab)
				return m, m.form.Init()
			}
			return m.commitService(true)
		case modeSelector:
			// Save immediately, then return to the selector for further edits.
			return m.commitService(false)
		default: // modeQuick
			return m.commitService(true)
		}
	case screenGlobal:
		return m.commitGlobalTab()
	case screenConfirmDelete:
		return m.commitDelete()
	case screenConfirmDiscard:
		return m.commitDiscard()
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
		// New service: guided walk through every tab.
		m.loadServiceFields(-1)
		m.mode = modeWizard
		m.tabActive = false
		m.editTab = 0
		m.screen = screenServiceEdit
		m.openServiceTab(0)
		return m, m.form.Init()

	case "enter":
		// Existing service: open the tab selector.
		if len(svcs) > 0 {
			m.loadServiceFields(m.cursor)
			m.mode = modeSelector
			m.tabActive = true
			m.editTab = 0
			m.form = nil
			m.screen = screenServiceEdit
			return m, nil
		}

	case "1", "2", "3", "4", "5":
		// Jump straight into one tab; completing it saves and returns here.
		if len(svcs) > 0 {
			m.loadServiceFields(m.cursor)
			m.mode = modeQuick
			m.tabActive = false
			m.editTab = int(k.Runes[0] - '1')
			m.screen = screenServiceEdit
			m.openServiceTab(m.editTab)
			return m, m.form.Init()
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

	case "t":
		if m.testing {
			return m, nil
		}
		m.testing = true
		m.setFlash("testing connection…", false)
		return m, testConnCmd(m.app)

	case "g":
		m.globalActive = true
		m.globalIdx = 0
		m.form = nil
		m.screen = screenGlobal
		return m, nil

	case "?":
		m.screen = screenHelp
		return m, nil
	}
	return m, nil
}

// ── service editor: field loading & template seeding ──────────────────────────

func (m *tui) loadServiceFields(idx int) {
	m.editIdx = idx
	if idx == -1 {
		m.fName = "otgen-"
		m.fTemplate = ""
		m.fInfraTemplate = ""
		m.fSpanKind = "server"
		m.fFailure = "5"
		m.fInterval = "5"
		m.fChildSpans = "0"
		m.fSignals = []string{"logs", "metrics", "spans"}
		m.fEnabled = true
		m.fAttrs = ""
		m.fAttrsSeed = ""
		m.fSpanAttrs = ""
		m.fSpanAttrsSeed = ""
	} else {
		svc := m.cfg.Services[idx]
		m.fName = svc.Name
		m.fTemplate = svc.Template
		m.fInfraTemplate = svc.InfraTemplate
		m.fSpanKind = svc.SpanKind
		m.fFailure = strconv.Itoa(svc.FailureRate)
		m.fInterval = strconv.Itoa(svc.Interval)
		m.fChildSpans = strconv.Itoa(svc.ChildSpans)
		if len(svc.Signals) == 0 {
			m.fSignals = []string{"logs", "metrics", "spans"}
		} else {
			m.fSignals = append([]string(nil), svc.Signals...)
			sort.Strings(m.fSignals)
		}
		m.fEnabled = svc.Enabled

		// Attributes explicitly saved by the user are shown as-is (no seed, so
		// they are always persisted). Otherwise the template defaults are shown
		// and recorded as the seed, so leaving them untouched keeps the service
		// following the template instead of baking the values in.
		if len(svc.Attributes) > 0 {
			m.fAttrs = attrsToText(svc.Attributes)
			m.fAttrsSeed = ""
		} else {
			m.fAttrs = attrsToText(infraDefaults(svc))
			m.fAttrsSeed = m.fAttrs
		}
		if len(svc.SpanAttrs) > 0 {
			m.fSpanAttrs = attrsToText(svc.SpanAttrs)
			m.fSpanAttrsSeed = ""
		} else {
			m.fSpanAttrs = attrsToText(templateDefaults(svc.Template))
			m.fSpanAttrsSeed = m.fSpanAttrs
		}
	}
	m.syncTemplateSeeds()
	m.origSvc = m.buildServiceFromFields()
}

// syncTemplateSeeds refreshes the attribute textareas after a template change.
// A textarea that still matches its previous seed is re-seeded from the new
// template; one the user has edited is left alone.
func (m *tui) syncTemplateSeeds() {
	probe := Service{Name: strings.TrimSpace(m.fName), InfraTemplate: m.fInfraTemplate}
	newSeed := attrsToText(infraDefaults(probe))
	if strings.TrimSpace(m.fAttrs) == strings.TrimSpace(m.fAttrsSeed) {
		m.fAttrs = newSeed
	}
	m.fAttrsSeed = newSeed

	newSpanSeed := attrsToText(templateDefaults(m.fTemplate))
	if strings.TrimSpace(m.fSpanAttrs) == strings.TrimSpace(m.fSpanAttrsSeed) {
		m.fSpanAttrs = newSpanSeed
	}
	m.fSpanAttrsSeed = newSpanSeed
}

// buildServiceFromFields materialises the editor state into a Service.
// Attribute maps are left empty when the textarea still matches its template
// seed, so the service keeps tracking the template rather than freezing a copy.
func (m *tui) buildServiceFromFields() Service {
	failRate, _ := strconv.Atoi(strings.TrimSpace(m.fFailure))
	interval, _ := strconv.Atoi(strings.TrimSpace(m.fInterval))
	childSpans, _ := strconv.Atoi(strings.TrimSpace(m.fChildSpans))

	signals := append([]string(nil), m.fSignals...)
	sort.Strings(signals)
	if len(signals) == 3 {
		signals = nil // all three = store empty (= all enabled)
	}

	attrs := parseAttrs(m.fAttrs)
	if m.fAttrsSeed != "" && strings.TrimSpace(m.fAttrs) == strings.TrimSpace(m.fAttrsSeed) {
		attrs = map[string]AttrValue{}
	}
	spanAttrs := parseAttrs(m.fSpanAttrs)
	if m.fSpanAttrsSeed != "" && strings.TrimSpace(m.fSpanAttrs) == strings.TrimSpace(m.fSpanAttrsSeed) {
		spanAttrs = map[string]AttrValue{}
	}

	return normalizeService(Service{
		Name:          strings.TrimSpace(m.fName),
		Template:      m.fTemplate,
		InfraTemplate: m.fInfraTemplate,
		SpanKind:      m.fSpanKind,
		FailureRate:   failRate,
		Interval:      interval,
		ChildSpans:    childSpans,
		Signals:       signals,
		Enabled:       m.fEnabled,
		Attributes:    attrs,
		SpanAttrs:     spanAttrs,
	})
}

// hasUnsavedChanges reports whether the editor differs from the last saved state.
func (m *tui) hasUnsavedChanges() bool {
	return !reflect.DeepEqual(m.buildServiceFromFields(), m.origSvc)
}

// ── service editor: forms ─────────────────────────────────────────────────────

const attrTypeHint = "alt+enter new line · enter save · key=value per line · " +
	"true/false→bool · 42→int · 3.14→double · \"42\"→string"

func (m *tui) openServiceTab(tabIdx int) {
	m.syncTemplateSeeds()
	m.form = m.makeServiceTabForm(tabIdx)
}

func (m *tui) makeServiceTabForm(tabIdx int) *huh.Form {
	w := m.formWidth()
	switch tabIdx {
	case 0: // Settings
		// Inline fields keep this tab inside a 24-row terminal. huh cannot
		// scroll a form that overflows (see makeServiceTabForm case 1), so the
		// whole group has to fit. Titles are padded to a common width so the
		// values line up.
		return huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title(settingsLabel("Service name")).
					Inline(true).
					Value(&m.fName).
					Validate(func(s string) error {
						if strings.TrimSpace(s) == "" {
							return fmt.Errorf("name is required")
						}
						return nil
					}),
				huh.NewInput().
					Title(settingsLabel("Interval (s)")).
					Inline(true).
					Value(&m.fInterval).
					Validate(func(s string) error {
						n, err := strconv.Atoi(strings.TrimSpace(s))
						if err != nil || n < 1 {
							return fmt.Errorf("must be ≥ 1")
						}
						return nil
					}),
				huh.NewInput().
					Title(settingsLabel("Failure rate %")).
					Inline(true).
					Value(&m.fFailure).
					Validate(func(s string) error {
						n, err := strconv.Atoi(strings.TrimSpace(s))
						if err != nil || n < 0 || n > 100 {
							return fmt.Errorf("must be a number 0–100")
						}
						return nil
					}),
				huh.NewInput().
					Title(settingsLabel("Child spans")).
					Inline(true).
					Value(&m.fChildSpans).
					Validate(func(s string) error {
						n, err := strconv.Atoi(strings.TrimSpace(s))
						if err != nil || n < 0 || n > 10 {
							return fmt.Errorf("must be 0–10")
						}
						return nil
					}),
				huh.NewSelect[string]().
					Title(settingsLabel("Span kind")).
					Inline(true).
					Options(
						huh.NewOption("server", "server"),
						huh.NewOption("client", "client"),
						huh.NewOption("internal", "internal"),
						huh.NewOption("producer", "producer"),
						huh.NewOption("consumer", "consumer"),
					).
					Value(&m.fSpanKind),
				huh.NewConfirm().
					Title(settingsLabel("Enabled")).
					Inline(true).
					Affirmative("Yes").
					Negative("No").
					Value(&m.fEnabled),
				newSignalsField(settingsLabel("Signals"), &m.fSignals),
			),
		).WithWidth(w)

	case 1: // Span template
		// No .Height() here: huh v1.0.0 pins viewport.YOffset to the selected
		// index on every Update when a height is set, so the list scrolls under
		// a stationary cursor. Each select gets its own tab instead, short
		// enough to render whole.
		return huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Span template").
					Description("OTel semantic-convention attributes on each span · / to filter").
					Options(
						huh.NewOption("None (generic)", ""),
						huh.NewOption("HTTP · server", "http-server"),
						huh.NewOption("HTTP · client", "http-client"),
						huh.NewOption("Database (db.*)", "db"),
						huh.NewOption("Messaging (Kafka / RabbitMQ / SQS)", "messaging"),
						huh.NewOption("gRPC", "grpc"),
					).
					Value(&m.fTemplate),
			),
		).WithWidth(w)

	case 2: // Infrastructure template
		return huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Infrastructure template").
					Description("Resource attributes for the deployment environment · / to filter").
					Options(
						huh.NewOption("None", ""),
						huh.NewOption("Kubernetes · vanilla", "k8s"),
						huh.NewOption("Kubernetes · Amazon EKS", "eks"),
						huh.NewOption("Kubernetes · Google GKE", "gke"),
						huh.NewOption("Kubernetes · Azure AKS", "aks"),
						huh.NewOption("Kubernetes · Red Hat OpenShift", "openshift"),
						huh.NewOption("Container · Docker", "docker"),
						huh.NewOption("Container · containerd", "containerd"),
						huh.NewOption("Container · Amazon ECS / Fargate", "ecs"),
						huh.NewOption("Container · Azure Container Apps", "azure-container-apps"),
						huh.NewOption("Serverless · AWS Lambda", "lambda"),
						huh.NewOption("Serverless · Azure Functions", "azure-functions"),
						huh.NewOption("Serverless · Google Cloud Functions", "gcp-functions"),
						huh.NewOption("Host · VM / bare metal", "host"),
						huh.NewOption("Host · process", "process"),
						huh.NewOption("Scheduler · HashiCorp Nomad", "nomad"),
						huh.NewOption("PaaS · Cloud Foundry / Tanzu", "cloudfoundry"),
					).
					Value(&m.fInfraTemplate),
			),
		).WithWidth(w)

	case 3: // Resource attrs
		desc := attrTypeHint
		if m.fAttrsSeed != "" && strings.TrimSpace(m.fAttrs) == strings.TrimSpace(m.fAttrsSeed) {
			desc = "Showing " + templateLabel(m.fInfraTemplate) + " defaults — edit to override · " + attrTypeHint
		}
		return huh.NewForm(
			huh.NewGroup(
				huh.NewText().
					Title("Resource attributes").
					Description(desc).
					Lines(m.textLines()).
					Value(&m.fAttrs),
			),
		).WithWidth(w)

	default: // 4 — Span attrs
		desc := attrTypeHint
		if m.fSpanAttrsSeed != "" && strings.TrimSpace(m.fSpanAttrs) == strings.TrimSpace(m.fSpanAttrsSeed) {
			desc = "Showing " + templateLabel(m.fTemplate) + " defaults — edit to override · " + attrTypeHint
		}
		return huh.NewForm(
			huh.NewGroup(
				huh.NewText().
					Title("Span attribute overrides").
					Description(desc).
					Lines(m.textLines()).
					Value(&m.fSpanAttrs),
			),
		).WithWidth(w)
	}
}

// settingsLabel pads a Settings-tab title so the inline values line up.
func settingsLabel(s string) string {
	const width = 16
	if n := width - len([]rune(s)); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

func templateLabel(t string) string {
	if t == "" {
		return "no-template"
	}
	return t
}

func (m *tui) commitService(returnToList bool) (tea.Model, tea.Cmd) {
	m.syncTemplateSeeds()
	svc := m.buildServiceFromFields()

	cfg := m.cfg
	services := append([]Service(nil), cfg.Services...)
	if m.editIdx == -1 {
		services = append(services, svc)
		m.editIdx = len(services) - 1
	} else {
		services[m.editIdx] = svc
	}
	cfg.Services = services

	if err := m.app.SetConfig(cfg); err != nil {
		m.setFlash("error: "+err.Error(), true)
		// stay where we are so the user can correct the problem
		if returnToList {
			m.screen = screenList
			m.form = nil
		} else {
			m.tabActive = true
			m.form = nil
		}
		return m, nil
	}

	m.cfg = cfg
	m.origSvc = svc
	m.setFlash("saved "+svc.Name, false)

	if returnToList {
		m.screen = screenList
		m.form = nil
		m.tabActive = false
		return m, nil
	}
	m.tabActive = true
	m.form = nil
	return m, nil
}

// ── service editor: tab selector ──────────────────────────────────────────────

func (m *tui) updateServiceSelector(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch k.String() {
	case "esc", "q":
		if m.hasUnsavedChanges() {
			return m.openDiscardConfirm()
		}
		m.screen = screenList
		m.tabActive = false
		return m, nil

	case "s":
		return m.commitService(true)

	case "up", "k", "left", "h":
		if m.editTab > 0 {
			m.editTab--
		}

	case "down", "j", "right", "l":
		if m.editTab < len(serviceTabNames)-1 {
			m.editTab++
		}

	case "1", "2", "3", "4", "5":
		m.editTab = int(k.Runes[0] - '1')
		m.tabActive = false
		m.openServiceTab(m.editTab)
		return m, m.form.Init()

	case "enter", " ":
		m.tabActive = false
		m.openServiceTab(m.editTab)
		return m, m.form.Init()
	}
	return m, nil
}

// ── global config ─────────────────────────────────────────────────────────────

func (m *tui) updateGlobalSelector(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch k.String() {
	case "esc", "q":
		m.screen = screenList
		m.globalActive = false
		return m, nil

	case "up", "k", "left", "h":
		if m.globalIdx > 0 {
			m.globalIdx--
		}

	case "down", "j", "right", "l":
		if m.globalIdx < len(globalTabNames)-1 {
			m.globalIdx++
		}

	case "t":
		if m.testing {
			return m, nil
		}
		m.testing = true
		m.setFlash("testing connection…", false)
		return m, testConnCmd(m.app)

	case "1", "2":
		m.globalIdx = int(k.Runes[0] - '1')
		m.globalActive = false
		m.form = m.makeGlobalTabForm(m.globalIdx)
		return m, m.form.Init()

	case "enter", " ":
		m.globalActive = false
		m.form = m.makeGlobalTabForm(m.globalIdx)
		return m, m.form.Init()
	}
	return m, nil
}

func (m *tui) makeGlobalTabForm(idx int) *huh.Form {
	w := m.formWidth()
	if idx == 1 {
		m.gAttrs = attrsToText(m.cfg.Attributes)
		return huh.NewForm(
			huh.NewGroup(
				huh.NewText().
					Title("Global resource attributes").
					Description("Merged into every service, lowest precedence · " + attrTypeHint).
					Lines(m.textLines()).
					Value(&m.gAttrs),
			),
		).WithWidth(w)
	}

	m.gEndpoint = m.cfg.Endpoint
	m.gToken = ""

	endpointDesc := "e.g. https://xxx.live.dynatrace.com/api/v2/otlp"
	if endpointFromEnv() {
		endpointDesc = "⚠ OTGEN_ENDPOINT is set — it overrides this value at runtime"
	}
	tokenDesc := "Leave blank to keep the current token"
	if !m.cfg.hasToken() {
		tokenDesc = "No token configured yet"
	}
	if tokenFromEnv() {
		tokenDesc = "⚠ OTGEN_TOKEN is set — it overrides this value at runtime"
	}

	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("OTLP endpoint").
				Description(endpointDesc).
				Value(&m.gEndpoint),
			huh.NewInput().
				Title("API token").
				Description(tokenDesc).
				Password(true).
				Value(&m.gToken),
		),
	).WithWidth(w)
}

func (m *tui) commitGlobalTab() (tea.Model, tea.Cmd) {
	cfg := m.cfg
	label := "settings saved"
	if m.globalIdx == 1 {
		cfg.Attributes = parseAttrs(m.gAttrs)
		label = "global attributes saved"
	} else {
		cfg.Endpoint = strings.TrimSpace(m.gEndpoint)
		if tok := strings.TrimSpace(m.gToken); tok != "" {
			cfg.Token = tok
		}
	}

	if err := m.app.SetConfig(cfg); err != nil {
		m.setFlash("error: "+err.Error(), true)
	} else {
		m.cfg = cfg
		m.setFlash(label, false)
	}
	m.globalActive = true
	m.form = nil
	return m, nil
}

// ── confirmations ─────────────────────────────────────────────────────────────

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
		services := append([]Service(nil), cfg.Services...)
		services = append(services[:m.cursor], services[m.cursor+1:]...)
		cfg.Services = services
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

func (m *tui) openDiscardConfirm() (tea.Model, tea.Cmd) {
	m.fDiscardConfirmed = false
	m.form = huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Discard unsaved changes to " + strings.TrimSpace(m.fName) + "?").
				Description("Press s in the tab list to save instead.").
				Affirmative("Yes, discard").
				Negative("Keep editing").
				Value(&m.fDiscardConfirmed),
		),
	).WithWidth(m.formWidth())
	m.screen = screenConfirmDiscard
	m.tabActive = false
	return m, m.form.Init()
}

func (m *tui) commitDiscard() (tea.Model, tea.Cmd) {
	m.form = nil
	if m.fDiscardConfirmed {
		m.screen = screenList
		m.tabActive = false
		return m, nil
	}
	// keep editing — back to the tab selector
	m.screen = screenServiceEdit
	m.tabActive = true
	return m, nil
}

// ── toggles ───────────────────────────────────────────────────────────────────

func (m *tui) toggleService() (tea.Model, tea.Cmd) {
	if m.cursor >= len(m.cfg.Services) {
		return m, nil
	}
	svcs := append([]Service(nil), m.cfg.Services...)
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
	m.status = m.app.GetStatus()
	return m, nil
}

// ── flash ─────────────────────────────────────────────────────────────────────

func (m *tui) setFlash(msg string, isErr bool) {
	m.flash = msg
	m.flashErr = isErr
	m.flashEnd = time.Now().Add(4 * time.Second)
}

// ── view ──────────────────────────────────────────────────────────────────────

func (m *tui) View() string {
	if m.width == 0 {
		return "Loading…"
	}
	switch m.screen {
	case screenHelp:
		return m.helpView()

	case screenServiceEdit:
		if m.tabActive {
			return m.serviceSelectorView()
		}
		if m.form != nil {
			return m.tabBar(serviceTabNames, m.editTab) + "\n" + m.tabSwitchHint() + "\n" + m.form.View()
		}

	case screenGlobal:
		if m.globalActive {
			return m.globalSelectorView()
		}
		if m.form != nil {
			return m.tabBar(globalTabNames, m.globalIdx) + "\n" + m.tabSwitchHint() + "\n" + m.form.View()
		}
	}
	if m.form != nil && m.screen != screenList {
		return m.form.View()
	}
	return m.listView()
}

// sepLine renders the single horizontal rule used across all screens.
func (m *tui) sepLine() string {
	w := m.width - 4
	if w > 72 {
		w = 72
	}
	if w < 20 {
		w = 20
	}
	return sMuted.Render(strings.Repeat("─", w))
}

// tabBar renders the compact bar shown above an open form. It is also the top
// of each selector view, so the two screens read as one continuous surface.
func (m *tui) tabBar(names []string, active int) string {
	// Full bar: every tab named. Falls back to numbers, then to the active tab
	// alone, so the bar never wraps on a narrow terminal.
	var full, numbered []string
	for i, name := range names {
		label := fmt.Sprintf("%d %s", i+1, name)
		num := strconv.Itoa(i + 1)
		if i == active {
			full = append(full, sPrimaryBold.Render(label))
			numbered = append(numbered, sPrimaryBold.Render(label))
		} else {
			full = append(full, sMuted.Render(label))
			numbered = append(numbered, sMuted.Render(num))
		}
	}

	sep := sMuted.Render(" │ ")
	for _, parts := range [][]string{full, numbered} {
		bar := "  " + strings.Join(parts, sep)
		if lipgloss.Width(bar) <= m.width {
			return bar + "\n  " + m.sepLine()
		}
	}
	bar := fmt.Sprintf("  %s  %s",
		sMuted.Render(fmt.Sprintf("tab %d/%d", active+1, len(names))),
		sPrimaryBold.Render(names[active]))
	return bar + "\n  " + m.sepLine()
}

// tabSwitchHint is shown under the tab bar while a form is focused, so the
// in-form switch keys are discoverable without opening the help overlay.
func (m *tui) tabSwitchHint() string {
	full := "  ctrl+o / ctrl+r  next / prev tab   ·   f1-f5  jump   ·   esc  tab list"
	if lipgloss.Width(full) <= m.width {
		return sHelp.Render(full)
	}
	return sHelp.Render("  ctrl+o next tab · esc tab list")
}

// serviceSelectorView lists the editor tabs with a summary of each one.
func (m *tui) serviceSelectorView() string {
	summaries := m.serviceTabSummaries()

	var rows []string
	rows = append(rows, m.tabBar(serviceTabNames, m.editTab))

	name := strings.TrimSpace(m.fName)
	if name == "" {
		name = "new service"
	}
	head := "  " + sBold.Render(name)
	if m.editIdx == -1 {
		head += sMuted.Render("  (not saved yet)")
	} else if m.hasUnsavedChanges() {
		head += "  " + sWarn.Render("● unsaved changes")
	} else {
		head += "  " + sMuted.Render("✓ saved")
	}
	rows = append(rows, head, "")

	for i, tabName := range serviceTabNames {
		line := fmt.Sprintf("%d %s", i+1, tabName)
		pad := 20 - len([]rune(line))
		if pad < 1 {
			pad = 1
		}
		line += strings.Repeat(" ", pad) + summaries[i]
		if i == m.editTab {
			rows = append(rows, sPrimary.Render("  ▸ ")+sPrimaryBold.Render(line))
		} else {
			rows = append(rows, sMuted.Render("    "+line))
		}
	}

	rows = append(rows, "", "  "+m.sepLine())
	rows = append(rows, sHelp.Render("  ↑↓ navigate  ·  enter / 1-5 open  ·  s save  ·  esc back"))
	rows = append(rows, sHelp.Render("  ~ inherited from template   ✎ your override"))
	return strings.Join(rows, "\n")
}

// serviceTabSummaries describes the current contents of each editor tab.
func (m *tui) serviceTabSummaries() []string {
	sigs := "all signals"
	if len(m.fSignals) != 3 && len(m.fSignals) > 0 {
		s := append([]string(nil), m.fSignals...)
		sort.Strings(s)
		sigs = strings.Join(s, ",")
	} else if len(m.fSignals) == 0 {
		sigs = "no signals"
	}
	state := "enabled"
	if !m.fEnabled {
		state = "disabled"
	}
	settings := fmt.Sprintf("%s · every %ss · %s%% err · %s",
		m.fSpanKind, strings.TrimSpace(m.fInterval), strings.TrimSpace(m.fFailure), sigs)
	if n, _ := strconv.Atoi(strings.TrimSpace(m.fChildSpans)); n > 0 {
		settings += fmt.Sprintf(" · +%d child", n)
	}
	settings += " · " + state

	spanTmpl := m.fTemplate
	if spanTmpl == "" {
		spanTmpl = sMuted.Render("none — generic spans")
	}
	infraTmpl := m.fInfraTemplate
	if infraTmpl == "" {
		infraTmpl = sMuted.Render("none")
	}

	return []string{
		settings,
		spanTmpl,
		infraTmpl,
		attrSummary(m.fAttrs, m.fAttrsSeed),
		attrSummary(m.fSpanAttrs, m.fSpanAttrsSeed),
	}
}

// attrSummary describes a textarea's contents and whether it is a template
// default or a user override.
func attrSummary(text, seed string) string {
	n := len(parseAttrs(text))
	if n == 0 {
		return sMuted.Render("none")
	}
	if seed != "" && strings.TrimSpace(text) == strings.TrimSpace(seed) {
		return fmt.Sprintf("~ %d from template", n)
	}
	return fmt.Sprintf("✎ %d custom", n)
}

func (m *tui) globalSelectorView() string {
	var rows []string
	rows = append(rows, m.tabBar(globalTabNames, m.globalIdx))
	rows = append(rows, "  "+sBold.Render("Global configuration"), "")

	ep := m.cfg.Endpoint
	if ep == "" {
		ep = "not configured"
	}
	if endpointFromEnv() {
		ep = "OTGEN_ENDPOINT (env override active)"
	}
	tok := "not configured"
	if m.cfg.hasToken() {
		tok = "configured"
	}
	if tokenFromEnv() {
		tok = "OTGEN_TOKEN (env override active)"
	}
	conn := truncate(ep, max(20, m.width-34)) + " · token " + tok

	summaries := []string{conn, attrSummary(attrsToText(m.cfg.Attributes), "")}
	for i, name := range globalTabNames {
		line := fmt.Sprintf("%d %s", i+1, name)
		pad := 20 - len([]rune(line))
		if pad < 1 {
			pad = 1
		}
		line += strings.Repeat(" ", pad) + summaries[i]
		if i == m.globalIdx {
			rows = append(rows, sPrimary.Render("  ▸ ")+sPrimaryBold.Render(line))
		} else {
			rows = append(rows, sMuted.Render("    "+line))
		}
	}

	if m.flash != "" {
		rows = append(rows, "")
		if m.flashErr {
			rows = append(rows, sError.Render("  ✗ "+truncate(m.flash, max(20, m.width-6))))
		} else {
			rows = append(rows, sSuccess.Render("  ✓ "+truncate(m.flash, max(20, m.width-6))))
		}
	}

	rows = append(rows, "", "  "+m.sepLine())
	rows = append(rows, sHelp.Render("  ↑↓ navigate  ·  enter / 1-2 open  ·  t test connection  ·  esc back"))
	return strings.Join(rows, "\n")
}

func (m *tui) helpView() string {
	type row struct{ key, desc string }
	sections := []struct {
		title string
		rows  []row
	}{
		{"Service list", []row{
			{"↑ ↓ / j k", "move between services"},
			{"enter", "open the service editor (tab list)"},
			{"1 – 5", "edit one tab directly, save on enter"},
			{"n", "new service (guided through all tabs)"},
			{"space", "enable / disable the service"},
			{"d", "delete the service"},
		}},
		{"Generator", []row{
			{"r", "start / stop sending"},
			{"t", "send one test span and report the result"},
		}},
		{"Configuration", []row{
			{"g", "global configuration (endpoint, token, attributes)"},
			{"?", "this help"},
			{"q", "quit (stops the generator)"},
		}},
		{"Editor — tab list", []row{
			{"enter / 1-5", "open a tab"},
			{"s", "save and return to the list"},
			{"esc", "leave the editor (asks if unsaved)"},
		}},
		{"Editor — inside a tab", []row{
			{"ctrl+o", "next tab, without leaving the form"},
			{"ctrl+r", "previous tab"},
			{"f1 – f5", "jump straight to a tab"},
			{"esc", "back to the tab list"},
			{"space", "toggle a signal on the Settings tab"},
			{"alt+enter", "new line inside an attribute textarea"},
			{"/", "filter long template lists"},
		}},
		{"Attribute markers", []row{
			{"~", "inherited from a template (or the global attributes)"},
			{"✎", "overridden by hand for this service"},
		}},
	}

	var rows []string
	rows = append(rows, "  "+sPrimaryBold.Render("otgen")+sMuted.Render("  keyboard reference"))
	rows = append(rows, "  "+m.sepLine())
	for _, sec := range sections {
		rows = append(rows, "", "  "+sBold.Render(sec.title))
		for _, r := range sec.rows {
			pad := 14 - len([]rune(r.key))
			if pad < 1 {
				pad = 1
			}
			rows = append(rows, "    "+sPrimary.Render(r.key)+strings.Repeat(" ", pad)+sMuted.Render(r.desc))
		}
	}
	rows = append(rows, "", "  "+m.sepLine())
	rows = append(rows, sHelp.Render("  press any key to go back"))
	return strings.Join(rows, "\n")
}

func (m *tui) listView() string {
	var rows []string

	rows = append(rows, m.renderHeader())
	rows = append(rows, "")

	if len(m.cfg.Services) == 0 {
		rows = append(rows, sMuted.Render("  No services yet — press n to create one."))
	} else {
		for i, svc := range m.cfg.Services {
			rows = append(rows, m.renderService(i, svc, i == m.cursor))
		}
	}

	if m.flash != "" {
		rows = append(rows, "")
		msg := truncate(m.flash, max(20, m.width-6))
		if m.flashErr {
			rows = append(rows, sError.Render("  ✗ "+msg))
		} else {
			rows = append(rows, sSuccess.Render("  ✓ "+msg))
		}
	}

	body := strings.Join(rows, "\n")

	// pad so the help line sits at the bottom of the terminal
	lineCount := strings.Count(body, "\n") + 1
	targetLine := m.height - 2
	if lineCount < targetLine {
		body += strings.Repeat("\n", targetLine-lineCount)
	}
	return body + "\n" + m.renderHelp()
}

func (m *tui) renderHeader() string {
	indicator := sMuted.Render("○ stopped")
	if m.status.Running {
		indicator = sSuccess.Render("● running")
	}

	ver := " v" + version
	if version == "dev" {
		ver = " (dev build)"
	}
	line1 := "  " + sPrimaryBold.Render("otgen") + sMuted.Render(ver) + "  " + indicator

	ep := m.cfg.Endpoint
	switch {
	case endpointFromEnv():
		ep = sMuted.Render(m.cfg.runtimeConfig().Endpoint) + " " + sWarn.Render("[env]")
	case ep == "":
		ep = sMuted.Italic(true).Render("no endpoint — press g to configure")
	default:
		ep = sMuted.Render(ep)
	}

	var badges []string
	if !m.cfg.hasToken() && !tokenFromEnv() {
		badges = append(badges, sWarn.Render("no token"))
	} else if tokenFromEnv() {
		badges = append(badges, sWarn.Render("token [env]"))
	}
	if n := len(m.cfg.Attributes); n > 0 {
		badges = append(badges, sMuted.Render(fmt.Sprintf("%d global attr", n)))
	}
	line2 := "  " + ep
	if len(badges) > 0 {
		line2 += sMuted.Render("  ·  ") + strings.Join(badges, sMuted.Render("  ·  "))
	}

	return line1 + "\n" + line2 + "\n  " + m.sepLine()
}

// renderService draws one service. The cursored row is expanded with its
// effective resource and span attributes; the others stay compact.
func (m *tui) renderService(i int, svc Service, expanded bool) string {
	cursor := "  "
	name := svc.Name
	if expanded {
		cursor = sPrimary.Render("▶ ")
		name = sBold.Render(name)
	}

	dot := sMuted.Render("○")
	if svc.Enabled {
		dot = sSuccess.Render("●")
	}

	meta := []string{svc.SpanKind}
	if svc.Template != "" {
		meta = append(meta, "["+svc.Template+"]")
	}
	if svc.InfraTemplate != "" {
		meta = append(meta, "["+svc.InfraTemplate+"]")
	}
	meta = append(meta, fmt.Sprintf("%ds", svc.Interval), fmt.Sprintf("%d%% err", svc.FailureRate))
	if svc.ChildSpans > 0 {
		meta = append(meta, fmt.Sprintf("+%d child", svc.ChildSpans))
	}
	row1 := cursor + dot + " " + name + "  " + sMuted.Render(strings.Join(meta, "  "))

	var lines []string
	lines = append(lines, row1)

	// Second line: live counters, or a clear disabled/idle marker.
	switch {
	case !svc.Enabled:
		lines = append(lines, "    "+sMuted.Render("disabled — press space to enable"))
	case !m.status.Running:
		lines = append(lines, "    "+sMuted.Render("idle — press r to start sending"))
	default:
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
		line := "    " + sMuted.Render(strings.Join(parts, "  "))
		for _, s := range []SignalStatus{ss.Spans, ss.Metrics, ss.Logs} {
			if s.LastError != "" {
				budget := m.width - lipgloss.Width(line) - 8
				line += "  " + sError.Render("! "+truncate(s.LastError, max(20, budget)))
				break
			}
		}
		lines = append(lines, line)
	}

	if !expanded {
		return strings.Join(lines, "\n")
	}

	// Expanded: show the attributes that will actually be emitted.
	resAttrs := svc.Attributes
	resMark := "✎"
	if len(resAttrs) == 0 {
		resAttrs = infraDefaults(svc)
		resMark = "~"
	}
	if len(m.cfg.Attributes) > 0 {
		merged := make(map[string]AttrValue, len(resAttrs)+len(m.cfg.Attributes))
		for k, v := range m.cfg.Attributes {
			merged[k] = v
		}
		for k, v := range resAttrs {
			merged[k] = v
		}
		resAttrs = merged
	}
	lines = append(lines, m.attrLine("res ", resMark, resAttrs))

	spanAttrs := svc.SpanAttrs
	spanMark := "✎"
	if len(spanAttrs) == 0 {
		spanAttrs = templateDefaults(svc.Template)
		spanMark = "~"
	}
	lines = append(lines, m.attrLine("span", spanMark, spanAttrs))

	return strings.Join(lines, "\n")
}

// attrLine renders one "res"/"span" preview row, fitted to the terminal width.
func (m *tui) attrLine(label, mark string, attrs map[string]AttrValue) string {
	if len(attrs) == 0 {
		return "    " + sMuted.Render(label+"  "+sMuted.Render("—"))
	}
	budget := m.width - 14
	if budget < 20 {
		budget = 20
	}
	preview, shown := attrsPreview(attrs, budget)
	if rest := len(attrs) - shown; rest > 0 {
		preview += fmt.Sprintf("  +%d", rest)
	}
	return "    " + sMuted.Render(label+" "+mark+" "+preview)
}

func (m *tui) renderHelp() string {
	full := []string{
		"n new", "↵ edit", "1-5 tab", "d delete", "␣ toggle",
		"r run/stop", "t test", "g config", "? help", "q quit",
	}
	medium := []string{"n new", "↵ edit", "r run/stop", "g config", "? help", "q quit"}
	short := []string{"↵ edit", "r run", "? help", "q quit"}

	for _, set := range [][]string{full, medium, short} {
		line := "  " + strings.Join(set, "  ·  ")
		if lipgloss.Width(line) <= m.width {
			return sHelp.Render(line)
		}
	}
	return sHelp.Render("  ? help")
}

// textLines sizes an attribute textarea to the terminal height.
func (m *tui) textLines() int {
	n := m.height - 12
	if n > 16 {
		n = 16
	}
	if n < 5 {
		n = 5
	}
	return n
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

// attrsPreview returns as many "key=value" pairs as fit in budget columns,
// along with how many were shown.
func attrsPreview(attrs map[string]AttrValue, budget int) (string, int) {
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	used := 0
	for _, k := range keys {
		pair := k + "=" + attrValueText(attrs[k], false)
		if used > 0 && used+len(pair)+2 > budget {
			break
		}
		used += len(pair) + 2
		parts = append(parts, pair)
	}
	return strings.Join(parts, "  "), len(parts)
}

// attrValueText renders an AttrValue. When quote is true, strings that would
// be re-parsed as another type are wrapped in "" so they round-trip.
func attrValueText(v AttrValue, quote bool) string {
	switch v.Type {
	case "bool":
		if v.Bool {
			return "true"
		}
		return "false"
	case "int":
		return strconv.FormatInt(v.Int, 10)
	case "double":
		s := strconv.FormatFloat(v.Double, 'f', -1, 64)
		if quote && !strings.Contains(s, ".") {
			s += ".0" // ensure it round-trips as double, not int
		}
		return s
	default:
		if quote && attrStrNeedsQuoting(v.Str) {
			return `"` + v.Str + `"`
		}
		return v.Str
	}
}

// attrsToText serialises a map[string]AttrValue to a human-editable
// "key=value" text (one entry per line, sorted by key).
func attrsToText(attrs map[string]AttrValue) string {
	if len(attrs) == 0 {
		return ""
	}
	lines := make([]string, 0, len(attrs))
	for k, v := range attrs {
		lines = append(lines, k+"="+attrValueText(v, true))
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
	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		return true
	}
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
	if n <= 1 {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
