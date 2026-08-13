package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

	// HTTP API runs in background alongside the TUI for curl/automation access.
	addr := ":8080"
	if p := strings.TrimSpace(os.Getenv("PORT")); p != "" {
		addr = ":" + p
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           withRequestLogging(app.routes()),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP API: %v", err)
		}
	}()

	p := tea.NewProgram(NewTUIModel(app), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}

	app.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

func tuiLogFile() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "otlpforge.log")
}
