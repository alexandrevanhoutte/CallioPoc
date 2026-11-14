# CallioPoc

Local, single-user AI receptionist POC. The browser streams PCM16 microphone audio over WebSocket to Go. CI-04 integrates **Gemini Live** with a configurable greeting, while keeping the original audio echo test mode.

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

The dark local playground automatically checks `GET /capabilities`, which reports only `status` and `live_configured`; it never exposes keys. Select **Echo** to check the microphone without credentials, or **Gemini Live** to speak to Callio. Start a call, allow browser microphone access, and end it using the on-screen control. The transcript and post-call summary remain available until the next call or reload. The collapsed **Advanced** panel contains streaming counters and optional Echo playback. No real phone transfer or PSTN call is made.

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `PORT` | `8080` | HTTP port |
| `AI_PROVIDER` | `gemini` | Provider; only Gemini implemented |
| `GEMINI_API_KEY` | empty | Secret, server-side only |
| `GEMINI_MODEL` | `gemini-3.8-live` | Live conversation model |
| `GEMINI_SUMMARY_MODEL` | `gemini-3.5-flash-lite` | Text model used only after an AI call completes |
| `CALL_GREETING_LOCALE` | `fr-FR` if unset | First greeting only: `fr-FR`, `en-US`, or `ko-KR` (`.env.example` uses `ko-KR`) |

To switch model, change `GEMINI_MODEL`. Future providers can implement `realtime.VoiceProvider` and `realtime.VoiceSession` without changing transport handlers.

## Testing

1. Select **Echo** for the CI-03 loopback audio test (no API key needed).
2. Select **Gemini Live** and click **Start conversation**. Grant microphone permission immediately: the assistant still speaks **first**, but you can interrupt its greeting while it is talking.
3. Speak French, Korean or English during or after the greeting, including switching mid-call. Gemini uses **500 ms** of detected silence to end your turn; responses are instructed to stay within one or two short sentences in the caller's current language. The **Chunks sent** counter increases after microphone access; **Chunks received** counts output chunks.
4. Wear headphones to avoid sound feedback. Gemini audio playback is enabled on the user click; Echo playback remains an optional toggle.
5. Click **Stop**, then repeat. Verify microphone tracks, WebSocket and Gemini session all close.
6. Stop the backend during a session; it should release provider/browser resources.

**Audio contract:** Browser sends 20 ms chunks of mono PCM16 signed little-endian 16 kHz, **640 bytes/message**, to `GET /ws/live`. Go sends Gemini audio as binary PCM16 mono **24 kHz**, normally 960-byte chunks (final chunk can be shorter); JSON text messages carry non-audio events (`turn_end`, future transcripts). `GET /ws/audio` retains the existing 16 kHz echo protocol.

The browser's AI status indicator is for local testing only, not the future telephone caller. The assistant greeting remains a separate initial content turn, while microphone capture starts immediately for full-duplex barge-in. The **500 ms** silence timeout may require adjustment if it interrupts natural pauses. Use headphones to avoid false barge-in caused by the model hearing its own output.

The interface defines `EventType` as numeric `iota + 1` with `NewEventType(string)` and `String()`. CI-05 assigns local backend turn IDs, drops queued/stale audio for interrupted turns, forwards Gemini's `interrupted` notification, and immediately flushes browser playback. A late `turn_end` following interruption is ignored, and WebSocket/session reuse continues normally. CI-06 adds Gemini input/output transcription events, per-speaker segments, and a live transcript panel.

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

## CI-06 — Live transcript

Gemini Live's `InputAudioTranscription` and `OutputAudioTranscription` are enabled on the existing voice session. Transcription is **best effort**: speech recognition and generation text can be delayed, revised or unavailable even when audio works.

The backend forwards JSON WebSocket events alongside the existing binary PCM16 messages:

```json
{
  "type": "transcript",
  "segment_id": "caller-1",
  "order": 2,
  "speaker": "CALLER",
  "text": "Bonjour, je voudrais parler à Alexandre.",
  "final": true,
  "timestamp": "2026-11-14T12:00:00Z"
}
```

`AI` is the other supported speaker. Events with the same `segment_id` replace earlier provisional text. A `final: true` segment is immutable. A final segment is immutable once emitted. Output transcription may finalize on the AI generation boundary when Gemini omits `Finished`; caller transcription is handled independently. Input/output transcript timestamps may not align with audio.

The `realtime.Transcript` collector exposes `Snapshot()` and `Finalized()` for CI-09/CI-10. The browser keeps transcript state in memory and retains it after Stop until the next conversation or page reload. **No file or database persistence** is added in CI-06. Unfinished interrupted AI speech is displayed as provisional and excluded from finalized snapshots.

Manual check: begin a Gemini Live conversation, confirm the greeting appears as Callio and spoken French appears as Caller; interrupt Callio, speak again, click Stop, and verify there are no duplicated lines and no transcript data in the filesystem.

Gemini does not always set `Transcription.Finished`. Output transcription is finalized on `GenerationComplete` (or `TurnComplete` as a fallback); input transcription is independently timed and is finalized on `Finished` or the end of the related completed model turn when available. Gemini transcription text is assembled as **incremental deltas**, so repeated words are preserved. A reserved `order` lets delayed caller text appear before the subsequent assistant turn, even when the associated JSON message arrives later. No content-based deduplication is attempted. Interrupted assistant speech remains provisional. This ordering is best-effort: Gemini does not give a universal utterance ID for correlating arbitrarily late or overlapping caller input, and recognition can be inaccurate.

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

## CI-08: simulated human takeover

The local browser POC supports a simulated handoff without a phone operator.

1. Start a Gemini Live conversation and ask to speak to the owner.
2. When `handoff_required` arrives, the **Conversation** area shows **Accept**, **Decline**, and **Simulate no answer**.
3. **Accept** moves to `human_active`, closes Gemini, stops browser microphone/playback and leaves the WebSocket open so you can click **End call**. No human-to-human audio path exists yet.
4. **Decline** and **Simulate no answer** return to `ai_active` and prompt Gemini to offer to take a message.
5. **End call** moves to `completed`, acknowledges the state and releases the streams.

Control messages are JSON text frames (`accept`, `decline`, `no_answer`, `end_call`) on `/ws/live`, alongside the existing PCM16 binary frames. The server responds with `call_state` or `call_error` events. The backend owns valid transitions; the UI only changes state after a server acknowledgement.

`internal/call.HandoffProvider` is an interchangeable interface; `MockHandoff` is the only implementation for this POC. **No real transfer or telephony availability check is performed.** The no-answer button is a manual simulation, not an automatic timeout. The existing CI-07 detector allows one handoff event per Gemini session; repeated handoffs after declining are not yet supported.

Run `go test ./...` and `node --test tests/*.test.mjs` to verify the lifecycle and WebSocket behavior without an API key.

## CI-15: automatic call completion

Gemini Live has a `request_call_end` function. When the caller clearly finishes
the conversation, Callio should say goodbye once and request completion.
The backend sends `call_finishing` after the final assistant turn (or a bounded
2-second fallback); the frontend drains its audio queue and responds with
`{"type":"playback_finished"}`. Go then confirms `call_state: completed`,
closes Gemini and the WebSocket, and the UI displays a disconnected call with
**Start new call** enabled. A new call creates a new provider, WebSocket, audio
player and transcript rather than reopening the completed lifecycle.

If neither side produces conversational events for an idle period, the backend
requests automatic completion. The default is **60 seconds**; configure
`CALL_IDLE_TIMEOUT_SECONDS` to an integer from 10 to 3600. Silent PCM upload
frames do **not** reset the timer. Timeout is suspended during simulated
human takeover and pending handoffs.

This is browser-simulated call completion, not a real phone-network hangup.
The model's ability to choose `request_call_end` depends on live behavior.
Review the manual goodbye, silence, interruption, handoff and repeated-call
scenarios before merging.

Tests: `go test ./...` and `node --test tests/*.test.mjs`.

## CI-12 — Multilingual receptionist

Model-facing instructions and tool descriptions are in English. Set `CALL_GREETING_LOCALE` in `.env` to `fr-FR`, `en-US`, or `ko-KR`; invalid values fail startup. If unset, the default is `fr-FR` for compatibility. The supplied `.env.example` uses `ko-KR` for Korea-based testing.

The selected locale changes **only the initial spoken greeting**. Afterwards Callio is instructed to match the caller's actual spoken language, including switching Korean → English → French without reconnecting or translating unsolicited. Restart the Go process and start a new call after changing `.env`. Prompts are provider-neutral and defined in `internal/realtime/prompt.go`. No frontend selector or language-detection service is introduced.

Manual test: start with `ko-KR` and listen for the Korean greeting; reply in English, then French, and verify that each response follows your language. Also test `request_handoff`, `request_call_end` and Echo. Live language switching is model-dependent and requires manual validation.

## CI-09 — Post-call summary

After an AI call completes, Callio closes the live voice session and requests a separate short **French** summary from `GEMINI_SUMMARY_MODEL`. The summary uses finalized caller/assistant transcript segments plus the requested handoff and end reason. It contains the caller name and call reason when known, urgency (`unknown`, `low`, `normal`, `high`), follow-up actions, whether a handoff was requested, and a factual French paragraph. Unknown details remain unknown.

The server sends `call_state: completed` followed by either `call_summary` or `summary_error` over the existing WebSocket; the browser waits for this final event before disconnecting and displays **Call Summary** below the transcript. The summary request uses the low-latency Gemini 3.5 Flash-Lite model by default, retries one transient upstream 5xx error with a short backoff, and has a **25-second timeout**; failure does not roll back completion. When no transcript and no handoff exist, a factual empty-data summary is returned without a model request. The result is **in-memory only**; CI-10 will implement persistence.

This text request uses the same server-side `GEMINI_API_KEY` but a separate model and may incur additional API usage. The live model name should not be used for text-only `GEMINI_SUMMARY_MODEL`. No call data is stored on disk. Transcripts sent to the configured text model may contain personal data; review consent and retention requirements before deploying outside this local POC.

Testing: `go test ./...`, `go test -race ./...`, and a manual call with French/English/Korean speech. Verify summary content, handoff state, manual and automatic hangup, the “Generating summary…” UI state, and that an unavailable text model displays “Summary unavailable” without trapping the call.


## CI-14 — Microphone silence filtering

In Gemini Live mode, a small browser-side `VoiceActivityGate` uses RMS
hysteresis, a 200 ms pre-roll and 700 ms trailing silence before sending
PCM16 over WebSocket. The browser still monitors microphone frames locally
during idle silence, but it does not upload continuous silent audio.
After each burst it sends `{"type":"audio_stream_end"}`; the Go bridge
forwards this to Gemini using `EndAudioStream` without closing the session.
The next speech burst reopens the audio stream automatically.

The Advanced panel separates microphone frames **processed**, **uploaded**,
**filtered** and **dropped** because of WebSocket backpressure. Some processed
frames may still be held in the bounded pre-roll buffer; these counters do
not necessarily sum at every instant. Echo mode bypasses the gate.

This RMS-only gate is a baseline: soft speech and noisy environments may
require threshold tuning. Manually check quiet speech, pauses, barge-in,
transcript completion, simulated handoff, auto-hangup and Stop/restart
against Gemini before merging. Lower WebSocket traffic is expected but
reduced Gemini billing is not guaranteed.

Run `node --test tests/*.test.cjs tests/*.test.mjs`, `go test ./...`,
`go test -race ./...`, and `go vet ./...`.
