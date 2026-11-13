# CallioPoc

Minimal single-user browser-based AI receptionist proof of concept. CI-01 sets up the Go HTTP server and a static test page; microphone, WebSocket streaming and Gemini Live arrive in later tickets.

## Requirements

- Go 1.23 or newer

No external Go dependencies or frontend build step are required for CI-01.

## Run

From the repository root:

```bash
go run ./cmd/callio
```

Open <http://localhost:8080>. Click **Check health** to test the backend, or run:

```bash
curl http://localhost:8080/health
# {"status":"ok"}
```

The server binds to `127.0.0.1` for local-only access.

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `PORT` | `8080` | HTTP port (1–65535) |

Example: `PORT=9090 go run ./cmd/callio`. The `.env.example` file documents settings; `.env` files are not loaded automatically.

## Test

```bash
go test ./...
go vet ./...
```

Stop with Ctrl+C (or send SIGTERM) for a graceful HTTP shutdown.

## Layout

- `cmd/callio/` — application entrypoint and dependency composition.
- `internal/config/` — environment configuration.
- `internal/httpapi/` — HTTP routing, health handler and server lifecycle.
- `web/` — plain HTML, JavaScript and CSS test UI.

This bootstrap deliberately has no database, auth, WebSocket or AI integration yet. Future dependencies should be constructed in `main` and passed to their consumers instead of being initialized inside HTTP handlers.
