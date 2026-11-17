package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/alexandrevanhoutte/CallioPoc/internal/call"
	"github.com/alexandrevanhoutte/CallioPoc/internal/logging"
	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
	"github.com/coder/websocket"
)

const (
	maxLiveMessageBytes = 4096
	farewellGrace       = 2 * time.Second
	defaultIdleTimeout  = 60 * time.Second
	summaryTimeout      = 25 * time.Second
)

// endPhase only tracks the audio shutdown handshake; CallLifecycle remains the
// source of truth for the actual call state.
type endPhase uint8

const (
	endActive endPhase = iota
	endFarewell
	endAwaitingPlayback
)

// One liveSession owns one browser connection and one provider session.
// writes serializes WebSocket output; mu protects the farewell and idle state.
type liveSession struct {
	conn       *websocket.Conn
	voice      realtime.VoiceSession
	lifecycle  *call.Lifecycle
	locale     realtime.Locale
	summarizer call.Summarizer
	l          logging.Logger
	transcript realtime.Transcript
	handoff    *realtime.HandoffRequest

	writes sync.Mutex
	mu     sync.Mutex

	phase        endPhase
	endReason    call.EndReason
	endVersion   uint64
	farewell     *time.Timer
	lastActivity time.Time
	aiSpeaking   bool
	streamOpen   bool // readBrowser only; suppress duplicate stream-end signals.
}

// The live protocol uploads 16 kHz mono PCM16 (640 bytes) and receives
// 24 kHz PCM16 audio plus JSON control/transcript messages.
func audioLive(appCtx context.Context, provider realtime.VoiceProvider, summarizer call.Summarizer, defaultLocale realtime.Locale) http.HandlerFunc {
	logger := logging.For("httpapi.live")
	return func(w http.ResponseWriter, r *http.Request) {
		if provider == nil {
			http.Error(w, "Live provider is not configured", http.StatusServiceUnavailable)
			return
		}
		locale := defaultLocale
		if values, specified := r.URL.Query()["locale"]; specified {
			if len(values) != 1 {
				http.Error(w, "exactly one locale is required", http.StatusBadRequest)
				return
			}
			var err error
			locale, err = realtime.NewLocale(values[0])
			if err != nil {
				http.Error(w, "unsupported locale", http.StatusBadRequest)
				return
			}
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

		voice, err := provider.Open(ctx, locale)
		if err != nil {
			_ = conn.Close(websocket.StatusInternalError, "AI session could not start")
			logger.Warn("live session opening failed", "error", err)
			return
		}
		defer voice.Close()
		logger.Info("live session started")
		defer logger.Info("live session closed")

		// Cancellation must unblock both WebSocket reads and provider Receive.
		stopSession := context.AfterFunc(ctx, func() {
			_ = voice.Close()
			conn.CloseNow()
		})
		defer stopSession()

		session := &liveSession{
			conn:         conn,
			voice:        voice,
			lifecycle:    call.NewLifecycle(call.MockHandoff{}),
			locale:       locale,
			summarizer:   summarizer,
			l:            logger,
			lastActivity: time.Now(),
		}
		go session.watchInactivity(ctx, configuredIdleTimeout())

		type streamResult struct {
			source string
			err    error
		}
		results := make(chan streamResult, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			results <- streamResult{source: "browser", err: session.readBrowser(ctx)}
		}()
		go func() {
			defer wg.Done()
			results <- streamResult{source: "model", err: session.readModel(ctx)}
		}()

		// The model stops on human takeover, but the browser remains connected
		// until the simulated human ends the call.
		for {
			result := <-results
			if result.source == "model" && (session.lifecycle.State() == call.HumanActive || session.lifecycle.State() == call.Completed) {
				continue
			}
			if result.err != nil && !errors.Is(result.err, context.Canceled) && appCtx.Err() == nil {
				logger.Debug("live stream ended", "source", result.source, "error", result.err)
			}
			break
		}
		cancel()
		_ = voice.Close()
		conn.CloseNow()
		wg.Wait()
		session.mu.Lock()
		if session.farewell != nil {
			session.farewell.Stop()
		}
		session.mu.Unlock()
	}
}

// send serializes text frames from both the model and browser control paths.
func (s *liveSession) send(ctx context.Context, event any) error {
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	s.writes.Lock()
	defer s.writes.Unlock()
	return s.conn.Write(ctx, websocket.MessageText, body)
}

func (s *liveSession) sendState(ctx context.Context, outcome string) error {
	return s.send(ctx, struct {
		Type    string `json:"type"`
		State   string `json:"state"`
		Outcome string `json:"outcome,omitempty"`
	}{
		Type: "call_state", State: s.lifecycle.State().String(), Outcome: outcome,
	})
}

func (s *liveSession) readBrowser(ctx context.Context) error {
	for {
		kind, data, err := s.conn.Read(ctx)
		if err != nil {
			return err
		}
		if kind == websocket.MessageBinary {
			if len(data) != audioChunkBytes {
				return errors.New("invalid browser audio chunk")
			}
			s.mu.Lock()
			finishing := s.phase == endAwaitingPlayback
			s.mu.Unlock()
			state := s.lifecycle.State()
			if finishing || (state != call.AIActive && state != call.HandoffRequired) {
				continue
			}
			if err := s.voice.SendAudio(ctx, data); err != nil {
				if s.lifecycle.State() == call.HumanActive || s.lifecycle.State() == call.Completed {
					continue
				}
				return err
			}
			s.streamOpen = true
			continue
		}
		if kind != websocket.MessageText || len(data) > maxLiveMessageBytes {
			return errors.New("invalid live control message")
		}
		var message struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &message); err != nil {
			return fmt.Errorf("decode control message: %w", err)
		}

		var actionErr error
		switch message.Type {
		case "audio_stream_end":
			s.mu.Lock()
			finishing := s.phase == endAwaitingPlayback
			s.mu.Unlock()
			state := s.lifecycle.State()
			if finishing || (state != call.AIActive && state != call.HandoffRequired) {
				actionErr = errors.New("audio stream end is not allowed in this call state")
				break
			}
			if s.streamOpen {
				if err := s.voice.EndAudioStream(ctx); err != nil {
					return fmt.Errorf("end provider audio stream: %w", err)
				}
				s.streamOpen = false
				s.touch() // Real speech ended; silent PCM frames never reset inactivity.
			}
		case "playback_finished":
			s.mu.Lock()
			ready := s.phase == endAwaitingPlayback
			reason := s.endReason
			s.mu.Unlock()
			if !ready || s.lifecycle.State() != call.AIActive {
				actionErr = errors.New("call is not finishing")
				break
			}
			if err := s.lifecycle.Complete(); err != nil {
				return err
			}
			s.l.Info("call completed after browser playback", "reason", reason.String())
			if err := s.sendState(ctx, reason.String()); err != nil {
				return err
			}
			return s.sendSummary(ctx, reason)
		case "accept":
			actionErr = s.lifecycle.Accept(ctx)
			if actionErr == nil {
				s.l.Info("simulated human takeover accepted")
				_ = s.voice.Close()
				actionErr = s.sendState(ctx, "accepted")
			}
		case "decline", "no_answer":
			if message.Type == "decline" {
				actionErr = s.lifecycle.Decline(ctx)
			} else {
				actionErr = s.lifecycle.NoAnswer(ctx)
			}
			if actionErr == nil {
				s.touch()
				if err := s.sendState(ctx, message.Type); err != nil {
					return err
				}
				s.l.Info("simulated handoff unresolved", "outcome", message.Type)
				if notifier, ok := s.voice.(realtime.HandoffOutcomeNotifier); ok {
					if err := notifier.NotifyHandoffOutcome(ctx, message.Type); err != nil {
						s.l.Warn("failed to notify voice provider of handoff outcome", "error", err)
					}
				}
			}
		case "end_call":
			actionErr = s.lifecycle.Complete()
			if actionErr == nil {
				s.l.Info("simulated call completed")
				if err := s.sendState(ctx, "ended"); err != nil {
					return err
				}
				return s.sendSummary(ctx, call.EndManual)
			}
		default:
			actionErr = errors.New("unknown control command")
		}
		if actionErr != nil {
			s.l.Debug("live control command rejected", "action", message.Type, "error", actionErr)
			if err := s.send(ctx, struct {
				Type   string `json:"type"`
				Action string `json:"action"`
				Error  string `json:"error"`
			}{
				Type: "call_error", Action: message.Type, Error: actionErr.Error(),
			}); err != nil {
				return err
			}
		}
	}
}

func (s *liveSession) readModel(ctx context.Context) error {
	var lastTurn, interruptedTurn uint64
	for {
		event, err := s.voice.Receive(ctx)
		if err != nil {
			return err
		}
		switch event.Type {
		case realtime.EventAudio:
			if s.lifecycle.State() == call.HandoffRequired {
				continue
			}
			// Discard buffered generations after an interruption.
			if event.TurnID != 0 && event.TurnID <= interruptedTurn {
				continue
			}
			if event.SampleRate != 24000 || len(event.Audio) == 0 ||
				len(event.Audio)%2 != 0 || len(event.Audio) > 64*1024 {
				return errors.New("invalid provider output audio")
			}
			if event.TurnID != 0 && event.TurnID != lastTurn {
				lastTurn = event.TurnID
				s.l.Info("assistant turn started", "turn_id", lastTurn)
			}
			s.mu.Lock()
			s.aiSpeaking = true
			s.mu.Unlock()
			s.writes.Lock()
			s.mu.Lock()
			ready := s.phase != endAwaitingPlayback
			s.mu.Unlock()
			state := s.lifecycle.State()
			if ready && state != call.HandoffRequired && state != call.HumanActive && state != call.Completed {
				err = s.conn.Write(ctx, websocket.MessageBinary, event.Audio)
			}
			s.writes.Unlock()
			if err != nil {
				return err
			}
		case realtime.EventTranscript:
			if event.Transcript == nil {
				continue
			}
			// Ignore speculative Gemini speech during an owner decision.
			// It must not be shown or saved for summary.
			if event.Transcript.Speaker == realtime.SpeakerAI && s.lifecycle.State() == call.HandoffRequired {
				continue
			}
			s.touch()
			s.mu.Lock()
			s.transcript.Apply(*event.Transcript)
			s.mu.Unlock()
			if err := s.send(ctx, struct {
				Type string `json:"type"`
				*realtime.TranscriptSegment
			}{
				Type: event.Type.String(), TranscriptSegment: event.Transcript,
			}); err != nil {
				return err
			}
		case realtime.EventCallEndRequested:
			if err := s.requestEnd(ctx, call.EndCallerFinished); err != nil {
				return err
			}
		case realtime.EventHandoffRequired:
			s.mu.Lock()
			s.aiSpeaking = false
			s.mu.Unlock()
			if event.Handoff == nil {
				return errors.New("missing handoff request")
			}
			if err := event.Handoff.Validate(); err != nil {
				return err
			}
			if err := s.lifecycle.RequestHandoff(); err != nil {
				if errors.Is(err, call.ErrInvalidTransition) {
					continue
				}
				return err
			}
			if err := s.send(ctx, struct {
				Type    string                  `json:"type"`
				Reason  string                  `json:"reason"`
				Context realtime.HandoffContext `json:"context"`
			}{
				Type: event.Type.String(), Reason: event.Handoff.Reason.String(),
				Context: event.Handoff.Context,
			}); err != nil {
				return err
			}
			s.mu.Lock()
			copyHandoff := *event.Handoff
			s.handoff = &copyHandoff
			s.mu.Unlock()
			s.l.Info("handoff_required sent to browser", "reason", event.Handoff.Reason.String())
			if err := s.sendState(ctx, "requested"); err != nil {
				return err
			}
		case realtime.EventTurnEnd, realtime.EventInterrupted:
			s.mu.Lock()
			s.aiSpeaking = false
			s.lastActivity = time.Now()
			s.mu.Unlock()
			if event.Type == realtime.EventInterrupted {
				if err := s.cancelEnd(ctx); err != nil {
					return err
				}
				if event.TurnID > interruptedTurn {
					interruptedTurn = event.TurnID
				}
				s.l.Info("assistant turn interrupted", "turn_id", event.TurnID)
			} else {
				if event.TurnID != 0 && event.TurnID <= interruptedTurn {
					continue
				}
				s.l.Info("assistant turn completed", "turn_id", event.TurnID)
			}
			if s.lifecycle.State() == call.HandoffRequired {
				continue
			}
			if err := s.send(ctx, struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{Type: event.Type.String(), Text: event.Text}); err != nil {
				return err
			}
			if event.Type == realtime.EventTurnEnd {
				if err := s.finishAudio(ctx, 0); err != nil {
					return err
				}
			}
		default:
			return errors.New("unexpected provider event type")
		}
	}
}

// sendSummary runs after the lifecycle has completed, so a model failure can
// never prevent the call from ending. The browser keeps this socket open until
// it receives the summary or summary_error event.
func (s *liveSession) sendSummary(ctx context.Context, reason call.EndReason) error {
	// Close the expensive live audio session before the independent text request.
	_ = s.voice.Close()
	s.mu.Lock()
	input := call.SummaryInput{
		Transcript: s.transcript.Finalized(),
		Handoff:    s.handoff,
		EndReason:  reason,
		Locale:     s.locale,
	}
	s.mu.Unlock()
	if s.summarizer == nil {
		return s.send(ctx, struct {
			Type  string `json:"type"`
			Error string `json:"error"`
		}{Type: "summary_error", Error: "Summary service is unavailable"})
	}

	summaryCtx, cancel := context.WithTimeout(ctx, summaryTimeout)
	defer cancel()
	summary, err := s.summarizer.Summarize(summaryCtx, input)
	if err == nil {
		err = summary.Validate()
	}
	if err != nil {
		s.l.Warn("call summary unavailable", "error", err)
		return s.send(ctx, struct {
			Type  string `json:"type"`
			Error string `json:"error"`
		}{Type: "summary_error", Error: "Unable to generate a call summary"})
	}
	// The backend owns handoff state; a generated summary cannot overwrite it.
	summary.HandoffRequested = input.Handoff != nil
	return s.send(ctx, struct {
		Type    string       `json:"type"`
		Summary call.Summary `json:"summary"`
	}{Type: "call_summary", Summary: summary})
}

func (s *liveSession) touch() {
	s.mu.Lock()
	s.lastActivity = time.Now()
	s.mu.Unlock()
}

// requestEnd starts the farewell; model output may still arrive until a turn
// ends or the bounded grace timer fires.
func (s *liveSession) requestEnd(ctx context.Context, reason call.EndReason) error {
	s.mu.Lock()
	if s.lifecycle.State() != call.AIActive || s.phase != endActive {
		s.mu.Unlock()
		return nil
	}
	s.phase = endFarewell
	s.endReason = reason
	s.endVersion++
	version := s.endVersion
	if reason == call.EndCallerFinished {
		s.farewell = time.AfterFunc(farewellGrace, func() {
			if err := s.finishAudio(ctx, version); err != nil && ctx.Err() == nil {
				s.l.Debug("failed to send call_finishing", "error", err)
			}
		})
	}
	s.mu.Unlock()
	s.l.Info("call completion initiated", "reason", reason.String())
	if reason == call.EndIdleTimeout {
		return s.finishAudio(ctx, 0)
	}
	return nil
}

// An interrupted closing turn may be followed by a new question; it must not
// permanently consume Gemini's ability to request a later goodbye.
func (s *liveSession) cancelEnd(ctx context.Context) error {
	s.mu.Lock()
	if s.phase != endFarewell {
		s.mu.Unlock()
		return nil
	}
	if s.farewell != nil {
		s.farewell.Stop()
		s.farewell = nil
	}
	s.phase = endActive
	s.endVersion++
	s.endReason = call.EndUnknown
	s.mu.Unlock()
	s.l.Info("natural call completion cancelled by interruption")
	return s.send(ctx, struct {
		Type string `json:"type"`
	}{Type: "call_end_cancelled"})
}

// finishAudio publishes the final boundary only once, so the browser can
// safely drain its queued PCM before acknowledging playback_finished.
func (s *liveSession) finishAudio(ctx context.Context, version uint64) error {
	// Serialize the final boundary with outgoing audio. No binary frame may
	// follow call_finishing, even when a farewell timer fires concurrently.
	s.writes.Lock()
	defer s.writes.Unlock()
	s.mu.Lock()
	if s.phase != endFarewell || (version != 0 && s.endVersion != version) ||
		s.lifecycle.State() != call.AIActive {
		s.mu.Unlock()
		return nil
	}
	s.phase = endAwaitingPlayback
	reason := s.endReason
	if s.farewell != nil {
		s.farewell.Stop()
		s.farewell = nil
	}
	s.mu.Unlock()
	body, err := json.Marshal(struct {
		Type   string `json:"type"`
		Reason string `json:"reason"`
	}{Type: "call_finishing", Reason: reason.String()})
	if err != nil {
		return err
	}
	return s.conn.Write(ctx, websocket.MessageText, body)
}

func (s *liveSession) watchInactivity(ctx context.Context, timeout time.Duration) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			last, speaking, phase := s.lastActivity, s.aiSpeaking, s.phase
			s.mu.Unlock()
			if phase != endActive || !idleExpired(time.Now(), last, timeout, s.lifecycle.State(), speaking) {
				continue
			}
			if err := s.requestEnd(ctx, call.EndIdleTimeout); err != nil && ctx.Err() == nil {
				s.l.Debug("failed to finish idle call", "error", err)
			}
			// Continue monitoring if a caller interruption cancels completion.
		}
	}
}

func configuredIdleTimeout() time.Duration {
	value := os.Getenv("CALL_IDLE_TIMEOUT_SECONDS")
	if value == "" {
		return defaultIdleTimeout
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 10 || seconds > 3600 {
		logging.For("httpapi.live").Warn("invalid CALL_IDLE_TIMEOUT_SECONDS; using default", "value", value)
		return defaultIdleTimeout
	}
	return time.Duration(seconds) * time.Second
}

func idleExpired(now, last time.Time, timeout time.Duration, state call.State, speaking bool) bool {
	return state == call.AIActive && !speaking && now.Sub(last) >= timeout
}
