# OTLPForge

A lightweight synthetic OTLP data generator with a terminal UI.

Run it, point it at an endpoint, watch spans · metrics · logs flow. Useful for testing Dynatrace ingest pipelines, validating dashboards, or load-testing collectors without needing a real application.

```
OTLPForge v0.1.0  ● running
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
- **Four OTel attribute types** — `string`, `bool`, `int64`, `double` — all on the wire as native OTLP types
- **TUI** — keyboard-driven, live status counters, no browser required
- **Headless mode** — runs without a TTY (Docker, CI) as an HTTP API server
- **Dynatrace-compatible** — HTTP OTLP only, protobuf, auto-prefixes `Api-Token`

## Quick start

```bash
go install github.com/tristanmarldt/OTLPForge@latest
otlpforge
```

Press `g` to set your endpoint and token, `n` to add a service, `r` to start.

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
version="1.0"                # string (quoted to prevent int/float detection)
```

All four types pass through to the OTLP wire format as native `AnyValue` types.

## Configuration

### Environment variables

| Variable | Description |
|----------|-------------|
| `OTLPFORGE_ENDPOINT` | OTLP base URL, overrides saved config |
| `OTLPFORGE_TOKEN` | API token, overrides saved config |
| `PORT` | HTTP API port (default `8080`) |

Config precedence: env vars → saved `config.json`.

The token is never returned by the API — it is always redacted from responses.

### Endpoint format

OTLPForge appends `/v1/traces`, `/v1/metrics`, `/v1/logs` automatically unless the URL already contains a `/v1/` path.

### Dynatrace

- Endpoint: `https://<env-id>.live.dynatrace.com/api/v2/otlp`
- Token: a DT API token with `openTelemetryTrace.ingest`, `metrics.ingest`, `logs.ingest` scopes
- OTLPForge auto-prefixes tokens with `Api-Token ` when using the `Authorization` header

## Docker

In Docker (no TTY) OTLPForge runs in headless mode: TUI is skipped, the HTTP API listens on `:8080`, and the sender starts automatically if an endpoint is configured.

```bash
docker run --rm \
  -e OTLPFORGE_ENDPOINT="https://xxx.live.dynatrace.com/api/v2/otlp" \
  -e OTLPFORGE_TOKEN="dt0c01.***" \
  ghcr.io/tristanmarldt/otlpforge:latest
```

Mount a directory to persist `config.json`:

```bash
docker run --rm \
  -v "$PWD:/data" -w /data \
  -e OTLPFORGE_ENDPOINT="..." \
  -e OTLPFORGE_TOKEN="..." \
  ghcr.io/tristanmarldt/otlpforge:latest
```

### Build and run locally

```bash
docker build -t otlpforge:latest .
docker run --rm -p 8080:8080 \
  -e OTLPFORGE_ENDPOINT="..." \
  -e OTLPFORGE_TOKEN="..." \
  otlpforge:latest
```

## HTTP API

Runs alongside the TUI (port 8080 by default). Useful for scripting or CI.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/config` | Read current config (token redacted) |
| `POST` | `/api/config` | Write config (JSON body) |
| `POST` | `/api/start` | Start the sender |
| `POST` | `/api/stop` | Stop the sender |
| `GET` | `/api/status` | Running state and per-service counters |

## Build from source

```bash
git clone https://github.com/tristanmarldt/OTLPForge.git
cd OTLPForge
go build -o otlpforge .
./otlpforge
```

Requires Go 1.24+. No CGO, no external system libraries.

## Design constraints

- HTTP OTLP only (no gRPC)
- Protobuf binary payloads (`application/x-protobuf`)
- Go standard library + OTLP protobuf types + Bubble Tea TUI stack
- Single binary, single file config (`config.json` in the working directory)
