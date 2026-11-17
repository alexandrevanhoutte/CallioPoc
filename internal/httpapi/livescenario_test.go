package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/alexandrevanhoutte/CallioPoc/internal/call"
	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
	"github.com/coder/websocket"
)

// Scripted provider events exercise the real WebSocket bridge and call lifecycle.
// They do not prove that Gemini correctly interprets speech or chooses its tools.
type liveScenario struct {
	t          *testing.T
	ctx        context.Context
	conn       *websocket.Conn
	voice      *fakeVoiceSession
	summarizer *fakeSummarizer
}

type scenarioMessage struct {
	Type    string `json:"type"`
	State   string `json:"state"`
	Outcome string `json:"outcome"`
	Reason  string `json:"reason"`
	Action  string `json:"action"`
	Error   string `json:"error"`
	ID      string `json:"segment_id"`
	Speaker realtime.Speaker `json:"speaker"`
	Text    string `json:"text"`
	Final   bool `json:"final"`
	Context realtime.HandoffContext `json:"context"`
	Summary call.Summary `json:"summary"`
}

func newLiveScenario(t *testing.T, locale string) *liveScenario {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	provider := &fakeVoiceProvider{}
	summarizer := &fakeSummarizer{}
	server := httptest.NewServer(NewRouter(ctx, fstest.MapFS{}, provider, summarizer, realtime.LocaleFrench))
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/live?locale="+locale, nil)
	if err != nil {
		server.Close()
		cancel()
		t.Fatalf("connect scripted call: %v", err)
	}
	t.Cleanup(func() {
		conn.CloseNow()
		cancel()
		server.Close()
	})
	s := &liveScenario{t: t, ctx: ctx, conn: conn, summarizer: summarizer}

	// A round-trip synchronizes with provider.Open (which follows the upgrade).
	input := bytes.Repeat([]byte{1, 2}, 320)
	s.write(websocket.MessageBinary, input)
	echo := make([]byte, 960)
	copy(echo, input)
	s.expectAudio(echo)
	provider.mu.Lock()
	s.voice = provider.session
	actualLocale := provider.locale
	provider.mu.Unlock()
	if s.voice == nil || actualLocale.String() != locale {
		t.Fatalf("voice not initialized with locale %s: session=%v locale=%s", locale, s.voice, actualLocale)
	}
	return s
}

func (s *liveScenario) write(kind websocket.MessageType, payload []byte) {
	s.t.Helper()
	if err := s.conn.Write(s.ctx, kind, payload); err != nil {
		s.t.Fatalf("write browser message: %v", err)
	}
}

func (s *liveScenario) command(action string) {
	s.t.Helper()
	payload, err := json.Marshal(map[string]string{"type": action})
	if err != nil {
		s.t.Fatal(err)
	}
	s.write(websocket.MessageText, payload)
}

func (s *liveScenario) emit(event realtime.Event) {
	s.t.Helper()
	select {
	case s.voice.events <- event:
	case <-s.ctx.Done():
		s.t.Fatalf("inject %s: %v", event.Type, s.ctx.Err())
	}
}

func (s *liveScenario) read() (websocket.MessageType, []byte) {
	s.t.Helper()
	kind, payload, err := s.conn.Read(s.ctx)
	if err != nil {
		s.t.Fatalf("read browser message: %v", err)
	}
	return kind, payload
}

func (s *liveScenario) expectAudio(expected []byte) {
	s.t.Helper()
	kind, payload := s.read()
	if kind != websocket.MessageBinary || !bytes.Equal(payload, expected) {
		s.t.Fatalf("audio mismatch: kind=%v bytes=%d want=%d", kind, len(payload), len(expected))
	}
}

func (s *liveScenario) expectEvent(expected string) scenarioMessage {
	s.t.Helper()
	kind, payload := s.read()
	if kind != websocket.MessageText {
		s.t.Fatalf("expected %q JSON, got binary (%d bytes)", expected, len(payload))
	}
	var event scenarioMessage
	if err := json.Unmarshal(payload, &event); err != nil {
		s.t.Fatalf("decode %q: %v (%s)", expected, err, payload)
	}
	if event.Type != expected {
		s.t.Fatalf("event type = %q, want %q: %s", event.Type, expected, payload)
	}
	return event
}

func (s *liveScenario) expectState(state, outcome string) {
	s.t.Helper()
	got := s.expectEvent("call_state")
	if got.State != state || got.Outcome != outcome {
		s.t.Fatalf("call state = %q/%q, want %q/%q", got.State, got.Outcome, state, outcome)
	}
}

func transcriptEvent(id string, order uint64, speaker realtime.Speaker, value string, final bool) realtime.Event {
	return realtime.Event{Type: realtime.EventTranscript, Transcript: &realtime.TranscriptSegment{
		ID: id, Order: order, Speaker: speaker, Text: value, Final: final,
	}}
}

func (s *liveScenario) transcript(id string, order uint64, speaker realtime.Speaker, value string, final bool) {
	s.t.Helper()
	s.emit(transcriptEvent(id, order, speaker, value, final))
	got := s.expectEvent("transcript")
	if got.ID != id || got.Speaker != speaker || got.Text != value || got.Final != final {
		s.t.Fatalf("transcript %s = %+v; want speaker=%s text=%q final=%v", id, got, speaker, value, final)
	}
}

func (s *liveScenario) summary() call.SummaryInput {
	s.t.Helper()
	got := s.expectEvent("call_summary")
	if err := got.Summary.Validate(); err != nil {
		s.t.Fatalf("invalid structured summary: %v", err)
	}
	s.summarizer.mu.Lock()
	input := s.summarizer.input
	s.summarizer.mu.Unlock()
	return input
}

func (s *liveScenario) finishManually() call.SummaryInput {
	s.t.Helper()
	s.command("end_call")
	s.expectState("completed", "ended")
	return s.summary()
}

func (s *liveScenario) expectVoiceClosed() {
	s.t.Helper()
	select {
	case <-s.voice.closed:
	case <-s.ctx.Done():
		s.t.Fatalf("voice not closed after teardown: %v", s.ctx.Err())
	}
}

func TestLiveScenarios(t *testing.T) {
	t.Run("French conversation and natural goodbye", func(t *testing.T) {
		s := newLiveScenario(t, "fr-FR")
		s.transcript("caller-1", 1, realtime.SpeakerCaller, "Je souhaite", false)
		s.transcript("caller-1", 1, realtime.SpeakerCaller, "Je souhaite un renseignement.", true)
		s.transcript("ai-1", 2, realtime.SpeakerAI, "Que souhaitez-vous savoir ?", true)
		s.transcript("caller-2", 3, realtime.SpeakerCaller, "Euh…", false)

		lastAudio := bytes.Repeat([]byte{3, 5}, 480)
		s.emit(realtime.Event{Type: realtime.EventCallEndRequested})
		s.emit(realtime.Event{Type: realtime.EventAudio, TurnID: 4, Audio: lastAudio, SampleRate: 24000})
		s.emit(realtime.Event{Type: realtime.EventTurnEnd, TurnID: 4})
		s.expectAudio(lastAudio)
		s.expectEvent("turn_end")
		if finishing := s.expectEvent("call_finishing"); finishing.Reason != "caller_finished" {
			t.Fatalf("farewell reason = %q", finishing.Reason)
		}
		s.command("playback_finished")
		s.expectState("completed", "caller_finished")
		input := s.summary()
		if input.Locale != realtime.LocaleFrench || input.EndReason != call.EndCallerFinished || input.Handoff != nil {
			t.Fatalf("summary metadata: %+v", input)
		}
		if len(input.Transcript) != 2 ||
			input.Transcript[0].Text != "Je souhaite un renseignement." ||
			input.Transcript[1].Text != "Que souhaitez-vous savoir ?" {
			t.Fatalf("summary must contain only finalized segments, ordered: %+v", input.Transcript)
		}
		s.expectVoiceClosed()
	})

	for _, tc := range []struct {
		name, action, state, outcome string
	}{
		{"recruiting handoff accepted", "accept", "human_active", "accepted"},
		{"recruiting handoff declined", "decline", "ai_active", "decline"},
		{"recruiting handoff no answer", "no_answer", "ai_active", "no_answer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newLiveScenario(t, "fr-FR")
			s.transcript("caller-1", 1, realtime.SpeakerCaller,
				"Bonjour, je suis Nora, pour un entretien Backend Engineer.", true)
			name := "Nora"
			s.emit(realtime.Event{Type: realtime.EventHandoffRequired, Handoff: &realtime.HandoffRequest{
				Reason: realtime.HandoffOwnerRequested,
				Context: realtime.HandoffContext{
					CallerName: &name, Purpose: "Entretien Backend Engineer",
					Summary: "Nora demande à parler au responsable.",
				},
			}})
			handoff := s.expectEvent("handoff_required")
			if handoff.Reason != "owner_requested" || handoff.Context.CallerName == nil ||
				*handoff.Context.CallerName != name || handoff.Context.Purpose != "Entretien Backend Engineer" {
				t.Fatalf("handoff context: %+v", handoff)
			}
			s.expectState("handoff_required", "requested")

			// Caller transcript is a strict ordering barrier. AI audio, AI
			// transcript and turn_end must not escape while owner decides.
			s.emit(realtime.Event{Type: realtime.EventAudio, TurnID: 2, Audio: make([]byte, 960), SampleRate: 24000})
			s.emit(transcriptEvent("ai-muted", 2, realtime.SpeakerAI, "Votre demande a été notée.", true))
			s.emit(realtime.Event{Type: realtime.EventTurnEnd, TurnID: 2})
			s.transcript("caller-2", 3, realtime.SpeakerCaller, "Allô ?", true)

			s.command(tc.action)
			s.expectState(tc.state, tc.outcome)
			if tc.action == "accept" {
				s.expectVoiceClosed()
			} else {
				resumed := bytes.Repeat([]byte{7, 2}, 480)
				s.emit(realtime.Event{Type: realtime.EventAudio, TurnID: 3, Audio: resumed, SampleRate: 24000})
				s.expectAudio(resumed)
				s.transcript("ai-resumed", 4, realtime.SpeakerAI, "Je peux prendre un message.", true)
			}
			input := s.finishManually()
			if input.Handoff == nil || input.Handoff.Context.CallerName == nil ||
				*input.Handoff.Context.CallerName != name || input.EndReason != call.EndManual {
				t.Fatalf("missing handoff in summary: %+v", input)
			}
			for _, segment := range input.Transcript {
				if segment.ID == "ai-muted" {
					t.Fatalf("suppressed handoff speech leaked to summary: %+v", input.Transcript)
				}
			}
			s.expectVoiceClosed()
		})
	}

	t.Run("ambiguous request leaves call active", func(t *testing.T) {
		s := newLiveScenario(t, "fr-FR")
		s.transcript("caller-1", 1, realtime.SpeakerCaller, "Je voulais parler de ça.", true)
		// This tests bridge behavior, not whether Gemini chooses to clarify.
		s.transcript("ai-1", 2, realtime.SpeakerAI, "Pouvez-vous préciser votre demande ?", true)
		s.command("accept")
		rejected := s.expectEvent("call_error")
		if rejected.Action != "accept" || rejected.Error == "" {
			t.Fatalf("premature takeover was not rejected: %+v", rejected)
		}
		s.transcript("caller-2", 3, realtime.SpeakerCaller, "C'est un rendez-vous.", true)
		input := s.finishManually()
		if len(input.Transcript) != 3 || input.Handoff != nil {
			t.Fatalf("ambiguous request must stay active without handoff: %+v", input)
		}
		s.expectVoiceClosed()
	})

	t.Run("interruption drops stale audio and preserves next turn", func(t *testing.T) {
		s := newLiveScenario(t, "en-US")
		s.emit(realtime.Event{Type: realtime.EventInterrupted, TurnID: 4})
		s.expectEvent("interrupted")
		s.emit(realtime.Event{Type: realtime.EventAudio, TurnID: 4, Audio: make([]byte, 960), SampleRate: 24000})
		s.emit(realtime.Event{Type: realtime.EventTurnEnd, TurnID: 4})
		s.transcript("caller-1", 1, realtime.SpeakerCaller, "Actually, I have another question.", true)
		audio := bytes.Repeat([]byte{5, 1}, 480)
		s.emit(realtime.Event{Type: realtime.EventAudio, TurnID: 5, Audio: audio, SampleRate: 24000})
		s.expectAudio(audio)
		s.emit(realtime.Event{Type: realtime.EventTurnEnd, TurnID: 5})
		s.expectEvent("turn_end")
		input := s.finishManually()
		if input.Locale != realtime.LocaleEnglish || len(input.Transcript) != 1 ||
			input.Transcript[0].Text != "Actually, I have another question." {
			t.Fatalf("interrupted transcript/locale: %+v", input)
		}
		s.expectVoiceClosed()
	})

	t.Run("browser disconnect closes provider", func(t *testing.T) {
		s := newLiveScenario(t, "ko-KR")
		s.conn.CloseNow()
		s.expectVoiceClosed()
	})
}
