package httpapi

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/alexandrevanhoutte/CallioPoc/internal/call"
)

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
