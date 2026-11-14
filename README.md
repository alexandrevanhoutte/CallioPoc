# CallioPoc

Minimal single-user browser-based AI receptionist proof of concept. The Go HTTP server serves a plain HTML/JS test page with local microphone capture. WebSocket streaming and Gemini Live come in later tickets.

## Requirements

- Go 1.23 or newer
- Modern browser supporting `getUserMedia` and `AudioWorklet` (Chrome, Firefox, or Safari)
- Optional: Node.js to run the AudioWorklet unit tests

No external Go dependencies, JavaScript packages, or frontend build step are required.

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

## Test microphone capture

1. Click **Start microphone** and allow browser microphone access.
2. Speak into the microphone. **Input level** should respond to your voice, and **Sound detected** should appear when the signal is strong enough.
3. **Audio chunks received** should count up approximately 50 times per second, even during silence.
4. Click **Stop microphone**. The browser's microphone usage indicator should turn off.
5. Start again to confirm that tracks and AudioContext were released.

The browser emits 20 ms chunks of **PCM signed 16-bit, little-endian, mono, 16 kHz** audio (320 samples / 640 bytes each). WebAudio's native capture rate is resampled inside `web/audio-processor.js` before encoding. Audio is not saved or sent to Go yet.

Microphone capture works on localhost, which browsers treat as a secure context; remote access requires HTTPS. Permission denial or unavailable devices are displayed in the page.

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `PORT` | `8080` | HTTP port (1–65535) |

Example: `PORT=9090 go run ./cmd/callio`. The `.env.example` file documents settings; `.env` files are not loaded automatically.

## Tests

```bash
go test ./...
go vet ./...
node --test tests/audio-processor.test.cjs
```

Stop with Ctrl+C (or send SIGTERM) for a graceful HTTP shutdown.

## Layout

- `cmd/callio/` — application entrypoint and dependency composition.
- `internal/config/` — environment configuration.
- `internal/httpapi/` — HTTP routing, health handler and server lifecycle.
- `web/` — plain HTML, JavaScript, CSS, and microphone/audio processing.
- `tests/` — no-dependency AudioWorklet unit tests using Node's built-in test runner.

This POC deliberately has no database, auth, WebSocket, or AI integration yet. Future dependencies should be constructed in `main` and passed to their consumers instead of being initialized inside HTTP handlers.
