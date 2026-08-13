package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// version is set at build time via -ldflags "-X main.version=…"
var version = "dev"

func main() {
	// Redirect log output so it doesn't corrupt the TUI.
	if lf, err := os.OpenFile(tuiLogFile(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		log.SetOutput(lf)
		defer lf.Close()
	} else {
		log.SetOutput(io.Discard)
	}

	app := NewApp("config.json", 10*time.Second)
	if err := app.LoadConfig(); err != nil {
		log.Printf("warning: load config: %v", err)
	}

	p := tea.NewProgram(NewTUIModel(app), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	app.Stop()
}

func tuiLogFile() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "otgen.log")
}
