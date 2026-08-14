package main

import (
	"hash/fnv"

	"github.com/charmbracelet/lipgloss"
)

// ── palette ───────────────────────────────────────────────────────────────────
// Adaptive dark/light colors. Dark values are Catppuccin Mocha;
// light values are Catppuccin Latte — the same palette the Dynatrace-internal
// dynatui tool uses, so the two tools feel like a family on both terminal themes.

var (
	colPrimary = lipgloss.AdaptiveColor{Dark: "#89b4fa", Light: "#1e66f5"} // blue
	colSky     = lipgloss.AdaptiveColor{Dark: "#89dceb", Light: "#04a5e5"} // sky
	colSuccess = lipgloss.AdaptiveColor{Dark: "#a6e3a1", Light: "#40a02b"} // green
	colError   = lipgloss.AdaptiveColor{Dark: "#f38ba8", Light: "#d20f39"} // red
	colWarn    = lipgloss.AdaptiveColor{Dark: "#f9e2af", Light: "#df8e1d"} // yellow
	colMuted   = lipgloss.AdaptiveColor{Dark: "#6c7086", Light: "#9ca0b0"} // quiet
	colText    = lipgloss.AdaptiveColor{Dark: "#cdd6f4", Light: "#4c4f69"} // readable text
	colInk     = lipgloss.AdaptiveColor{Dark: "#1e1e2e", Light: "#eff1f5"} // on-accent (dark bg / light bg)
)

// ── base styles ───────────────────────────────────────────────────────────────

var (
	sPrimary     = lipgloss.NewStyle().Foreground(colPrimary)
	sPrimaryBold = lipgloss.NewStyle().Foreground(colPrimary).Bold(true)
	sSuccess     = lipgloss.NewStyle().Foreground(colSuccess)
	sError       = lipgloss.NewStyle().Foreground(colError)
	sWarn        = lipgloss.NewStyle().Foreground(colWarn)
	sMuted       = lipgloss.NewStyle().Foreground(colMuted)
	sBold        = lipgloss.NewStyle().Bold(true)
	sText        = lipgloss.NewStyle().Foreground(colText)

	// sHelp is used for separators ("  ·  ") and as the plain help fallback.
	sHelp = lipgloss.NewStyle().Foreground(colMuted)
	// sHelpKey highlights just the key letter(s) in each footer hint.
	sHelpKey = lipgloss.NewStyle().Bold(true).Foreground(colSky)
)

// ── tab styles ────────────────────────────────────────────────────────────────
// Active tab: filled blue pill so the current panel reads immediately.
// Inactive tabs: plain muted labels — no padding so the bar stays compact.

var (
	sTabActive   = lipgloss.NewStyle().Bold(true).Background(colPrimary).Foreground(colInk).Padding(0, 1)
	sTabInactive = lipgloss.NewStyle().Foreground(colMuted)
)

// ── series palette / per-service stable color ─────────────────────────────────
// Six accent hues that cycle. colorForService uses FNV-32a to map a service
// name to a stable index so the same name always renders in the same hue —
// making visual scanning on the list much faster once there are many services.

var seriesStyles = [6]lipgloss.Style{
	lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Dark: "#89b4fa", Light: "#1e66f5"}), // blue
	lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Dark: "#94e2d5", Light: "#179299"}), // teal
	lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Dark: "#cba6f7", Light: "#8839ef"}), // mauve
	lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Dark: "#fab387", Light: "#fe640b"}), // peach
	lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Dark: "#89dceb", Light: "#04a5e5"}), // sky
	lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Dark: "#a6e3a1", Light: "#40a02b"}), // green
}

// colorForService returns a stable style from the series cycle for name.
func colorForService(name string) lipgloss.Style {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return seriesStyles[h.Sum32()%uint32(len(seriesStyles))]
}

// ── spinner ───────────────────────────────────────────────────────────────────
// Ten-frame braille spinner, advanced on each tick while a long operation runs.

var spinnerFrames = [10]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
