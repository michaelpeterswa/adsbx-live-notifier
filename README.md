# adsbx-live-notifier

Watches a local [ADS-B Exchange](https://www.adsbexchange.com/) feeder for
specific aircraft (by tail number or ICAO hex) and dispatches push
notifications via [Pulsar](https://github.com/michaelpeterswa) → Pushover when
they appear on the air.

## Features

- Dual ingest: HTTP poller for `aircraft.json` (tar1090) and TCP streamer for
  SBS BaseStation (`:30003`)
- Per-aircraft cooldown (TTL-cached) so a single overflight produces one alert
- Pulsar webhook delivery with idempotency keys for server-side dedupe
- Structured JSON logging with configurable log levels via [slog](https://pkg.go.dev/log/slog)
- OpenTelemetry metrics and tracing via [ootel](https://alpineworks.io/ootel)
- Runtime and host metrics instrumentation
- Environment-based configuration via [env](https://github.com/caarlos0/env)
- Multi-stage Docker build with distroless final image
- Local development setup with Grafana LGTM stack

## Configuration

Everything except the watchlist (planes + descriptions) is set via environment
variables.

### Core / observability

| Variable | Description | Default |
|----------|-------------|---------|
| `LOG_LEVEL` | Logging level (debug, info, warn, error) | `error` |
| `METRICS_ENABLED` | Enable Prometheus metrics | `true` |
| `METRICS_PORT` | Port for metrics endpoint | `8081` |
| `LOCAL` | Use OTLP gRPC exporter instead of Prometheus | `false` |
| `TRACING_ENABLED` | Enable distributed tracing | `false` |
| `TRACING_SAMPLERATE` | Trace sampling rate | `0.01` |
| `TRACING_SERVICE` | Service name for traces | `adsbx-live-notifier` |
| `TRACING_VERSION` | Service version for traces | - |

### ADS-B feeder

| Variable | Description | Default |
|----------|-------------|---------|
| `ADSBX_HOST` | Feeder hostname or IP (used to derive URLs below) | `adsbexchange.local` |
| `ADSBX_JSON_URL` | Full `aircraft.json` URL (overrides derived) | `http://${ADSBX_HOST}/tar1090/data/aircraft.json` |
| `ADSBX_SBS_ADDR` | SBS BaseStation host:port (overrides derived) | `${ADSBX_HOST}:30003` |
| `ADSBX_POLL_INTERVAL` | HTTP poll interval | `1s` |
| `ADSBX_JSON_ENABLED` | Enable HTTP poller | `true` |
| `ADSBX_SBS_ENABLED` | Enable SBS streamer | `true` |

### Watchlist & notifier

The watchlist file is the **only** non-env-var configuration: a JSON file that
lists planes (`tail` and/or `hex`) plus an optional `description` per entry.
All cooldown / credential / endpoint settings are env-driven.

| Variable | Description | Default |
|----------|-------------|---------|
| `WATCHLIST_PATH` | Path to watchlist JSON file. If empty, alerting is disabled. | - |
| `WATCHLIST_COOLDOWN` | Minimum interval between alerts for the same entry | `10m` |
| `PULSAR_URL` | Base URL of Pulsar notifications service (required if watchlist set) | - |
| `PULSAR_BEARER_TOKEN` | Pulsar writer bearer token (required if watchlist set) | - |
| `PULSAR_PUSHOVER_USER_KEY` | 30-char Pushover user/group key (required if watchlist set) | - |
| `PULSAR_PRIORITY` | Pushover priority (-2..2) | `0` |

### Watchlist file format

```json
{
  "aircraft": [
    { "tail": "N12345", "description": "Friend's Cessna 172" },
    { "hex":  "a1b2c3", "description": "Notable airframe" }
  ]
}
```

Each entry must include at least one of `tail` or `hex`. `description` is
optional and is included in the alert body.

## Getting Started

### Run Locally with Docker Compose

```bash
cp watchlist.example.json watchlist.json
# edit watchlist.json
docker-compose up
```

This starts the application along with the Grafana LGTM (Loki, Grafana, Tempo,
Mimir) stack for local observability:

- **Application**: Port 8081 (metrics)
- **Grafana UI**: http://localhost:3000
- **OTLP gRPC**: Port 4317
- **OTLP HTTP**: Port 4318

### Build and Run

```bash
go build -o adsbx-live-notifier ./cmd/adsbx-live-notifier
./adsbx-live-notifier
```

## Project Structure

```
.
├── cmd/adsbx-live-notifier/   # Application entrypoint
├── internal/
│   ├── adsbx/                 # JSON poller, SBS streamer, Aircraft type
│   ├── config/                # Environment-based configuration
│   ├── logging/               # Logging utilities
│   ├── metrics/               # Application metrics
│   ├── notifier/              # Pulsar notifier
│   └── watchlist/             # Watchlist file loader & match cache
├── docker/
│   └── grafana/               # Grafana dashboard provisioning
├── Dockerfile                 # Multi-stage build
└── docker-compose.yml         # Local development stack
```

## Metrics

| Metric | Description |
|--------|-------------|
| `adsbx_messages_received_total` | Total messages received, labeled by `source` (`json`/`sbs`) |
| `adsbx_watchlist_matches_total` | Total watchlist matches (pre-cooldown), labeled by `label` |
| `adsbx_alerts_fired_total` | Total alerts dispatched (post-cooldown), labeled by `label` |

Plus standard Go runtime and host metrics from the OpenTelemetry contrib
packages.

## CI/CD

Pull requests are validated with:

- **commitlint**: Conventional commit message enforcement
- **golangci-lint**: Go linting
- **yamllint**: YAML linting
- **hadolint**: Dockerfile linting
- **go test**: Unit tests
