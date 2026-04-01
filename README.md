# OTLPForge

Minimal OTLP data generator with a tiny web admin panel.

It sends synthetic OTLP **spans**, **metrics**, and **logs** continuously to a configured endpoint. You can set endpoint + token in the frontend and use one global config (shared attributes plus emit checkboxes).

Dynatrace-compatible transport is used:

- HTTP OTLP only (no gRPC)
- Protocol Buffers binary payloads (`application/x-protobuf`)
- JSON OTLP payloads are not used

## Why this project

- Very small footprint (Go stdlib only)
- Single binary deployment
- Optional container image
- Frontend is only config/admin; background sender runs continuously in the same process

## UI model (minimal)

- One global config pane
- Checkboxes to enable/disable `spans`, `metrics`, `logs`
- One shared `resourceAttributes` JSON for all signal types
- Global interval plus optional names/message fields
- Span controls for kind, random duration range, child span count, failure rate, failure mode, failure status code, and failure message

## Run locally

```bash
go run .
```

Open `http://localhost:8080`.

### Span simulation

Spans can be configured to:

- use `internal`, `server`, `client`, `producer`, or `consumer` span kinds
- emit random durations within a configured min/max range
- generate parent/child traces with a configurable child span count
- fail by mode: `http`, `timeout`, or `backend`

### Notes for Dynatrace

- Put your Dynatrace token in the Token field.
- Keep `Token Header` as `Authorization` unless you need something else.
- With `Authorization`, OTLPForge auto-prefixes your token with `Api-Token `.
- Endpoint can be either:
  - OTLP base endpoint (OTLPForge appends `/v1/logs`, `/v1/metrics`, `/v1/traces`)
  - Full signal endpoint path directly

### Environment Variables

You can also provide runtime config via environment variables:

- `OTLPFORGE_ENDPOINT`
- `OTLPFORGE_TOKEN`
- `OTLPFORGE_TOKEN_HEADER`

These override the corresponding saved UI or `config.json` values at runtime.

Config precedence:

- `OTLPFORGE_ENDPOINT`, `OTLPFORGE_TOKEN`, and `OTLPFORGE_TOKEN_HEADER` win when set
- saved UI / `config.json` values are used when the env var is not set

When an env var override is active, the UI shows the effective runtime value and marks that field as env-controlled.

The config API does not return the saved token value. The UI shows whether a token is configured, but the secret itself is redacted.

## Build binary

```bash
go build -o otlpforge .
./otlpforge
```

## Docker

```bash
docker build -t otlpforge:latest .
docker run --rm -p 8080:8080 otlpforge:latest
```

Docker with env-based Dynatrace config:

```bash
docker run --rm -p 8080:8080 \
  -e OTLPFORGE_ENDPOINT="https://your-env.live.dynatrace.com/api/v2/otlp" \
  -e OTLPFORGE_TOKEN="dt0c01..." \
  -e OTLPFORGE_TOKEN_HEADER="Authorization" \
  otlpforge:latest
```

If you want `config.json` persistence in Docker, mount a writable working directory:

```bash
docker run --rm -p 8080:8080 \
  -v "$PWD:/app-data" \
  -w /app-data \
  otlpforge:latest
```

## Config persistence

Saved config is written to `config.json` in the working directory whenever you click **Save Config** or **Start Sending**.

## API (optional)

- `GET /api/config`
- `POST /api/config`
- `POST /api/start`
- `POST /api/stop`
- `GET /api/status`

## Security warning

`Skip TLS verification` is only for test labs/self-signed certs. Do not use in production.
