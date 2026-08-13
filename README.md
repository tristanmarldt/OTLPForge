# otgen

A lightweight synthetic OTLP data generator with a terminal UI.

Run it, point it at an endpoint, watch spans · metrics · logs flow. Useful for testing Dynatrace ingest pipelines, validating dashboards, or load-testing collectors without needing a real application.

```
otgen v0.1.0  ● running
  https://xxx.live.dynatrace.com/api/v2/otlp  interval: 5s
────────────────────────────────────────────────────────────────────────

▶ ● checkout-svc   server  5% err
    spans↑142  metrics↑142  logs↑142

  ● payment-svc    client  10% err
    spans↑139  metrics↑139  logs↑139

  ○ flaky-worker   consumer  80% err  (disabled)
    spans↑0

  n new  ·  ↵ edit  ·  a attrs  ·  d delete  ·  ␣ toggle  ·  r run/stop  ·  g settings  ·  q quit
```

## Features

- **Service-centric model** — each service emits independently with its own span kind, failure rate, signal selection, and resource attributes
- **Four OTel attribute types** — `string`, `bool`, `int64`, `double` — sent as native OTLP `AnyValue` types on the wire
- **Keyboard-driven TUI** — live status counters, no browser required
- **Dynatrace-compatible** — HTTP OTLP, protobuf, auto-prefixes `Api-Token`

## Install

```bash
go install github.com/tristanmarldt/otgen@latest
otgen
```

Press `g` to set your endpoint and token, `n` to add a service, `r` to start.

### Build from source

```bash
git clone https://github.com/tristanmarldt/otgen.git
cd otgen
go build -o otgen .
./otgen
```

Requires Go 1.24+.

## Key bindings

| Key | Action |
|-----|--------|
| `↑` / `↓` or `j` / `k` | Navigate service list |
| `↵` Enter | Edit service (full editor) |
| `a` | Edit resource attributes only |
| `Space` | Toggle service enabled / disabled |
| `r` | Start / stop sending |
| `n` | New service |
| `d` | Delete service (with confirm) |
| `g` | Global settings (endpoint, token, interval) |
| `q` | Quit |
| `Esc` | Cancel / back (in any editor) |

## Resource attributes

The attribute editor accepts one `key=value` per line. Types are auto-detected:

```
env=staging                  # string
feature.enabled=true         # bool
http.port=8080               # int64
sampling.ratio=0.25          # double
version="1.0"                # string (quoted to prevent numeric detection)
```

## Configuration

### Environment variables

| Variable | Description |
|----------|-------------|
| `OTLPFORGE_ENDPOINT` | OTLP base URL, overrides saved config |
| `OTLPFORGE_TOKEN` | API token, overrides saved config |
| `PORT` | HTTP API port (default `8080`) |

Config is saved to `config.json` in the working directory. The token is always redacted from API responses.

### Endpoint format

otgen appends `/v1/traces`, `/v1/metrics`, `/v1/logs` automatically unless the URL already contains a `/v1/` path.

### Dynatrace

- Endpoint: `https://<env-id>.live.dynatrace.com/api/v2/otlp`
- Token scopes required: `openTelemetryTrace.ingest`, `metrics.ingest`, `logs.ingest`
- Tokens are auto-prefixed with `Api-Token ` for the `Authorization` header

## HTTP API

Runs alongside the TUI on port 8080. Useful for scripting.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/config` | Read config (token redacted) |
| `POST` | `/api/config` | Write config |
| `POST` | `/api/start` | Start the sender |
| `POST` | `/api/stop` | Stop the sender |
| `GET` | `/api/status` | Running state and per-service counters |
