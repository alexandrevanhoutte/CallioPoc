package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alexandrevanhoutte/CallioPoc/internal/call"
	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
	"github.com/coder/websocket"
)

// The live protocol keeps 16 kHz mono PCM16 upload chunks (640 bytes).
// Binary replies are mono PCM16 24 kHz; JSON text messages carry other events.
func audioLive(appCtx context.Context, provider realtime.VoiceProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if provider == nil {
			http.Error(w, "Live provider is not configured", http.StatusServiceUnavailable)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		conn.SetReadLimit(maxLiveMessageBytes)

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		stopShutdown := context.AfterFunc(appCtx, cancel)
		defer stopShutdown()

		session, err := provider.Open(ctx)
		if err != nil {
			_ = conn.Close(websocket.StatusInternalError, "AI session could not start")
			slog.Warn("live session opening failed", "error", err)
			return
		}
		defer session.Close()
		slog.Info("live session started")
		defer slog.Info("live session closed")

		// Both directions run independently. On disconnect/error, cancellation
		// closes the provider and WebSocket, so no goroutine is left blocked.
		stopSession := context.AfterFunc(ctx, func() {
			_ = session.Close()
			conn.CloseNow()
		})
		defer stopSession()

		bridge := &liveBridge{
			conn: conn,
			session: session,
			lifecycle: call.NewLifecycle(call.MockHandoff{}),
		}
		bridge.touch()
		go bridge.watchInactivity(ctx, configuredIdleTimeout())
		type streamResult struct {
			source string
			err error
		}
		results := make(chan streamResult, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			results <- streamResult{source: "input", err: bridge.readInput(ctx)}
		}()
		go func() {
			defer wg.Done()
			results <- streamResult{source: "model", err: forwardModel(ctx, bridge)}
		}()

		// On takeover the model is deliberately closed but the browser stays
		// connected until the owner explicitly ends the simulated call.
		for {
			result := <-results
			if result.source == "model" && bridge.lifecycle.State() == call.HumanActive {
				continue
			}
			if result.err != nil && !errors.Is(result.err, context.Canceled) && appCtx.Err() == nil {
				slog.Debug("live audio stream ended", "source", result.source, "error", result.err)
			}
			break
		}
		cancel()
		_ = session.Close()
		conn.CloseNow()
		wg.Wait()
	}
}

func forwardModel(ctx context.Context, bridge *liveBridge) error {
	var lastTurn, interruptedTurn uint64
	for {
		event, err := bridge.session.Receive(ctx)
		if err != nil {
			return err
		}
		switch event.Type {
		case realtime.EventAudio:
			bridge.aiSpeaking.Store(true)
			// A provider may deliver buffered audio after announcing cancellation.
			// Never forward explicitly stale generations to the browser.
			if event.TurnID != 0 && event.TurnID <= interruptedTurn {
				continue
			}
			if event.TurnID != 0 && event.TurnID != lastTurn {
				lastTurn = event.TurnID
				slog.Info("assistant turn started", "turn_id", lastTurn)
			}
			if event.SampleRate != 24000 || len(event.Audio) == 0 || len(event.Audio)%2 != 0 || len(event.Audio) > 64*1024 {
				return errors.New("invalid provider output audio")
			}
			if err := bridge.writeAudio(ctx, event.Audio); err != nil {
				return err
			}
		case realtime.EventTranscript:
			bridge.touch()
			if event.Transcript == nil {
				continue
			}
			body, err := json.Marshal(struct {
				Type string `json:"type"`
				*realtime.TranscriptSegment
			}{
				Type: event.Type.String(),
				TranscriptSegment: event.Transcript,
			})
			if err != nil {
				return err
			}
			if err := bridge.writeText(ctx, body); err != nil {
				return err
			}
		case realtime.EventCallEndRequested:
			bridge.beginFinishing(ctx, call.EndCallerFinished)
		case realtime.EventHandoffRequired:
			if event.Handoff == nil {
				return errors.New("missing handoff request")
			}
			if err := event.Handoff.Validate(); err != nil {
				return err
			}
			if err := bridge.lifecycle.RequestHandoff(); err != nil {
				if errors.Is(err, call.ErrInvalidTransition) { continue }
				return err
			}
			body, err := json.Marshal(struct {
				Type    string                  `json:"type"`
				Reason  string                  `json:"reason"`
				Context realtime.HandoffContext `json:"context"`
			}{
				Type: event.Type.String(), Reason: event.Handoff.Reason.String(),
				Context: event.Handoff.Context,
			})
			if err != nil {
				return err
			}
			if err := bridge.writeText(ctx, body); err != nil {
				return err
			}
			slog.Info("handoff_required sent to browser", "reason", event.Handoff.Reason.String())
			if err := bridge.sendState(ctx, "requested"); err != nil { return err }
		case realtime.EventTurnEnd, realtime.EventInterrupted:
			if event.Type == realtime.EventInterrupted {
				bridge.cancelFinishing(ctx)
				bridge.aiSpeaking.Store(false)
				if event.TurnID > interruptedTurn {
					interruptedTurn = event.TurnID
				}
				slog.Info("assistant turn interrupted", "turn_id", event.TurnID)
			} else if event.Type == realtime.EventTurnEnd {
				bridge.aiSpeaking.Store(false)
				bridge.touch()
				if event.TurnID != 0 && event.TurnID <= interruptedTurn {
					continue
				}
				slog.Info("assistant turn completed", "turn_id", event.TurnID)
			}
			body, err := json.Marshal(map[string]string{
				"type": event.Type.String(),
				"text": event.Text,
			})
			if err != nil {
				return err
			}
			if err := bridge.writeText(ctx, body); err != nil {
				return err
			}
			if event.Type == realtime.EventTurnEnd && bridge.finishing.Load() {
				if err := bridge.sendFinishing(ctx); err != nil { return err }
			}
		default:
			return errors.New("unexpected provider event type")
		}
	}
}


const maxLiveMessageBytes = 4096

// liveBridge serializes writes from the model and control handlers.
// Its lifecycle belongs to one browser WebSocket connection.
type liveBridge struct {
	conn *websocket.Conn
	session realtime.VoiceSession
	lifecycle *call.Lifecycle
	writes sync.Mutex
	finishing atomic.Bool
	finishSent atomic.Bool
	aiSpeaking atomic.Bool
	lastActivity atomic.Int64
	finishReason call.EndReason
	finishMu sync.Mutex
	finishGeneration atomic.Uint64
}

func (b *liveBridge) writeText(ctx context.Context, data []byte) error {
	b.writes.Lock()
	defer b.writes.Unlock()
	return b.conn.Write(ctx, websocket.MessageText, data)
}

func (b *liveBridge) writeJSON(ctx context.Context, event any) error {
	data, err := json.Marshal(event)
	if err != nil { return err }
	return b.writeText(ctx, data)
}

func (b *liveBridge) writeAudio(ctx context.Context, audio []byte) error {
	b.writes.Lock()
	defer b.writes.Unlock()
	if b.lifecycle.State() == call.HumanActive || b.lifecycle.State() == call.Completed {
		return nil
	}
	return b.conn.Write(ctx, websocket.MessageBinary, audio)
}

func (b *liveBridge) sendState(ctx context.Context, outcome string) error {
	state := b.lifecycle.State()
	event := struct {
		Type string `json:"type"`
		State string `json:"state"`
		Outcome string `json:"outcome,omitempty"`
	}{Type: "call_state", State: state.String(), Outcome: outcome}
	return b.writeJSON(ctx, event)
}

type controlMessage struct {
	Type string `json:"type"`
}

func (b *liveBridge) readInput(ctx context.Context) error {
	for {
		kind, data, err := b.conn.Read(ctx)
		if err != nil { return err }
		if kind == websocket.MessageBinary {
			if len(data) != audioChunkBytes { return errors.New("invalid browser audio chunk") }
			if b.finishSent.Load() || (b.lifecycle.State() != call.AIActive && b.lifecycle.State() != call.HandoffRequired) {
				continue // Ignore buffered microphone chunks after takeover.
			}
			if err := b.session.SendAudio(ctx, data); err != nil {
				if b.lifecycle.State() == call.HumanActive || b.lifecycle.State() == call.Completed { continue }
				return err
			}
			continue
		}
		if kind != websocket.MessageText || len(data) > maxLiveMessageBytes {
			return errors.New("invalid live control message")
		}
		var message controlMessage
		if err := json.Unmarshal(data, &message); err != nil {
			return fmt.Errorf("decode control message: %w", err)
		}
		switch message.Type {
		case "playback_finished":
			if !b.finishSent.Load() || b.lifecycle.State() != call.AIActive {
				if err := b.sendControlError(ctx, message.Type, errors.New("call is not finishing")); err != nil { return err }
				continue
			}
			if err := b.lifecycle.Complete(); err != nil { return err }
			slog.Info("call completed after browser playback", "reason", b.finishReason.String())
			if err := b.sendState(ctx, b.finishReason.String()); err != nil { return err }
			return nil
		case "accept":
			if err := b.lifecycle.Accept(ctx); err != nil {
				if err := b.sendControlError(ctx, message.Type, err); err != nil { return err }
				continue
			}
			// The provider has accepted. Close Gemini, but keep the browser
			// socket for the final end_call action.
			slog.Info("simulated human takeover accepted")
			_ = b.session.Close()
			if err := b.sendState(ctx, "accepted"); err != nil { return err }
		case "decline", "no_answer":
			var transitionErr error
			if message.Type == "decline" {
				transitionErr = b.lifecycle.Decline(ctx)
			} else {
				transitionErr = b.lifecycle.NoAnswer(ctx)
			}
			if transitionErr != nil {
				if err := b.sendControlError(ctx, message.Type, transitionErr); err != nil { return err }
				continue
			}
			if err := b.sendState(ctx, message.Type); err != nil { return err }
			b.touch()
			slog.Info("simulated handoff unresolved", "outcome", message.Type)
			// Instruct the AI to offer to take a message, not to claim that
			// an actual telephone transfer took place.
			if notifier, ok := b.session.(realtime.HandoffOutcomeNotifier); ok {
				if err := notifier.NotifyHandoffOutcome(ctx, message.Type); err != nil {
					slog.Warn("failed to notify voice provider of handoff outcome", "error", err)
				}
			}
		case "end_call":
			if err := b.lifecycle.Complete(); err != nil {
				if err := b.sendControlError(ctx, message.Type, err); err != nil { return err }
				continue
			}
			slog.Info("simulated call completed")
			if err := b.sendState(ctx, "ended"); err != nil { return err }
			return nil
		default:
			if err := b.sendControlError(ctx, message.Type, errors.New("unknown control command")); err != nil { return err }
		}
	}
}

func (b *liveBridge) sendControlError(ctx context.Context, action string, cause error) error {
	slog.Debug("live control command rejected", "action", action, "error", cause)
	return b.writeJSON(ctx, struct {
		Type string `json:"type"`
		Action string `json:"action"`
		Error string `json:"error"`
	}{Type: "call_error", Action: action, Error: cause.Error()})
}


// The model may request closure before Gemini has delivered its last audio.
// Prefer turn_end; the bounded fallback avoids leaving calls open forever.
const farewellGrace = 2 * time.Second
const defaultIdleTimeout = 60 * time.Second

func configuredIdleTimeout() time.Duration {
	value := os.Getenv("CALL_IDLE_TIMEOUT_SECONDS")
	if value == "" { return defaultIdleTimeout }
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 10 || seconds > 3600 {
		slog.Warn("invalid CALL_IDLE_TIMEOUT_SECONDS; using default", "value", value)
		return defaultIdleTimeout
	}
	return time.Duration(seconds) * time.Second
}

func (b *liveBridge) touch() {
	b.lastActivity.Store(time.Now().UnixNano())
}

func (b *liveBridge) beginFinishing(ctx context.Context, reason call.EndReason) {
	b.finishMu.Lock()
	if b.lifecycle.State() != call.AIActive || b.finishing.Load() {
		b.finishMu.Unlock()
		return
	}
	b.finishReason = reason
	b.finishing.Store(true)
	generation := b.finishGeneration.Add(1)
	b.finishMu.Unlock()
	slog.Info("call completion initiated", "reason", reason.String())
	if reason == call.EndIdleTimeout {
		_ = b.sendFinishing(ctx)
		return
	}
	go func() {
		timer := time.NewTimer(farewellGrace)
		defer timer.Stop()
		select {
		case <-timer.C:
			if b.finishGeneration.Load() != generation { return }
			if err := b.sendFinishing(ctx); err != nil && ctx.Err() == nil {
				slog.Debug("could not send call_finishing", "error", err)
			}
		case <-ctx.Done():
		}
	}()
}

// If the caller interrupts the farewell, abort pending natural completion.
func (b *liveBridge) cancelFinishing(ctx context.Context) {
	b.finishMu.Lock()
	if !b.finishing.Load() || b.finishSent.Load() {
		b.finishMu.Unlock()
		return
	}
	b.finishing.Store(false)
	b.finishReason = call.EndUnknown
	b.finishGeneration.Add(1)
	b.finishMu.Unlock()
	slog.Info("natural call completion cancelled by interruption")
	if err := b.writeJSON(ctx, struct {
		Type string `json:"type"`
	}{Type: "call_end_cancelled"}); err != nil && ctx.Err() == nil {
		slog.Debug("failed to send call_end_cancelled", "error", err)
	}
}

func (b *liveBridge) sendFinishing(ctx context.Context) error {
	b.finishMu.Lock()
	if b.lifecycle.State() != call.AIActive || !b.finishing.Load() || b.finishSent.Load() {
		b.finishMu.Unlock()
		return nil
	}
	b.finishSent.Store(true)
	reason := b.finishReason
	b.finishMu.Unlock()
	return b.writeJSON(ctx, struct {
		Type string `json:"type"`
		Reason string `json:"reason"`
	}{Type: "call_finishing", Reason: reason.String()})
}

// Watch real conversation events, not the stream of silent PCM frames.
func (b *liveBridge) watchInactivity(ctx context.Context, timeout time.Duration) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done(): return
		case <-ticker.C:
			if b.lifecycle.State() != call.AIActive || b.finishing.Load() || b.aiSpeaking.Load() { continue }
			last := time.Unix(0, b.lastActivity.Load())
			if idleExpired(time.Now(), last, timeout, b.lifecycle.State(), b.aiSpeaking.Load()) {
				b.beginFinishing(ctx, call.EndIdleTimeout)
				return
			}
		}
	}
}

func idleExpired(now, last time.Time, timeout time.Duration, state call.State, speaking bool) bool {
	return state == call.AIActive && !speaking && now.Sub(last) >= timeout
}
