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

const version = "0.1.0"

func main() {
	app := NewApp("config.json", 10*time.Second)
	if err := app.LoadConfig(); err != nil {
		log.Printf("warning: load config: %v", err)
	}

	addr := ":8080"
	if p := strings.TrimSpace(os.Getenv("PORT")); p != "" {
		addr = ":" + p
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           withRequestLogging(app.routes()),
		ReadHeaderTimeout: 5 * time.Second,
	}

	if !isTerminal() {
		// Headless mode (Docker, CI, piped): HTTP API only.
		// Auto-start the sender when an endpoint is already configured.
		log.SetOutput(os.Stderr)
		log.SetFlags(log.LstdFlags)
		cfg := app.GetConfig().runtimeConfig()
		if strings.TrimSpace(cfg.Endpoint) != "" {
			if err := app.Start(); err != nil {
				log.Printf("warning: could not start sender: %v", err)
			} else {
				log.Printf("sender started (%d service(s))", len(cfg.Services))
			}
		}
		log.Printf("OTLPForge v%s — headless mode, HTTP API on %s", version, addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server: %v", err)
		}
		return
	}

	// TUI mode: redirect logs so they don't corrupt the screen.
	if lf, err := os.OpenFile(tuiLogFile(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		log.SetOutput(lf)
		defer lf.Close()
	} else {
		log.SetOutput(io.Discard)
	}

	// HTTP API runs in background for curl/automation access.
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

	// Graceful shutdown when TUI exits.
	app.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// isTerminal reports whether stdout is connected to a real terminal.
func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func tuiLogFile() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "otlpforge.log")
}
