package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	envEndpoint = "OTGEN_ENDPOINT"
	envToken    = "OTGEN_TOKEN"
)

// AttrValue is a typed OTLP attribute value.  The four scalar types from the
// OTel spec are supported: string, bool, int (int64), double (float64).
// In JSON it serialises as the native type so that config.json is readable:
//
//	"string" → JSON string     "hello"
//	"bool"   → JSON boolean    true / false
//	"int"    → JSON integer    42
//	"double" → JSON number     3.14
type AttrValue struct {
	Type   string // "string" | "bool" | "int" | "double"
	Str    string
	Bool   bool
	Int    int64
	Double float64
}

func (a AttrValue) MarshalJSON() ([]byte, error) {
	switch a.Type {
	case "bool":
		return json.Marshal(a.Bool)
	case "int":
		return json.Marshal(a.Int)
	case "double":
		return json.Marshal(a.Double)
	default:
		return json.Marshal(a.Str)
	}
}

func (a *AttrValue) UnmarshalJSON(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	switch b[0] {
	case 't', 'f': // JSON boolean literal
		var bv bool
		if err := json.Unmarshal(b, &bv); err != nil {
			return err
		}
		*a = boolAttrVal(bv)
		return nil
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		// JSON number: integer if no '.', 'e', or 'E'; double otherwise.
		raw := string(b)
		if !containsAny(raw, '.', 'e', 'E') {
			var iv int64
			if err := json.Unmarshal(b, &iv); err != nil {
				return err
			}
			*a = intAttrVal(iv)
			return nil
		}
		var fv float64
		if err := json.Unmarshal(b, &fv); err != nil {
			return err
		}
		*a = doubleAttrVal(fv)
		return nil
	default: // JSON string
		var sv string
		if err := json.Unmarshal(b, &sv); err != nil {
			return err
		}
		*a = strAttrVal(sv)
		return nil
	}
}

func containsAny(s string, chars ...byte) bool {
	for _, c := range chars {
		for i := 0; i < len(s); i++ {
			if s[i] == c {
				return true
			}
		}
	}
	return false
}

// Convenience constructors for the four OTel scalar attribute types.
func strAttrVal(v string) AttrValue    { return AttrValue{Type: "string", Str: v} }
func boolAttrVal(v bool) AttrValue     { return AttrValue{Type: "bool", Bool: v} }
func intAttrVal(v int64) AttrValue     { return AttrValue{Type: "int", Int: v} }
func doubleAttrVal(v float64) AttrValue { return AttrValue{Type: "double", Double: v} }

// Service defines a named synthetic service to emit OTLP signals for.
type Service struct {
	Name        string               `json:"name"`
	SpanKind    string               `json:"spanKind"`    // server|client|internal|producer|consumer
	FailureRate int                  `json:"failureRate"` // 0–100 %
	Signals     []string             `json:"signals"`     // "spans","metrics","logs"; empty = all three
	Attributes  map[string]AttrValue `json:"attributes"`  // service.name is always added automatically
	Enabled     bool                 `json:"enabled"`
}

// Config is the full application configuration schema.
type Config struct {
	Endpoint string    `json:"endpoint"`
	Token    string    `json:"token"`
	Interval int       `json:"interval"` // seconds between sends, default 5
	Services []Service `json:"services"`
}

// SignalStatus tracks send state for one signal kind within a service.
type SignalStatus struct {
	LastSentAt string `json:"lastSentAt,omitempty"`
	LastError  string `json:"lastError,omitempty"`
	SentCount  uint64 `json:"sentCount"`
}

// ServiceStatus groups signal statuses for one service.
type ServiceStatus struct {
	Spans   SignalStatus `json:"spans"`
	Metrics SignalStatus `json:"metrics"`
	Logs    SignalStatus `json:"logs"`
}

// RuntimeStatus is the top-level status payload returned by GET /api/status.
type RuntimeStatus struct {
	Running  bool                     `json:"running"`
	Services map[string]ServiceStatus `json:"services"`
}

type signalKind string

const (
	signalSpans   signalKind = "spans"
	signalMetrics signalKind = "metrics"
	signalLogs    signalKind = "logs"
)

// hasSignal returns true if the service should emit the given signal kind.
// An empty Signals slice means all three signals are enabled.
func (svc Service) hasSignal(kind signalKind) bool {
	if len(svc.Signals) == 0 {
		return true
	}
	for _, s := range svc.Signals {
		if signalKind(strings.ToLower(s)) == kind {
			return true
		}
	}
	return false
}

// redacted returns a copy of the config with the token cleared.
func (cfg Config) redacted() Config {
	cfg.Token = ""
	return cfg
}

// hasToken returns true if the config has a non-blank token.
func (cfg Config) hasToken() bool {
	return strings.TrimSpace(cfg.Token) != ""
}

// withPreservedSecret returns next with the current token copied in if next.Token is blank.
func (cfg Config) withPreservedSecret(next Config) Config {
	if strings.TrimSpace(next.Token) == "" {
		next.Token = cfg.Token
	}
	return next
}

// runtimeConfig applies OTGEN_ENDPOINT and OTGEN_TOKEN env overrides,
// then normalizes the result.
func (cfg Config) runtimeConfig() Config {
	if endpoint := strings.TrimSpace(os.Getenv(envEndpoint)); endpoint != "" {
		cfg.Endpoint = endpoint
	}
	if token := strings.TrimSpace(os.Getenv(envToken)); token != "" {
		cfg.Token = token
	}
	return normalizeConfig(cfg)
}

func defaultConfig() Config {
	return Config{
		Interval: 5,
		Services: []Service{{
			Name:        "otgen",
			SpanKind:    "server",
			FailureRate: 5,
			Signals:     []string{"spans", "metrics", "logs"},
			Attributes: map[string]AttrValue{
				"env":                 strAttrVal("dev"),
				"dt.cost.costcenter":  strAttrVal("test-cost-center"),
				"dt.security_context": strAttrVal("test-sec-ctxt"),
			},
			Enabled: true,
		}},
	}
}

func normalizeService(svc Service) Service {
	svc.Name = strings.TrimSpace(svc.Name)
	if strings.TrimSpace(svc.SpanKind) == "" {
		svc.SpanKind = "server"
	}
	if svc.FailureRate < 0 {
		svc.FailureRate = 0
	}
	if svc.FailureRate > 100 {
		svc.FailureRate = 100
	}
	if svc.Attributes == nil {
		svc.Attributes = map[string]AttrValue{}
	}
	return svc
}

func normalizeConfig(cfg Config) Config {
	if cfg.Interval <= 0 {
		cfg.Interval = 5
	}
	for i, svc := range cfg.Services {
		cfg.Services[i] = normalizeService(svc)
	}
	return cfg
}

func validateConfig(cfg Config) error {
	if len(cfg.Services) == 0 {
		return errors.New("at least one service is required")
	}
	seen := make(map[string]struct{}, len(cfg.Services))
	for i, svc := range cfg.Services {
		if svc.Name == "" {
			return fmt.Errorf("services[%d]: name is required", i)
		}
		if _, dup := seen[svc.Name]; dup {
			return fmt.Errorf("services[%d]: duplicate service name %q", i, svc.Name)
		}
		seen[svc.Name] = struct{}{}
		switch strings.ToLower(strings.TrimSpace(svc.SpanKind)) {
		case "internal", "server", "client", "producer", "consumer":
		default:
			return fmt.Errorf("services[%d]: spanKind must be one of: internal, server, client, producer, consumer", i)
		}
		for j, sig := range svc.Signals {
			switch strings.ToLower(sig) {
			case "spans", "metrics", "logs":
			default:
				return fmt.Errorf("services[%d].signals[%d]: must be one of: spans, metrics, logs", i, j)
			}
		}
	}
	return nil
}

// LoadConfig reads and applies config.json, normalizing and validating the result.
func (a *App) LoadConfig() error {
	b, err := os.ReadFile(a.configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	var cfg Config
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

// saveConfig writes cfg to config.json as indented JSON.
func (a *App) saveConfig(cfg Config) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(a.configPath, b, 0o644)
}

// endpointFor returns the full OTLP endpoint URL for the given signal kind.
// If base already contains a /v1/ path it is used as-is; otherwise the
// per-signal path is appended.
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

// endpointFromEnv reports whether OTGEN_ENDPOINT is set in the environment.
func endpointFromEnv() bool {
	return strings.TrimSpace(os.Getenv(envEndpoint)) != ""
}

// envTokenConfigured reports whether OTGEN_TOKEN is set in the environment.
func envTokenConfigured() bool {
	return strings.TrimSpace(os.Getenv(envToken)) != ""
}
