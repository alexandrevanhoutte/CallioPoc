package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"

	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
	"github.com/alexandrevanhoutte/CallioPoc/internal/call"
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
				if event.TurnID > interruptedTurn {
					interruptedTurn = event.TurnID
				}
				slog.Info("assistant turn interrupted", "turn_id", event.TurnID)
			} else if event.Type == realtime.EventTurnEnd {
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
		default:
			return errors.New("unexpected provider event type")
		}
	}
}
