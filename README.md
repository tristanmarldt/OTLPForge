# otgen

A lightweight synthetic OTLP data generator with a terminal UI.

Run it, point it at an endpoint, watch spans · metrics · logs flow. Useful for testing Dynatrace ingest pipelines, validating dashboards, or load-testing collectors without needing a real application.

```
  otgen v0.2.2  ● running
  https://xxx.live.dynatrace.com/api/v2/otlp
  ────────────────────────────────────────────────────────────────────────

▶ ● checkout-svc  server  [http-server]  [k8s]  5s  5% err  +3 local child
    spans↑142  metrics↑142  logs↑142
    res  ~ k8s.cluster.name=my-cluster  k8s.pod.uid=a1b2c3d4-…  +10
    span ~ http.request.method=GET  url.scheme=https  +4
  ● payment-svc  client  [grpc]  [eks]  5s  10% err
    spans↑139  metrics↑139  logs↑139
  ○ flaky-worker  consumer  [messaging]  2s  80% err
    disabled — press space to enable

  n new · ↵ edit · d delete · ␣ toggle · r run/stop · t test · g config · ? help · q quit
```

## Features

- **Service-centric model** — each service emits independently with its own interval, span kind, failure rate, local child spans, signal selection, mesh options, and attributes
- **Distributed scenarios** — connect services with downstream calls to generate one shared trace across an acyclic service graph
- **Configurable signals** — choose sum, gauge, or histogram metrics and DEBUG/INFO/WARN/ERROR log severity
- **Semantic-convention templates** — HTTP, database, messaging and gRPC spans carry the right OTel attributes so Dynatrace detects the technology
- **Istio mesh telemetry** — optionally add Istio workload semantics to spans/resources and standard mesh metrics to the Metrics signal
- **Infrastructure templates** — Kubernetes (incl. EKS / GKE / AKS / OpenShift), ECS, Docker, Lambda, Cloud Foundry and more, matching what the Dynatrace collector's `k8sattributesprocessor` and Operator inject
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
| `n` | Create a service |
| `Space` | Toggle service enabled / disabled |
| `d` | Delete service (with confirm) |
| `r` | Start / stop sending |
| `t` | Send one test span and report the result |
| `g` | Global configuration (endpoint, token, attributes) |
| `?` | Keyboard reference |
| `q` | Quit |
| `Esc` | Leave the current form, then leave the editor |

### Service editor

The editor is split into seven tabs, reachable from the tab list or directly with `1`–`7`:

| Tab | Contents |
|-----|----------|
| `1` Service | Name, interval, failure rate, signals, Istio mesh |
| `2` Spans | Span template, span kind, local child spans |
| `3` Calls | One downstream service name per line; calls must be acyclic |
| `4` Metrics & logs | Metric type/name/unit and log severity |
| `5` Infrastructure | Kubernetes (incl. EKS / GKE / AKS / OpenShift), containers, serverless, host |
| `6` Resource attrs | Resource-level overrides |
| `7` Span attrs | Span-level overrides for the span template |

The tab list shows a summary of each tab and flags unsaved changes. `s` saves,
`Esc` backs out (and asks first if anything is unsaved).

From **inside** a tab you can switch without going back to the list:

| Key | Action |
|-----|--------|
| `Ctrl+R` | Next tab |
| `Esc` | Back to the tab list |

`Ctrl+1`–`7` is deliberately not used: terminals cannot transmit it — `Ctrl+1`
sends nothing at all and `Ctrl+3` arrives as `Esc`.

Attributes marked `~` are inherited from the selected template. Attribute
editors contain only explicit overrides, marked `✎`.

New configurations start with no custom resource attributes. Add values such as
`env`, `dt.cost.costcenter`, or `dt.security_context` in Global configuration
when they should apply to every service, or in Resource attrs for one service.

The **Istio mesh telemetry** toggle adds workload, namespace, principal,
canonical-service, and cluster attributes plus `istio_requests_total`, request
duration, request size, and response size to the existing Metrics signal.

Downstream calls are additive service relationships. For example:

```json
{
  "name": "checkout-svc",
  "downstreamCalls": ["payment-svc"],
  "metric": {"type": "histogram", "unit": "ms"},
  "logSeverity": "info",
  "mesh": true
}
```

The target's `enabled` value controls only its standalone scheduler; it can
still appear in a trace started by another enabled service. A target with logs
enabled contributes one log linked to its generated service span. Cycles,
self-calls, duplicate edges, and missing targets are rejected.

Call edges are synthetic OTLP client spans, not real network requests. They
inherit protocol semantics from the target template: HTTP templates add HTTP
client attributes, gRPC adds RPC attributes, and generic targets keep the
minimal edge attributes.

Metric defaults are `<service>.requests.total` (`sum`), `<service>.load`
(`gauge`), and `<service>.request.duration` (`histogram`). Histogram values are
delta observations in milliseconds. Logs default to `INFO`; failed service
spans promote lower configured severities to `ERROR`.

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
environment wins over anything entered in the UI. In global configuration,
clearing the token field clears the saved token.

Config is saved to `config.json` in the working directory.

### Endpoint format

otgen appends `/v1/traces`, `/v1/metrics`, `/v1/logs` automatically unless the URL already contains a `/v1/` path.

### Dynatrace

- Endpoint: `https://<env-id>.live.dynatrace.com/api/v2/otlp`
- Token scopes required: `openTelemetryTrace.ingest`, `metrics.ingest`, `logs.ingest`
- Tokens are auto-prefixed with `Api-Token ` for the `Authorization` header
