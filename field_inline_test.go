package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

func sigKey(s string) tea.KeyMsg {
	switch s {
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// TestSignalsFieldRendersOneRow is the reason this field exists: huh's
// MultiSelect costs four rows, which pushed the Settings tab past the height a
// terminal can show (huh cannot scroll an oversized form).
func TestSignalsFieldRendersOneRow(t *testing.T) {
	v := []string{"spans", "logs"}
	f := newSignalsField("Signals", &v)
	f.WithWidth(60)

	view := f.View()
	if n := strings.Count(view, "\n") + 1; n != 1 {
		t.Errorf("expected a single row, got %d:\n%s", n, view)
	}
	for _, want := range []string{"spans", "metrics", "logs"} {
		if !strings.Contains(view, want) {
			t.Errorf("view is missing %q: %s", want, view)
		}
	}
	// selected signals get a check, unselected an empty marker
	if !strings.Contains(view, "✓ spans") || !strings.Contains(view, "✓ logs") {
		t.Errorf("selected signals not checked: %s", view)
	}
	if !strings.Contains(view, "○ metrics") {
		t.Errorf("unselected signal should be unchecked: %s", view)
	}
}

func TestSignalsFieldToggle(t *testing.T) {
	v := []string{"spans", "metrics", "logs"}
	f := newSignalsField("Signals", &v)
	f.Focus()

	// cursor starts on spans — turn it off
	f.Update(sigKey("space"))
	if got := strings.Join(v, ","); got != "logs,metrics" {
		t.Errorf("after toggling spans off: %q", got)
	}

	// move to logs and turn it off
	f.Update(sigKey("right"))
	f.Update(sigKey("right"))
	f.Update(sigKey("space"))
	if got := strings.Join(v, ","); got != "metrics" {
		t.Errorf("after toggling logs off: %q", got)
	}

	// back on
	f.Update(sigKey("space"))
	if got := strings.Join(v, ","); got != "logs,metrics" {
		t.Errorf("after toggling logs back on: %q", got)
	}
}

func TestSignalsFieldCursorStaysInRange(t *testing.T) {
	v := []string{"spans"}
	f := newSignalsField("Signals", &v)
	f.Focus()

	for i := 0; i < 5; i++ {
		f.Update(sigKey("left"))
	}
	if f.cursor != 0 {
		t.Errorf("cursor ran past the left edge: %d", f.cursor)
	}
	for i := 0; i < 8; i++ {
		f.Update(sigKey("right"))
	}
	if f.cursor != len(signalOptions)-1 {
		t.Errorf("cursor ran past the right edge: %d", f.cursor)
	}
}

// TestSignalsFieldAdvancesForm checks the field hands control back to huh, so
// enter/tab still move between fields.
func TestSignalsFieldAdvancesForm(t *testing.T) {
	v := []string{"spans"}
	f := newSignalsField("Signals", &v)
	f.Focus()

	if _, cmd := f.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil {
		t.Error("enter should return a command advancing to the next field")
	}
	if _, cmd := f.Update(tea.KeyMsg{Type: tea.KeyShiftTab}); cmd == nil {
		t.Error("shift+tab should return a command moving to the previous field")
	}
}

// ── choiceField (span kind) ───────────────────────────────────────────────────

func TestChoiceFieldRendersOneRow(t *testing.T) {
	v := "client"
	f := newChoiceField("spanKind", "Span kind", &v, spanKindOptions)
	f.WithWidth(90)

	view := f.View()
	if n := strings.Count(view, "\n") + 1; n != 1 {
		t.Errorf("expected a single row, got %d:\n%s", n, view)
	}
	for _, want := range spanKindOptions {
		if !strings.Contains(view, want) {
			t.Errorf("view is missing option %q: %s", want, view)
		}
	}
}

// TestChoiceFieldCompactWhenNarrow checks the fallback that keeps the control
// on one row when the full option set will not fit.
func TestChoiceFieldCompactWhenNarrow(t *testing.T) {
	v := "producer"
	f := newChoiceField("spanKind", "Span kind", &v, spanKindOptions)
	f.WithWidth(30)

	view := f.View()
	if n := strings.Count(view, "\n") + 1; n != 1 {
		t.Errorf("expected a single row, got %d:\n%s", n, view)
	}
	if !strings.Contains(view, "producer") {
		t.Errorf("compact view must still show the current value: %s", view)
	}
	if strings.Contains(view, "consumer") {
		t.Errorf("compact view should show only the current value: %s", view)
	}
}

func TestChoiceFieldArrowsChangeValue(t *testing.T) {
	v := "server" // index 0
	f := newChoiceField("spanKind", "Span kind", &v, spanKindOptions)
	f.Focus()

	f.Update(sigKey("right"))
	if v != "client" {
		t.Errorf("after →: %q, want client", v)
	}
	f.Update(sigKey("left"))
	if v != "server" {
		t.Errorf("after →←: %q, want server", v)
	}

	// clamps at both ends rather than wrapping
	for i := 0; i < 5; i++ {
		f.Update(sigKey("left"))
	}
	if v != "server" {
		t.Errorf("ran past the left edge: %q", v)
	}
	for i := 0; i < 9; i++ {
		f.Update(sigKey("right"))
	}
	if want := spanKindOptions[len(spanKindOptions)-1]; v != want {
		t.Errorf("ran past the right edge: %q, want %q", v, want)
	}
}

// TestChoiceFieldStartsOnCurrentValue guards against the cursor resetting to
// the first option when the editor is reopened.
func TestChoiceFieldStartsOnCurrentValue(t *testing.T) {
	v := "consumer" // last option
	f := newChoiceField("spanKind", "Span kind", &v, spanKindOptions)
	f.Focus()

	f.Update(sigKey("left"))
	if want := spanKindOptions[len(spanKindOptions)-2]; v != want {
		t.Errorf("cursor did not start on the current value: got %q, want %q", v, want)
	}
}

// TestSpanKindRoundTripsThroughConfig checks every option the field offers is
// accepted by validateConfig and maps to a real OTLP span kind.
func TestSpanKindRoundTripsThroughConfig(t *testing.T) {
	m := testTUI(t)
	m.loadServiceFields(0)

	for _, kind := range spanKindOptions {
		m.fSpanKind = kind
		svc := m.buildServiceFromFields()
		if svc.SpanKind != kind {
			t.Errorf("span kind %q did not survive: %q", kind, svc.SpanKind)
		}
		cfg := Config{Services: []Service{svc}}
		if err := validateConfig(normalizeConfig(cfg)); err != nil {
			t.Errorf("validateConfig rejects span kind %q: %v", kind, err)
		}
		if got := mapSpanKind(kind); got == tracepb.Span_SPAN_KIND_UNSPECIFIED {
			t.Errorf("mapSpanKind(%q) is unspecified", kind)
		}
	}
}

// TestSignalsRoundTripThroughEditor guards the binding between the field and
// the saved service.
func TestSignalsRoundTripThroughEditor(t *testing.T) {
	m := testTUI(t)
	m.loadServiceFields(0)

	f := newSignalsField("Signals", &m.fSignals)
	f.Focus()
	f.Update(sigKey("right")) // metrics
	f.Update(sigKey("space")) // off

	svc := m.buildServiceFromFields()
	if got := strings.Join(svc.Signals, ","); got != "logs,spans" {
		t.Errorf("service signals = %q, want logs,spans", got)
	}
	if !svc.hasSignal(signalSpans) || svc.hasSignal(signalMetrics) || !svc.hasSignal(signalLogs) {
		t.Errorf("hasSignal disagrees with the saved set: %v", svc.Signals)
	}
}
