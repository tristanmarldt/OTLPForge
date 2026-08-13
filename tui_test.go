package main

import (
	"strings"
	"testing"
)

func testTUI(t *testing.T) *tui {
	t.Helper()
	app := NewApp("/dev/null", 0)
	app.cfg = normalizeConfig(Config{
		Endpoint:   "https://example.live.dynatrace.com/api/v2/otlp",
		Token:      "dt0c01.TOKEN",
		Attributes: map[string]AttrValue{"dt.security_context": strAttrVal("ctx")},
		Services: []Service{{
			Name: "svc", Template: "http-server", InfraTemplate: "k8s",
			SpanKind: "server", FailureRate: 5, Interval: 5, ChildSpans: 3, Enabled: true,
		}},
	})
	m := NewTUIModel(app)
	m.width, m.height = 100, 32
	return m
}

// TestUntouchedTemplateAttrsAreNotPersisted guards against template defaults
// being frozen into the config as explicit overrides on every save, which would
// stop the service from tracking the template.
func TestUntouchedTemplateAttrsAreNotPersisted(t *testing.T) {
	m := testTUI(t)
	m.loadServiceFields(0)

	svc := m.buildServiceFromFields()
	if len(svc.Attributes) != 0 {
		t.Errorf("untouched template attrs persisted as overrides: %v", svc.Attributes)
	}
	if len(svc.SpanAttrs) != 0 {
		t.Errorf("untouched span attrs persisted as overrides: %v", svc.SpanAttrs)
	}
	if m.hasUnsavedChanges() {
		t.Error("freshly loaded service reports unsaved changes")
	}
}

// TestTemplateSwitchReseedsAttrs verifies that changing the infrastructure
// template refreshes the attribute editor instead of leaving the previous
// template's values behind.
func TestTemplateSwitchReseedsAttrs(t *testing.T) {
	m := testTUI(t)
	m.loadServiceFields(0)

	m.fInfraTemplate = "host"
	m.syncTemplateSeeds()

	got := parseAttrs(m.fAttrs)
	for k := range got {
		if strings.HasPrefix(k, "k8s.") {
			t.Errorf("stale attribute %q survived the switch to the host template", k)
		}
	}
	if _, ok := got["host.name"]; !ok {
		t.Errorf("host template attrs were not seeded, got %v", got)
	}
	if svc := m.buildServiceFromFields(); len(svc.Attributes) != 0 {
		t.Errorf("re-seeded template attrs persisted as overrides: %v", svc.Attributes)
	}
	if !m.hasUnsavedChanges() {
		t.Error("template change should count as an unsaved change")
	}
}

// TestManualAttrEditsSurviveTemplateSwitch ensures a hand-written attribute set
// is never silently replaced by template defaults.
func TestManualAttrEditsSurviveTemplateSwitch(t *testing.T) {
	m := testTUI(t)
	m.loadServiceFields(0)

	m.fAttrs = "my.custom=1\nmy.flag=true"
	m.fInfraTemplate = "ecs"
	m.syncTemplateSeeds()

	if !strings.Contains(m.fAttrs, "my.custom=1") {
		t.Fatalf("manual edits were overwritten by the template: %q", m.fAttrs)
	}
	svc := m.buildServiceFromFields()
	if len(svc.Attributes) != 2 {
		t.Fatalf("expected 2 persisted attrs, got %v", svc.Attributes)
	}
	if svc.Attributes["my.custom"] != intAttrVal(1) {
		t.Errorf("my.custom typed wrongly: %+v", svc.Attributes["my.custom"])
	}
	if svc.Attributes["my.flag"] != boolAttrVal(true) {
		t.Errorf("my.flag typed wrongly: %+v", svc.Attributes["my.flag"])
	}
}

// TestEnvOverridesAreSurfaced checks that an env-var override is visible in the
// header rather than silently beating whatever the user typed in the UI.
func TestEnvOverridesAreSurfaced(t *testing.T) {
	t.Setenv(envEndpoint, "https://env.example.com/api/v2/otlp")
	t.Setenv(envToken, "dt0c01.ENV")

	m := testTUI(t)
	header := m.renderHeader()
	for _, want := range []string{"env.example.com", "[env]"} {
		if !strings.Contains(header, want) {
			t.Errorf("header missing %q, got:\n%s", want, header)
		}
	}
}

// TestHelpLineFitsNarrowTerminals guards the help footer against wrapping.
func TestHelpLineFitsNarrowTerminals(t *testing.T) {
	m := testTUI(t)
	for _, w := range []int{40, 62, 80, 100, 160} {
		m.width = w
		line := m.renderHelp()
		if got := len([]rune(stripANSI(line))); got > w {
			t.Errorf("help line is %d cols at width %d: %q", got, w, line)
		}
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEscape = true
		case inEscape && r == 'm':
			inEscape = false
		case !inEscape:
			b.WriteRune(r)
		}
	}
	return b.String()
}
