package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
