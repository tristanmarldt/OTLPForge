package main

import (
	"embed"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

//go:embed web/index.html
var webFS embed.FS

func main() {
	app := NewApp("config.json", 10*time.Second)
	if err := app.LoadConfig(); err != nil {
		log.Printf("warning: could not load config: %v", err)
	}

	addr := ":8080"
	if fromEnv := strings.TrimSpace(os.Getenv("PORT")); fromEnv != "" {
		addr = ":" + fromEnv
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           withRequestLogging(app.routes()),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("OTLPForge listening on http://localhost%s", addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server failed: %v", err)
	}
}
