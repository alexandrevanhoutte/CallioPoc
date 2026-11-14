package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/alexandrevanhoutte/CallioPoc/internal/call"
	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
	"github.com/coder/websocket"
)

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
