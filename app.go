package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"
)

type App struct {
	mu          sync.RWMutex
	cfg         AppConfig
	status      RuntimeStatus
	runCancel   context.CancelFunc
	configPath  string
	httpTimeout time.Duration
}

func NewApp(configPath string, httpTimeout time.Duration) *App {
	return &App{
		cfg: defaultConfig(),
		status: RuntimeStatus{
			Signals: map[string]SignalStatus{},
		},
		configPath:  configPath,
		httpTimeout: httpTimeout,
	}
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handleIndex)
	mux.HandleFunc("/api/config", a.handleConfig)
	mux.HandleFunc("/api/start", a.handleStart)
	mux.HandleFunc("/api/stop", a.handleStop)
	mux.HandleFunc("/api/status", a.handleStatus)
	return mux
}

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	content, err := webFS.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, "failed to load ui", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(content)
}

func (a *App) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.mu.RLock()
		effectiveCfg := a.cfg.runtimeConfig()
		resp := map[string]any{
			"config":              a.cfg.redacted(),
			"effectiveConfig":     effectiveCfg.redacted(),
			"status":              a.status,
			"tokenConfigured":     effectiveCfg.hasToken(),
			"endpointFromEnv":     endpointFromEnv(a.cfg),
			"tokenFromEnv":        envTokenConfigured(),
			"tokenHeaderFromEnv":  tokenHeaderFromEnv(a.cfg),
		}
		a.mu.RUnlock()
		writeJSON(w, http.StatusOK, resp)
	case http.MethodPost:
		defer r.Body.Close()
		var cfg AppConfig
		if err := json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&cfg); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}

		cfg = normalizeConfig(cfg)
		a.mu.RLock()
		cfg = a.cfg.withPreservedSecret(cfg)
		a.mu.RUnlock()
		if err := validateConfig(cfg); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := a.SetConfig(cfg); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *App) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := a.Start(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (a *App) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a.Stop()
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (a *App) handleStatus(w http.ResponseWriter, _ *http.Request) {
	a.mu.RLock()
	status := a.status
	a.mu.RUnlock()
	writeJSON(w, http.StatusOK, status)
}

func (a *App) SetConfig(cfg AppConfig) error {
	a.mu.Lock()
	a.cfg = cfg
	running := a.status.Running
	a.mu.Unlock()

	if err := a.saveConfig(cfg); err != nil {
		return err
	}
	if !running {
		return nil
	}

	a.Stop()
	return a.Start()
}

func (a *App) Start() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.status.Running {
		return nil
	}
	runtimeCfg := a.cfg.runtimeConfig()
	if strings.TrimSpace(runtimeCfg.Endpoint) == "" {
		return fmt.Errorf("endpoint is required")
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.runCancel = cancel
	a.status.Running = true
	for _, kind := range allSignalKinds {
		if runtimeCfg.enabled(kind) {
			go a.runSignalLoop(ctx, kind)
		}
	}
	return nil
}

func (a *App) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.status.Running {
		return
	}
	if a.runCancel != nil {
		a.runCancel()
		a.runCancel = nil
	}
	a.status.Running = false
}

func (a *App) runSignalLoop(ctx context.Context, kind signalKind) {
	a.mu.RLock()
	interval := a.cfg.IntervalSeconds
	a.mu.RUnlock()
	if interval <= 0 {
		interval = 5
	}

	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	a.sendOne(kind)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.sendOne(kind)
		}
	}
}

func (a *App) sendOne(kind signalKind) {
	a.mu.RLock()
	cfg := a.cfg.runtimeConfig()
	a.mu.RUnlock()
	if !cfg.enabled(kind) {
		return
	}

	payload, err := buildPayload(cfg, kind)
	if err == nil {
		err = a.postOTLP(cfg, kind, payload)
	}
	a.updateSignalStatus(kind, err)
}

func (a *App) postOTLP(cfg AppConfig, kind signalKind, payload []byte) error {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return fmt.Errorf("endpoint is empty")
	}

	req, err := http.NewRequest(http.MethodPost, endpointFor(cfg.Endpoint, kind), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Accept", "application/x-protobuf, application/json")
	if token := strings.TrimSpace(cfg.Token); token != "" {
		req.Header.Set(cfg.tokenHeader(), formatTokenHeader(cfg.tokenHeader(), token))
	}

	client := &http.Client{
		Timeout: a.httpTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.InsecureSkipTLS}, //nolint:gosec
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
}

func (a *App) updateSignalStatus(kind signalKind, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	cur := a.status.Signals[string(kind)]
	if err == nil {
		cur.SentCount++
		cur.LastSentAt = time.Now().Format(time.RFC3339)
		cur.LastError = ""
	} else {
		cur.LastError = err.Error()
	}
	a.status.Signals[string(kind)] = cur
}

func (cfg AppConfig) tokenHeader() string {
	if strings.TrimSpace(cfg.TokenHeader) == "" {
		return "Authorization"
	}
	return cfg.TokenHeader
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func withRequestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, path.Clean(r.URL.Path), time.Since(start).String())
	})
}
