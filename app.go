package main

import (
	"bytes"
	"context"
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
	cfg         Config
	status      RuntimeStatus
	runCancel   context.CancelFunc
	configPath  string
	httpTimeout time.Duration
}

func NewApp(configPath string, httpTimeout time.Duration) *App {
	return &App{
		cfg: defaultConfig(),
		status: RuntimeStatus{
			Services: map[string]ServiceStatus{},
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
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, "otgen\n\nGET  /api/config   read config\nPOST /api/config   write config\nPOST /api/start    start sending\nPOST /api/stop     stop sending\nGET  /api/status   runtime status\n")
}

func (a *App) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.mu.RLock()
		effectiveCfg := a.cfg.runtimeConfig()
		resp := map[string]any{
			"config":          a.cfg.redacted(),
			"effectiveConfig": effectiveCfg.redacted(),
			"status":          a.status,
			"tokenConfigured": effectiveCfg.hasToken(),
			"endpointFromEnv": endpointFromEnv(),
			"tokenFromEnv":    envTokenConfigured(),
		}
		a.mu.RUnlock()
		writeJSON(w, http.StatusOK, resp)
	case http.MethodPost:
		defer r.Body.Close()
		var cfg Config
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

func (a *App) SetConfig(cfg Config) error {
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
	for _, svc := range runtimeCfg.Services {
		if !svc.Enabled {
			continue
		}
		go a.runService(ctx, svc)
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

func (a *App) runService(ctx context.Context, svc Service) {
	a.mu.RLock()
	interval := a.cfg.Interval
	a.mu.RUnlock()
	if interval <= 0 {
		interval = 5
	}
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()
	a.sendService(svc)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.sendService(svc)
		}
	}
}

func (a *App) sendService(svc Service) {
	a.mu.RLock()
	cfg := a.cfg.runtimeConfig()
	a.mu.RUnlock()
	for _, kind := range []signalKind{signalSpans, signalMetrics, signalLogs} {
		if !svc.hasSignal(kind) {
			continue
		}
		payload, err := buildPayload(cfg, svc, kind)
		if err == nil {
			err = a.postOTLP(cfg.Endpoint, cfg.Token, kind, payload)
		}
		a.updateSignalStatus(svc.Name, kind, err)
	}
}

func (a *App) postOTLP(endpoint, token string, kind signalKind, payload []byte) error {
	if strings.TrimSpace(endpoint) == "" {
		return fmt.Errorf("endpoint is empty")
	}
	req, err := http.NewRequest(http.MethodPost, endpointFor(endpoint, kind), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Accept", "application/x-protobuf, application/json")
	if tok := strings.TrimSpace(token); tok != "" {
		req.Header.Set("Authorization", formatToken(tok))
	}
	client := &http.Client{Timeout: a.httpTimeout}
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

// formatToken ensures the token is prefixed with "Api-Token " when it is not
// already so prefixed.
func formatToken(token string) string {
	if !strings.HasPrefix(strings.ToLower(token), "api-token ") {
		return "Api-Token " + token
	}
	return token
}

func (a *App) updateSignalStatus(svcName string, kind signalKind, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ss := a.status.Services[svcName]
	var cur *SignalStatus
	switch kind {
	case signalSpans:
		cur = &ss.Spans
	case signalMetrics:
		cur = &ss.Metrics
	case signalLogs:
		cur = &ss.Logs
	}
	if cur != nil {
		if err == nil {
			cur.SentCount++
			cur.LastSentAt = time.Now().Format(time.RFC3339)
			cur.LastError = ""
		} else {
			cur.LastError = err.Error()
		}
	}
	a.status.Services[svcName] = ss
}

// GetConfig returns a snapshot of the current config (safe to call from any goroutine).
func (a *App) GetConfig() Config {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg
}

// GetStatus returns a snapshot of the current runtime status (safe to call from any goroutine).
func (a *App) GetStatus() RuntimeStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
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
