package main

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
)

const (
	envEndpoint    = "OTLPFORGE_ENDPOINT"
	envToken       = "OTLPFORGE_TOKEN"
	envTokenHeader = "OTLPFORGE_TOKEN_HEADER"
	legacyExampleEndpoint = "https://example.com/api/v2/otlp"
)

type AppConfig struct {
	Endpoint           string            `json:"endpoint"`
	Token              string            `json:"token"`
	TokenHeader        string            `json:"tokenHeader"`
	IntervalSeconds    int               `json:"intervalSeconds"`
	InsecureSkipTLS    bool              `json:"insecureSkipTLS"`
	EmitSpans          bool              `json:"emitSpans"`
	EmitMetrics        bool              `json:"emitMetrics"`
	EmitLogs           bool              `json:"emitLogs"`
	ResourceAttributes map[string]string `json:"resourceAttributes"`
	SpanName           string            `json:"spanName"`
	SpanKind           string            `json:"spanKind"`
	SpanMinDurationMs  int               `json:"spanMinDurationMs"`
	SpanMaxDurationMs  int               `json:"spanMaxDurationMs"`
	SpanFailureRate    int               `json:"spanFailureRate"`
	SpanFailureMode    string            `json:"spanFailureMode"`
	SpanFailureCode    int               `json:"spanFailureCode"`
	SpanFailureMessage string            `json:"spanFailureMessage"`
	SpanChildCount     int               `json:"spanChildCount"`
	MetricName         string            `json:"metricName"`
	LogMessage         string            `json:"logMessage"`
}

type SignalStatus struct {
	LastSentAt string `json:"lastSentAt,omitempty"`
	LastError  string `json:"lastError,omitempty"`
	SentCount  uint64 `json:"sentCount"`
}

type RuntimeStatus struct {
	Running bool                    `json:"running"`
	Signals map[string]SignalStatus `json:"signals"`
}

type signalKind string

const (
	signalSpans   signalKind = "spans"
	signalMetrics signalKind = "metrics"
	signalLogs    signalKind = "logs"
)

var allSignalKinds = []signalKind{signalSpans, signalMetrics, signalLogs}

func defaultConfig() AppConfig {
	return AppConfig{
		TokenHeader:     "Authorization",
		IntervalSeconds: 5,
		EmitSpans:       true,
		EmitMetrics:     true,
		EmitLogs:        true,
		ResourceAttributes: map[string]string{
			"service.name": "otlpforge",
			"env":          "dev",
		},
		SpanName:           "otlpforge.demo.span",
		SpanKind:           "server",
		SpanMinDurationMs:  20,
		SpanMaxDurationMs:  80,
		SpanFailureRate:    5,
		SpanFailureMode:    "http",
		SpanFailureCode:    500,
		SpanFailureMessage: "simulated upstream service failure",
		SpanChildCount:     0,
		MetricName:         "otlpforge.requests.total",
		LogMessage:         "Hello from OTLPForge",
	}
}

func (a *App) LoadConfig() error {
	b, err := os.ReadFile(a.configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	var cfg AppConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return err
	}

	cfg = normalizeConfig(cfg)
	if err := validateConfig(cfg); err != nil {
		return err
	}

	a.cfg = cfg
	return nil
}

func (a *App) saveConfig(cfg AppConfig) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(a.configPath, b, 0o644)
}

func normalizeConfig(cfg AppConfig) AppConfig {
	if strings.EqualFold(strings.TrimSpace(cfg.Endpoint), legacyExampleEndpoint) {
		cfg.Endpoint = ""
	}
	if cfg.IntervalSeconds <= 0 {
		cfg.IntervalSeconds = 5
	}
	if strings.TrimSpace(cfg.TokenHeader) == "" {
		cfg.TokenHeader = "Authorization"
	}
	if cfg.ResourceAttributes == nil {
		cfg.ResourceAttributes = map[string]string{}
	}
	if strings.TrimSpace(cfg.SpanName) == "" {
		cfg.SpanName = "otlpforge.synthetic.span"
	}
	if strings.TrimSpace(cfg.SpanKind) == "" {
		cfg.SpanKind = "server"
	}
	if cfg.SpanMinDurationMs <= 0 {
		cfg.SpanMinDurationMs = 20
	}
	if cfg.SpanMaxDurationMs <= 0 {
		cfg.SpanMaxDurationMs = 80
	}
	if cfg.SpanMaxDurationMs < cfg.SpanMinDurationMs {
		cfg.SpanMaxDurationMs = cfg.SpanMinDurationMs
	}
	if cfg.SpanFailureRate < 0 {
		cfg.SpanFailureRate = 0
	}
	if cfg.SpanFailureRate > 100 {
		cfg.SpanFailureRate = 100
	}
	if strings.TrimSpace(cfg.SpanFailureMode) == "" {
		cfg.SpanFailureMode = "http"
	}
	if cfg.SpanFailureCode <= 0 {
		cfg.SpanFailureCode = 500
	}
	if strings.TrimSpace(cfg.SpanFailureMessage) == "" {
		cfg.SpanFailureMessage = "simulated upstream service failure"
	}
	if cfg.SpanChildCount < 0 {
		cfg.SpanChildCount = 0
	}
	if strings.TrimSpace(cfg.MetricName) == "" {
		cfg.MetricName = "otlpforge.requests.total"
	}
	if strings.TrimSpace(cfg.LogMessage) == "" {
		cfg.LogMessage = "OTLPForge synthetic log line"
	}
	return cfg
}

func validateConfig(cfg AppConfig) error {
	if !cfg.EmitSpans && !cfg.EmitMetrics && !cfg.EmitLogs {
		return errors.New("at least one signal type must be enabled")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.SpanKind)) {
	case "internal", "server", "client", "producer", "consumer":
	default:
		return errors.New("spanKind must be one of: internal, server, client, producer, consumer")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.SpanFailureMode)) {
	case "http", "timeout", "backend":
	default:
		return errors.New("spanFailureMode must be one of: http, timeout, backend")
	}
	return nil
}

func (cfg AppConfig) enabled(kind signalKind) bool {
	switch kind {
	case signalSpans:
		return cfg.EmitSpans
	case signalMetrics:
		return cfg.EmitMetrics
	case signalLogs:
		return cfg.EmitLogs
	default:
		return false
	}
}

func (cfg AppConfig) redacted() AppConfig {
	cfg.Token = ""
	return cfg
}

func (cfg AppConfig) hasToken() bool {
	return strings.TrimSpace(cfg.Token) != ""
}

func (cfg AppConfig) withPreservedSecret(next AppConfig) AppConfig {
	if strings.TrimSpace(next.Token) == "" {
		next.Token = cfg.Token
	}
	return next
}

func (cfg AppConfig) runtimeConfig() AppConfig {
	if endpoint := strings.TrimSpace(os.Getenv(envEndpoint)); endpoint != "" {
		cfg.Endpoint = endpoint
	}
	if token := strings.TrimSpace(os.Getenv(envToken)); token != "" {
		cfg.Token = token
	}
	if tokenHeader := strings.TrimSpace(os.Getenv(envTokenHeader)); tokenHeader != "" {
		cfg.TokenHeader = tokenHeader
	}
	return normalizeConfig(cfg)
}

func endpointFromEnv(cfg AppConfig) bool {
	return strings.TrimSpace(os.Getenv(envEndpoint)) != ""
}

func envTokenConfigured() bool {
	return strings.TrimSpace(os.Getenv(envToken)) != ""
}

func tokenHeaderFromEnv(cfg AppConfig) bool {
	return strings.TrimSpace(os.Getenv(envTokenHeader)) != ""
}

func endpointFor(base string, kind signalKind) string {
	base = strings.TrimSpace(base)
	if strings.Contains(base, "/v1/logs") || strings.Contains(base, "/v1/metrics") || strings.Contains(base, "/v1/traces") {
		return base
	}

	base = strings.TrimRight(base, "/")
	switch kind {
	case signalLogs:
		return base + "/v1/logs"
	case signalMetrics:
		return base + "/v1/metrics"
	default:
		return base + "/v1/traces"
	}
}
