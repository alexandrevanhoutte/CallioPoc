package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"

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
		conn.SetReadLimit(audioChunkBytes)

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

		results := make(chan error, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			results <- forwardMicrophone(ctx, conn, session)
		}()
		go func() {
			defer wg.Done()
			results <- forwardModel(ctx, conn, session)
		}()

		first := <-results
		cancel()
		_ = session.Close()
		conn.CloseNow()
		wg.Wait()

		if first != nil && !errors.Is(first, context.Canceled) && appCtx.Err() == nil {
			slog.Debug("live audio stream ended", "error", first)
		}
	}
}

func forwardMicrophone(ctx context.Context, conn *websocket.Conn, session realtime.VoiceSession) error {
	for {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		if kind != websocket.MessageBinary || len(data) != audioChunkBytes {
			return errors.New("invalid browser audio chunk")
		}
		if err := session.SendAudio(ctx, data); err != nil {
			return err
		}
	}
}

func forwardModel(ctx context.Context, conn *websocket.Conn, session realtime.VoiceSession) error {
	var lastTurn, interruptedTurn uint64
	for {
		event, err := session.Receive(ctx)
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
			if err := conn.Write(ctx, websocket.MessageBinary, event.Audio); err != nil {
				return err
			}
		case realtime.EventTranscript, realtime.EventTurnEnd, realtime.EventInterrupted:
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
			if err := conn.Write(ctx, websocket.MessageText, body); err != nil {
				return err
			}
		default:
			return errors.New("unexpected provider event type")
		}
	}
}
