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

Press `?` in the app for the full reference.

| Key | Action |
|-----|--------|
| `↑` / `↓` or `j` / `k` | Navigate service list |
| `↵` Enter | Open the service editor (tab list) |
| `1` – `4` | Edit one tab directly, saves on submit |
| `n` | New service (guided through every tab) |
| `Space` | Toggle service enabled / disabled |
| `d` | Delete service (with confirm) |
| `r` | Start / stop sending |
| `t` | Send one test span and report the result |
| `g` | Global configuration (endpoint, token, attributes) |
| `?` | Keyboard reference |
| `q` | Quit |
| `Esc` | Leave the current form, then leave the editor |

### Service editor

The editor is split into four tabs, reachable from the tab list or directly with `1`–`4`:

| Tab | Contents |
|-----|----------|
| `1` Settings | Name, interval, failure rate, child spans, span kind, signals, enabled |
| `2` Templates | Span template (HTTP / DB / messaging / gRPC) and infrastructure template |
| `3` Resource attrs | Resource-level attributes |
| `4` Span attrs | Span-level overrides for the span template |

The tab list shows a summary of each tab and flags unsaved changes. `s` saves,
`Esc` backs out (and asks first if anything is unsaved).

Attributes marked `~` are inherited from the selected template and keep tracking
it; editing one turns it into an override, marked `✎`.

## Attributes

The attribute editors accept one `key=value` per line. Types are auto-detected:

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
| `OTGEN_ENDPOINT` | OTLP base URL, overrides saved config |
| `OTGEN_TOKEN` | API token, overrides saved config |

When either is set the header marks the affected field `[env]`, since the
environment wins over anything entered in the UI.

Config is saved to `config.json` in the working directory.

### Endpoint format

otgen appends `/v1/traces`, `/v1/metrics`, `/v1/logs` automatically unless the URL already contains a `/v1/` path.

### Dynatrace

- Endpoint: `https://<env-id>.live.dynatrace.com/api/v2/otlp`
- Token scopes required: `openTelemetryTrace.ingest`, `metrics.ingest`, `logs.ingest`
- Tokens are auto-prefixed with `Api-Token ` for the `Authorization` header

