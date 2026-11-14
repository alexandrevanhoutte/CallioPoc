# CallioPoc

Local single-user AI receptionist POC. The browser captures PCM16 audio and streams it to Go over WebSocket. For now, Go echoes the exact audio bytes back; a Gemini Live session will replace this in CI-04.

## Requirements

- Go 1.23 or newer
- Chrome, Firefox or Safari with microphone and AudioWorklet support
- Optional: Node.js 22+ for browser module unit tests

## Run

From the repository root:

```bash
go mod tidy
go run ./cmd/callio
```

Open <http://localhost:8080>. The HTTP server binds to `127.0.0.1` for local-only use; localhost is a secure context for microphone access.

## Manual streaming test

1. Click **Check health** to verify that Go is running.
2. Click **Start microphone & streaming** and grant microphone permission. WebSocket should show **Connected** and microphone **Recording**.
3. Speak. The audio level should move. **Chunks sent** and **Chunks received** should both increase continuously (approximately 50 per second) as the Go backend echoes audio.
4. To hear the returned audio, use **headphones**, then check **Play echoed audio**. This is disabled by default to prevent speaker feedback.
5. Click **Stop**. Confirm the microphone indicator turns off and WebSocket becomes **Disconnected**. Start again to verify resource cleanup.
6. Stop the Go server while the microphone is active: the browser should show a disconnect and release microphone resources.

### Binary audio protocol

Each WebSocket binary message to `GET /ws/audio` is **exactly 640 bytes** of signed **PCM16 little-endian**, **mono**, **16 kHz** audio (320 samples / 20 ms). The Go endpoint echoes the identical bytes. No JSON, Base64, disk recording or AI processing is involved yet.

Invalid message types/sizes are rejected. The browser limits buffered WebSocket output to around 32 KiB and **drops new chunks** when the queue is backed up instead of accumulating latency; the **Chunks dropped** counter reports this. Playback has a separate 250 ms queue cap.

### Tests

```bash
go test ./...
go test -race ./...
go vet ./...
node --test tests/*.test.cjs tests/*.test.mjs
```

Browser testing is still required to validate actual permissions, capture and audio output hardware.

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `PORT` | `8080` | HTTP port (1–65535) |

Example: `PORT=9090 go run ./cmd/callio`. `.env.example` documents settings; `.env` is not loaded automatically.

## Layout

- `cmd/callio/` — application entrypoint and dependency composition.
- `internal/config/` — configuration.
- `internal/httpapi/` — HTTP routing, WebSocket echo handler and server lifecycle.
- `web/` — static test page, microphone capture, WebSocket client, audio playback.
- `tests/` — dependency-free Node tests for browser audio modules.

The Go HTTP handler stays small and deliberately does not introduce a service abstraction before CI-04. WebSocket connections are closed on stop, disconnect or application shutdown.
