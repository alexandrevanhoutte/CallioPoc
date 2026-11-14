# CallioPoc

Local, single-user AI receptionist POC. The browser streams PCM16 microphone audio over WebSocket to Go. CI-04 integrates **Gemini Live** in French, while keeping the original audio echo test mode.

## Requirements

- Go **1.23+** (Gemini Go SDK pinned to v1.26.0 for Go 1.23 compatibility)
- Modern browser (Chrome/Firefox/Safari) with microphone and AudioWorklet support
- Node.js 22+ recommended for frontend tests

## Local setup

```bash
cp .env.example .env
```

Open `.env` and **manually** set `GEMINI_API_KEY` to your own key. Do not commit or share it. The repository's `.gitignore` excludes `.env`, `.env.*` except `.env.example`.

```bash
go mod tidy
go run ./cmd/callio
```

Open <http://localhost:8080>. The server binds to loopback only; localhost is a secure browser context. Existing process environment variables take precedence over `.env` values. The server starts without a Gemini key, but **only Echo mode** is available until the key is supplied.

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `PORT` | `8080` | HTTP port |
| `AI_PROVIDER` | `gemini` | Provider; only Gemini implemented |
| `GEMINI_API_KEY` | empty | Secret, server-side only |
| `GEMINI_MODEL` | `gemini-3.8-live` | Live conversation model |

To switch model, change `GEMINI_MODEL`. Future providers can implement `realtime.VoiceProvider` and `realtime.VoiceSession` without changing transport handlers.

## Testing

1. Select **Echo** for the CI-03 loopback audio test (no API key needed).
2. Select **Gemini Live** and click **Start conversation**. Approve microphone access.
3. Speak French. The **Chunks sent** counter increases; the AI should reply audibly in French. **Chunks received** counts the output chunks.
4. Wear headphones to avoid sound feedback. Gemini audio playback is enabled on the user click; Echo playback remains an optional toggle.
5. Click **Stop**, then repeat. Verify microphone tracks, WebSocket and Gemini session all close.
6. Stop the backend during a session; it should release provider/browser resources.

**Audio contract:** Browser sends 20 ms chunks of mono PCM16 signed little-endian 16 kHz, **640 bytes/message**, to `GET /ws/live`. Go sends Gemini audio as binary PCM16 mono **24 kHz**, normally 960-byte chunks (final chunk can be shorter); JSON text messages carry non-audio events (`turn_end`, future transcripts). `GET /ws/audio` retains the existing 16 kHz echo protocol.

The interface defines `EventType` as numeric `iota + 1` with `NewEventType(string)` and `String()`. CI-05 implements robust interruption handling; CI-06 adds transcription.

## Validation commands

```bash
go mod tidy
go test ./...
go test -race ./...
go vet ./...
node --test tests/*.test.cjs tests/*.test.mjs
```

A manual microphone/AI call is essential. Gemini access, quotas and billing depend on your Google project. Do not paste the API key into GitHub issues, logs or chat.

## Layout

- `cmd/callio/` — dependency wiring (provider chosen once, at startup).
- `internal/config/` — `.env` and environment configuration.
- `internal/httpapi/` — `/ws/audio` echo and `/ws/live` generic real-time bridge.
- `internal/realtime/` — provider/session interfaces and typed events.
- `internal/realtime/gemini/` — Gemini Live SDK adapter.
- `web/` — browser microphone, WebSocket client, audio playback.
- `tests/` — frontend audio tests.
