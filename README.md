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
2. Select **Gemini Live** and click **Start conversation**. Grant microphone permission immediately: the assistant still speaks **first**, but you can interrupt its greeting while it is talking.
3. Speak French during or after the greeting. Gemini uses **500 ms** of detected silence to end your turn; responses are instructed to stay within one or two short French sentences. The **Chunks sent** counter increases after microphone access; **Chunks received** counts output chunks.
4. Wear headphones to avoid sound feedback. Gemini audio playback is enabled on the user click; Echo playback remains an optional toggle.
5. Click **Stop**, then repeat. Verify microphone tracks, WebSocket and Gemini session all close.
6. Stop the backend during a session; it should release provider/browser resources.

**Audio contract:** Browser sends 20 ms chunks of mono PCM16 signed little-endian 16 kHz, **640 bytes/message**, to `GET /ws/live`. Go sends Gemini audio as binary PCM16 mono **24 kHz**, normally 960-byte chunks (final chunk can be shorter); JSON text messages carry non-audio events (`turn_end`, future transcripts). `GET /ws/audio` retains the existing 16 kHz echo protocol.

The browser's AI status indicator is for local testing only, not the future telephone caller. The assistant greeting remains a separate initial content turn, while microphone capture starts immediately for full-duplex barge-in. The **500 ms** silence timeout may require adjustment if it interrupts natural pauses. Use headphones to avoid false barge-in caused by the model hearing its own output.

The interface defines `EventType` as numeric `iota + 1` with `NewEventType(string)` and `String()`. CI-05 assigns local backend turn IDs, drops queued/stale audio for interrupted turns, forwards Gemini's `interrupted` notification, and immediately flushes browser playback. A late `turn_end` following interruption is ignored, and WebSocket/session reuse continues normally. CI-06 adds transcription.

### Troubleshooting: Gemini's voice is chopped or silent

Gemini sends output in PCM16 24 kHz chunks, sometimes much faster than real-time playback. The browser now schedules up to **20 seconds** of audio without dropping the first few seconds of a burst; the earlier 250 ms limit silently discarded most speech.

Watch the new debug counters:
- **Chunks received** increases but **Playback dropped** increases: the playback queue is full or the browser audio context isn't running. Check the developer console for a one-time warning containing queued seconds/context state.
- **Silent model chunks** increases rapidly (fully zero PCM16 payloads): the upstream model may be returning silence, not a sample-rate conversion problem.
- **AI turn interruptions** increases during the model's reply: Gemini's VAD may be hearing the output through your microphone. Test with headphones; queued playback is flushed on interruption.
- **Chunks received** stays at zero: check Gemini model availability, server logs, and the WebSocket connection.

Live audio interruptions and all-zero output chunks are separate from WebSocket or audio-format errors, and the new counters make it possible to distinguish them.

## CI-05 interruption smoke test

1. Use **headphones** and select **Gemini Live**. When the greeting begins, say `Attends, je veux parler à Alexandre` before the AI finishes. It should stop and respond to the new request without reconnecting.
2. Ask for a longer explanation and interrupt the response in the middle. Remaining old audio should stop, and the next response should not overlap.
3. Repeat interruption three times, then click Stop and start again. The microphone and session must close cleanly.
4. Watch **AI turn interruptions**. It should increase on confirmed Gemini interruptions, while **Chunks dropped (playback)** should normally remain at zero.

The backend logs assistant turn starts/completions/interruptions with an internal `turn_id`. Binary audio frames remain compatible with CI-04; no JSON metadata is added to each chunk. Turn IDs are local diagnostics, not a guarantee that an external provider can identify every delayed audio frame. Browser playback tracking uses the real `AudioBufferSourceNode.onended` callback rather than assuming `turn_end` means all queued audio has finished playing.

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
