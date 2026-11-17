package httpapi

import (
	"errors"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/alexandrevanhoutte/CallioPoc/internal/call"
	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
	"github.com/coder/websocket"
)

type fakeVoiceProvider struct {
	mu sync.Mutex
	session *fakeVoiceSession
	locale realtime.Locale
}

func (p *fakeVoiceProvider) Open(_ context.Context, locale realtime.Locale) (realtime.VoiceSession, error) {
	s := &fakeVoiceSession{
		events: make(chan realtime.Event, 8),
		streamEnds: make(chan struct{}, 8),
		closed: make(chan struct{}),
	}
	p.mu.Lock()
	p.session = s
	p.locale = locale
	p.mu.Unlock()
	return s, nil
}

type fakeVoiceSession struct {
	events chan realtime.Event
	streamEnds chan struct{}
	closed chan struct{}
	once sync.Once
}

func (s *fakeVoiceSession) SendAudio(ctx context.Context, audio []byte) error {
	if len(audio) != 640 {
		return context.Canceled
	}
	out := realtime.Event{Type: realtime.EventAudio, Audio: make([]byte, 960), SampleRate: 24000}
	copy(out.Audio, audio)
	select {
	case s.events <- out:
		return nil
	case <-s.closed:
		return context.Canceled
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *fakeVoiceSession) EndAudioStream(ctx context.Context) error {
	select {
	case s.streamEnds <- struct{}{}:
		return nil
	case <-s.closed:
		return context.Canceled
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *fakeVoiceSession) Receive(ctx context.Context) (realtime.Event, error) {
	select {
	case event := <-s.events:
		return event, nil
	case <-s.closed:
		return realtime.Event{}, context.Canceled
	case <-ctx.Done():
		return realtime.Event{}, ctx.Err()
	}
}

func (s *fakeVoiceSession) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

func TestLivePerCallLocaleAndFallback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider := &fakeVoiceProvider{}
	server := httptest.NewServer(NewRouter(ctx, fstest.MapFS{}, provider, nil, realtime.LocaleKorean))
	defer server.Close()
	base := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/live"

	for _, tc := range []struct { query string; want realtime.Locale }{
		{"", realtime.LocaleKorean},
		{"?locale=fr-FR", realtime.LocaleFrench},
		{"?locale=en-US", realtime.LocaleEnglish},
		{"?locale=ko-KR", realtime.LocaleKorean},
	} {
		conn, _, err := websocket.Dial(ctx, base+tc.query, nil)
		if err != nil { t.Fatalf("dial %s: %v", tc.query, err) }
		// The WebSocket handshake precedes provider.Open; an audio round-trip
		// makes the provider initialization observable before reading the locale.
		if err := conn.Write(ctx, websocket.MessageBinary, make([]byte, 640)); err != nil { t.Fatal(err) }
		if _, _, err := conn.Read(ctx); err != nil { t.Fatal(err) }
		provider.mu.Lock()
		got := provider.locale
		provider.mu.Unlock()
		if got != tc.want { t.Errorf("locale %q = %s, want %s", tc.query, got, tc.want) }
		conn.CloseNow()
	}
	for _, query := range []string{"?locale=de-DE", "?locale=", "?locale=fr-FR&locale=ko-KR"} {
		res, err := http.Get(server.URL + "/ws/live" + query)
		if err != nil { t.Fatal(err) }
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("invalid %q returned %d, want 400", query, res.StatusCode)
		}
	}
}

func TestLiveBridge(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider := &fakeVoiceProvider{}
	server := httptest.NewServer(NewRouter(ctx, fstest.MapFS{}, provider, nil, 0))
	defer server.Close()

	uri := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/live"
	conn, _, err := websocket.Dial(ctx, uri, nil)
	if err != nil {
		t.Fatal(err)
	}

	audio := bytes.Repeat([]byte{2, 3}, 320)
	if err := conn.Write(ctx, websocket.MessageBinary, audio); err != nil {
		t.Fatal(err)
	}
	kind, result, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if kind != websocket.MessageBinary || len(result) != 960 || !bytes.Equal(result[:640], audio) {
		t.Fatalf("unexpected live audio response: type=%v bytes=%d", kind, len(result))
	}
	provider.mu.Lock()
	s := provider.session
	provider.mu.Unlock()
	s.events <- realtime.Event{Type: realtime.EventInterrupted}
	kind, result, err = conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if kind != websocket.MessageText || !bytes.Contains(result, []byte(`"type":"interrupted"`)) {
		t.Fatalf("unexpected interruption notification: type=%v body=%s", kind, result)
	}
	conn.CloseNow()
	select {
	case <-s.closed:
	case <-time.After(time.Second):
		t.Fatal("live session was not closed on browser disconnect")
	}
}

func TestInterruptedTurnDoesNotForwardStaleAudio(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider := &fakeVoiceProvider{}
	server := httptest.NewServer(NewRouter(ctx, fstest.MapFS{}, provider, nil, 0))
	defer server.Close()

	uri := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/live"
	conn, _, err := websocket.Dial(ctx, uri, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	// First input ensures the provider is ready and gives a baseline chunk.
	if err := conn.Write(ctx, websocket.MessageBinary, make([]byte, 640)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatal(err)
	}
	provider.mu.Lock()
	s := provider.session
	provider.mu.Unlock()

	s.events <- realtime.Event{Type: realtime.EventInterrupted, TurnID: 1}
	s.events <- realtime.Event{Type: realtime.EventAudio, TurnID: 1, Audio: make([]byte, 960), SampleRate: 24000}
	s.events <- realtime.Event{Type: realtime.EventTurnEnd, TurnID: 1}
	nextAudio := bytes.Repeat([]byte{5, 9}, 480)
	s.events <- realtime.Event{Type: realtime.EventAudio, TurnID: 2, Audio: nextAudio, SampleRate: 24000}
	s.events <- realtime.Event{Type: realtime.EventTurnEnd, TurnID: 2}

	typ, payload, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if typ != websocket.MessageText || !bytes.Contains(payload, []byte(`"type":"interrupted"`)) {
		t.Fatalf("expected interruption signal first, got kind=%d payload=%s", typ, payload)
	}
	typ, payload, err = conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if typ != websocket.MessageBinary || !bytes.Equal(payload, nextAudio) {
		t.Fatalf("expected fresh turn 2 audio without stale turn 1 output, got kind=%d bytes=%d", typ, len(payload))
	}
	typ, payload, err = conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if typ != websocket.MessageText || !bytes.Contains(payload, []byte(`"type":"turn_end"`)) {
		t.Fatalf("expected completion of fresh turn, got kind=%d payload=%s", typ, payload)
	}
}

func TestLiveTranscriptEvent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider := &fakeVoiceProvider{}
	server := httptest.NewServer(NewRouter(ctx, fstest.MapFS{}, provider, nil, 0))
	defer server.Close()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/live", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	// A microphone frame ensures the provider has been opened.
	if err := conn.Write(ctx, websocket.MessageBinary, make([]byte, 640)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatal(err)
	}
	provider.mu.Lock()
	s := provider.session
	provider.mu.Unlock()

	s.events <- realtime.Event{
		Type: realtime.EventTranscript,
		Transcript: &realtime.TranscriptSegment{
			ID: "caller-1", Speaker: realtime.SpeakerCaller,
			Text: "Bonjour", Final: true,
			Timestamp: time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC),
		},
	}
	kind, payload, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if kind != websocket.MessageText {
		t.Fatalf("expected transcript text event, got %d", kind)
	}
	var event struct {
		Type string `json:"type"`
		realtime.TranscriptSegment
	}
	if err := json.Unmarshal(payload, &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "transcript" || event.ID != "caller-1" ||
		event.Speaker != realtime.SpeakerCaller || !event.Final ||
		event.Text != "Bonjour" || event.Timestamp.IsZero() {
		t.Fatalf("unexpected transcript event: %+v", event)
	}
}

func TestLiveWithoutProvider(t *testing.T) {
	server := httptest.NewServer(NewRouter(context.Background(), fstest.MapFS{}, nil, nil, 0))
	defer server.Close()
	response, err := http.Get(server.URL + "/ws/live")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", response.StatusCode)
	}
}

func TestLiveHandoffEvent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider := &fakeVoiceProvider{}
	server := httptest.NewServer(NewRouter(ctx, fstest.MapFS{}, provider, nil, 0))
	defer server.Close()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/live", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	if err := conn.Write(ctx, websocket.MessageBinary, make([]byte, 640)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatal(err)
	}
	provider.mu.Lock()
	s := provider.session
	provider.mu.Unlock()

	s.events <- realtime.Event{
		Type: realtime.EventHandoffRequired,
		Handoff: &realtime.HandoffRequest{
			Reason: realtime.HandoffOwnerRequested,
			Context: realtime.HandoffContext{
				Purpose: "Recruitment",
				Summary: "Caller wants to speak to the owner about recruitment.",
			},
		},
	}
	kind, payload, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if kind != websocket.MessageText {
		t.Fatalf("expected JSON event, got %d", kind)
	}
	var event struct {
		Type string `json:"type"`
		Reason string `json:"reason"`
		Context struct {
			CallerName *string `json:"caller_name"`
			Purpose string `json:"purpose"`
			Summary string `json:"summary"`
		} `json:"context"`
	}
	if err := json.Unmarshal(payload, &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "handoff_required" || event.Reason != "owner_requested" ||
		event.Context.CallerName != nil || event.Context.Purpose != "Recruitment" ||
		event.Context.Summary == "" {
		t.Fatalf("unexpected handoff event: %+v", event)
	}
}

func TestLiveLifecycleControl(t *testing.T) {
	for _, tc := range []struct {
		action string
		want string
	}{
		{"accept", "human_active"},
		{"decline", "ai_active"},
		{"no_answer", "ai_active"},
	} {
		t.Run(tc.action, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			provider := &fakeVoiceProvider{}
			server := httptest.NewServer(NewRouter(ctx, fstest.MapFS{}, provider, nil, 0))
			defer server.Close()
			conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/live", nil)
			if err != nil { t.Fatal(err) }
			defer conn.CloseNow()

			// A frame both initializes the provider and confirms the bridge is ready.
			if err := conn.Write(ctx, websocket.MessageBinary, make([]byte, 640)); err != nil { t.Fatal(err) }
			if kind, _, err := conn.Read(ctx); err != nil || kind != websocket.MessageBinary {
				t.Fatalf("expected initial echo, kind=%v err=%v", kind, err)
			}
			provider.mu.Lock()
			s := provider.session
			provider.mu.Unlock()
			if s == nil { t.Fatal("missing provider session") }
			s.events <- realtime.Event{Type: realtime.EventHandoffRequired, Handoff: &realtime.HandoffRequest{
				Reason: realtime.HandoffOwnerRequested,
				Context: realtime.HandoffContext{Purpose: "Recruitment", Summary: "Caller wishes to discuss a job."},
			}}
			for _, want := range []string{"handoff_required", "call_state"} {
				kind, body, err := conn.Read(ctx)
				if err != nil { t.Fatal(err) }
				if kind != websocket.MessageText { t.Fatalf("expected text, got %v", kind) }
				var event struct { Type string `json:"type"`; State string `json:"state"` }
				if err := json.Unmarshal(body, &event); err != nil { t.Fatal(err) }
				if event.Type != want { t.Fatalf("got %s, want %s", event.Type, want) }
				if want == "call_state" && event.State != "handoff_required" { t.Fatalf("unexpected state: %s", event.State) }
			}

			if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"`+tc.action+`"}`)); err != nil { t.Fatal(err) }
			kind, body, err := conn.Read(ctx)
			if err != nil { t.Fatal(err) }
			var response struct {
				Type string `json:"type"`
				State string `json:"state"`
				Outcome string `json:"outcome"`
			}
			if kind != websocket.MessageText { t.Fatalf("expected state text, got %v", kind) }
			if err := json.Unmarshal(body, &response); err != nil { t.Fatal(err) }
			if response.Type != "call_state" || response.State != tc.want || response.Outcome != tc.action && !(tc.action == "accept" && response.Outcome == "accepted") {
				t.Fatalf("unexpected response: %+v", response)
			}

			if tc.action == "accept" {
				select {
				case <-s.closed:
				case <-time.After(time.Second): t.Fatal("Gemini session not closed after takeover")
				}
			} else {
				select {
				case <-s.closed: t.Fatal("Gemini session closed after declined handoff")
				default:
				}
			}
			if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"end_call"}`)); err != nil { t.Fatal(err) }
			kind, body, err = conn.Read(ctx)
			if err != nil { t.Fatal(err) }
			if kind != websocket.MessageText || !bytes.Contains(body, []byte(`"state":"completed"`)) {
				t.Fatalf("expected completion, got kind=%v payload=%s", kind, body)
			}
			select {
			case <-s.closed:
			case <-time.After(time.Second): t.Fatal("session did not close after end_call")
			}
		})
	}
}

func TestInvalidLifecycleCommandDoesNotChangeState(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider := &fakeVoiceProvider{}
	server := httptest.NewServer(NewRouter(ctx, fstest.MapFS{}, provider, nil, 0))
	defer server.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/live", nil)
	if err != nil { t.Fatal(err) }
	defer conn.CloseNow()

	for _, action := range []string{"accept", "decline", "no_answer", "invalid"} {
		if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"`+action+`"}`)); err != nil { t.Fatal(err) }
		kind, body, err := conn.Read(ctx)
		if err != nil { t.Fatal(err) }
		if kind != websocket.MessageText || !bytes.Contains(body, []byte(`"type":"call_error"`)) {
			t.Fatalf("expected validation error for %s, got %s", action, body)
		}
	}
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"end_call"}`)); err != nil { t.Fatal(err) }
	_, body, err := conn.Read(ctx)
	if err != nil || !bytes.Contains(body, []byte(`"state":"completed"`)) {
		t.Fatalf("expected completion after errors, payload=%s err=%v", body, err)
	}
}

func TestLiveNaturalCompletionWaitsForPlayback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider := &fakeVoiceProvider{}
	server := httptest.NewServer(NewRouter(ctx, fstest.MapFS{}, provider, nil, 0))
	defer server.Close()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/live", nil)
	if err != nil { t.Fatal(err) }
	defer conn.CloseNow()

	if err := conn.Write(ctx, websocket.MessageBinary, make([]byte, 640)); err != nil { t.Fatal(err) }
	if kind, _, err := conn.Read(ctx); err != nil || kind != websocket.MessageBinary {
		t.Fatalf("expected initial audio, kind=%v err=%v", kind, err)
	}
	provider.mu.Lock()
	s := provider.session
	provider.mu.Unlock()

	// The tool request must not immediately disconnect or discard final audio.
	s.events <- realtime.Event{Type: realtime.EventCallEndRequested}
	s.events <- realtime.Event{Type: realtime.EventAudio, TurnID: 2, Audio: make([]byte, 960), SampleRate: 24000}
	s.events <- realtime.Event{Type: realtime.EventTurnEnd, TurnID: 2}

	kind, _, err := conn.Read(ctx)
	if err != nil { t.Fatal(err) }
	if kind != websocket.MessageBinary { t.Fatalf("farewell audio was discarded: %v", kind) }
	kind, payload, err := conn.Read(ctx)
	if err != nil { t.Fatal(err) }
	if kind != websocket.MessageText || !bytes.Contains(payload, []byte(`"type":"turn_end"`)) {
		t.Fatalf("expected turn end before finish signal, got %s", payload)
	}
	kind, payload, err = conn.Read(ctx)
	if err != nil { t.Fatal(err) }
	if kind != websocket.MessageText || !bytes.Contains(payload, []byte(`"type":"call_finishing"`)) {
		t.Fatalf("expected finishing event, got %s", payload)
	}
	select {
	case <-s.closed: t.Fatal("provider closed before browser confirmed playback")
	default:
	}
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"playback_finished"}`)); err != nil { t.Fatal(err) }
	kind, payload, err = conn.Read(ctx)
	if err != nil { t.Fatal(err) }
	if kind != websocket.MessageText || !bytes.Contains(payload, []byte(`"state":"completed"`)) ||
		!bytes.Contains(payload, []byte(`"outcome":"caller_finished"`)) {
		t.Fatalf("expected completed state after playback, got %s", payload)
	}
	select {
	case <-s.closed:
	case <-time.After(time.Second): t.Fatal("Gemini not closed after completion")
	}
}

func TestPlaybackAcknowledgementRequiresFinishing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider := &fakeVoiceProvider{}
	server := httptest.NewServer(NewRouter(ctx, fstest.MapFS{}, provider, nil, 0))
	defer server.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/live", nil)
	if err != nil { t.Fatal(err) }
	defer conn.CloseNow()
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"playback_finished"}`)); err != nil { t.Fatal(err) }
	kind, payload, err := conn.Read(ctx)
	if err != nil { t.Fatal(err) }
	if kind != websocket.MessageText || !bytes.Contains(payload, []byte(`"type":"call_error"`)) {
		t.Fatalf("expected rejected premature playback ack, got %s", payload)
	}
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"end_call"}`)); err != nil { t.Fatal(err) }
	_, payload, err = conn.Read(ctx)
	if err != nil || !bytes.Contains(payload, []byte(`"state":"completed"`)) {
		t.Fatalf("manual completion failed: %s (%v)", payload, err)
	}
}

func TestIdleExpired(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct{
		name string
		last time.Time
		state call.State
		speaking bool
		want bool
	}{
		{"silent for 61 seconds", now.Add(-61*time.Second), call.AIActive, false, true},
		{"recent speech", now.Add(-20*time.Second), call.AIActive, false, false},
		{"AI still speaking", now.Add(-70*time.Second), call.AIActive, true, false},
		{"handoff pending", now.Add(-70*time.Second), call.HandoffRequired, false, false},
		{"human active", now.Add(-70*time.Second), call.HumanActive, false, false},
		{"completed", now.Add(-70*time.Second), call.Completed, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := idleExpired(now, tc.last, time.Minute, tc.state, tc.speaking); got != tc.want {
				t.Fatalf("idleExpired()=%t want %t", got, tc.want)
			}
		})
	}
}

func TestConfiguredIdleTimeout(t *testing.T) {
	t.Setenv("CALL_IDLE_TIMEOUT_SECONDS", "90")
	if got := configuredIdleTimeout(); got != 90*time.Second { t.Fatalf("got %s", got) }
	t.Setenv("CALL_IDLE_TIMEOUT_SECONDS", "1")
	if got := configuredIdleTimeout(); got != defaultIdleTimeout { t.Fatalf("invalid timeout got %s", got) }
}


func TestLiveFarewellInterruptionAllowsAnotherEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider := &fakeVoiceProvider{}
	server := httptest.NewServer(NewRouter(ctx, fstest.MapFS{}, provider, nil, 0))
	defer server.Close()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/live", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	if err := conn.Write(ctx, websocket.MessageBinary, make([]byte, 640)); err != nil {
		t.Fatal(err)
	}
	if kind, _, err := conn.Read(ctx); err != nil || kind != websocket.MessageBinary {
		t.Fatalf("expected initial audio: kind=%v, err=%v", kind, err)
	}
	provider.mu.Lock()
	voice := provider.session
	provider.mu.Unlock()

	voice.events <- realtime.Event{Type: realtime.EventCallEndRequested}
	voice.events <- realtime.Event{Type: realtime.EventInterrupted, TurnID: 1}

	for _, kind := range []string{"call_end_cancelled", "interrupted"} {
		typ, body, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var event struct {
			Type string `json:"type"`
		}
		if typ != websocket.MessageText || json.Unmarshal(body, &event) != nil || event.Type != kind {
			t.Fatalf("expected %s, got %s", kind, body)
		}
	}
	// The cancelled farewell must not prevent a subsequent request.
	voice.events <- realtime.Event{Type: realtime.EventCallEndRequested}
	voice.events <- realtime.Event{Type: realtime.EventTurnEnd, TurnID: 2}

	for _, kind := range []string{"turn_end", "call_finishing"} {
		typ, body, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var event struct {
			Type string `json:"type"`
		}
		if typ != websocket.MessageText || json.Unmarshal(body, &event) != nil || event.Type != kind {
			t.Fatalf("expected %s, got %s", kind, body)
		}
	}
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"playback_finished"}`)); err != nil {
		t.Fatal(err)
	}
	_, body, err := conn.Read(ctx)
	if err != nil || !bytes.Contains(body, []byte(`"state":"completed"`)) {
		t.Fatalf("expected completed state, payload=%s err=%v", body, err)
	}
}

func TestLiveSessionRejectsDuplicateCompletionRequests(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider := &fakeVoiceProvider{}
	server := httptest.NewServer(NewRouter(ctx, fstest.MapFS{}, provider, nil, 0))
	defer server.Close()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/live", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	if err := conn.Write(ctx, websocket.MessageBinary, make([]byte, 640)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatal(err)
	}
	provider.mu.Lock()
	voice := provider.session
	provider.mu.Unlock()

	voice.events <- realtime.Event{Type: realtime.EventCallEndRequested}
	voice.events <- realtime.Event{Type: realtime.EventCallEndRequested}
	voice.events <- realtime.Event{Type: realtime.EventTurnEnd, TurnID: 1}
	for _, kind := range []string{"turn_end", "call_finishing"} {
		_, body, err := conn.Read(ctx)
		if err != nil || !bytes.Contains(body, []byte(`"type":"`+kind+`"`)) {
			t.Fatalf("expected %s, body=%s err=%v", kind, body, err)
		}
	}
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"playback_finished"}`)); err != nil {
		t.Fatal(err)
	}
	_, body, err := conn.Read(ctx)
	if err != nil || !bytes.Contains(body, []byte(`"state":"completed"`)) {
		t.Fatalf("expected completed state, payload=%s err=%v", body, err)
	}
}


type fakeSummarizer struct {
	mu sync.Mutex
	input call.SummaryInput
	err error
}

func (s *fakeSummarizer) Summarize(_ context.Context, input call.SummaryInput) (call.Summary, error) {
	s.mu.Lock()
	s.input = input
	s.mu.Unlock()
	if s.err != nil { return call.Summary{}, s.err }
	return call.Summary{
		Urgency: "unknown",
		Text: "L'appelant souhaite discuter de chaussures.",
		FollowUpActions: []string{"Recontacter l'appelant si nécessaire."},
	}, nil
}

func TestCallSummarySentAfterCompleted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider := &fakeVoiceProvider{}
	summarizer := &fakeSummarizer{}
	server := httptest.NewServer(NewRouter(ctx, fstest.MapFS{}, provider, summarizer, realtime.LocaleFrench))
	defer server.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/live?locale=en-US", nil)
	if err != nil { t.Fatal(err) }
	defer conn.CloseNow()
	if err := conn.Write(ctx, websocket.MessageBinary, make([]byte, 640)); err != nil { t.Fatal(err) }
	if kind, _, err := conn.Read(ctx); err != nil || kind != websocket.MessageBinary { t.Fatalf("audio: %v", err) }
	provider.mu.Lock()
	voice := provider.session
	provider.mu.Unlock()
	voice.events <- realtime.Event{Type: realtime.EventTranscript, Transcript: &realtime.TranscriptSegment{
		ID: "caller-1", Speaker: realtime.SpeakerCaller, Text: "I want to sell shoes", Final: true,
	}}
	voice.events <- realtime.Event{Type: realtime.EventHandoffRequired, Handoff: &realtime.HandoffRequest{
		Reason: realtime.HandoffOwnerRequested,
		Context: realtime.HandoffContext{Purpose: "Offer shoes", Summary: "Caller wants to offer shoes"},
	}}
	// Read the two metadata notifications plus the transcript before ending.
	for i := 0; i < 3; i++ {
		if kind, _, err := conn.Read(ctx); err != nil || kind != websocket.MessageText {
			t.Fatalf("metadata %d: %v", i, err)
		}
	}
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"end_call"}`)); err != nil { t.Fatal(err) }
	var state struct {Type string `json:"type"`; State string `json:"state"`}
	_, body, err := conn.Read(ctx)
	if err != nil { t.Fatal(err) }
	if err := json.Unmarshal(body, &state); err != nil || state.Type != "call_state" || state.State != "completed" {
		t.Fatalf("expected completed state first: %s (%v)", body, err)
	}
	_, body, err = conn.Read(ctx)
	if err != nil { t.Fatal(err) }
	var output struct {
		Type string `json:"type"`
		Summary call.Summary `json:"summary"`
	}
	if err := json.Unmarshal(body, &output); err != nil || output.Type != "call_summary" {
		t.Fatalf("expected summary: %s (%v)", body, err)
	}
	if !output.Summary.HandoffRequested || output.Summary.Text == "" {
		t.Fatalf("summary incorrect: %+v", output.Summary)
	}
	summarizer.mu.Lock()
	input := summarizer.input
	summarizer.mu.Unlock()
	if len(input.Transcript) != 1 || input.Transcript[0].Text != "I want to sell shoes" ||
		input.Handoff == nil || input.EndReason != call.EndManual || input.Locale != realtime.LocaleEnglish {
		t.Fatalf("summarizer input incorrect: %+v", input)
	}
}

func TestSummaryFailureDoesNotBlockCompletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider := &fakeVoiceProvider{}
	server := httptest.NewServer(NewRouter(ctx, fstest.MapFS{}, provider, &fakeSummarizer{
		err: errors.New("model offline"),
	}, 0))
	defer server.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/live", nil)
	if err != nil { t.Fatal(err) }
	defer conn.CloseNow()
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"end_call"}`)); err != nil { t.Fatal(err) }
	_, body, err := conn.Read(ctx)
	if err != nil || !bytes.Contains(body, []byte(`"state":"completed"`)) {
		t.Fatalf("call completion failed: %s (%v)", body, err)
	}
	_, body, err = conn.Read(ctx)
	if err != nil || !bytes.Contains(body, []byte(`"type":"summary_error"`)) {
		t.Fatalf("expected summary_error after completion: %s (%v)", body, err)
	}
}


func TestLiveAudioStreamEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider := &fakeVoiceProvider{}
	server := httptest.NewServer(NewRouter(ctx, fstest.MapFS{}, provider, nil, 0))
	defer server.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/live", nil)
	if err != nil { t.Fatal(err) }
	defer conn.CloseNow()

	pcm := bytes.Repeat([]byte{1, 2}, 320)
	if err := conn.Write(ctx, websocket.MessageBinary, pcm); err != nil { t.Fatal(err) }
	if kind, _, err := conn.Read(ctx); err != nil || kind != websocket.MessageBinary {
		t.Fatalf("expected audio before stream end: kind=%v err=%v", kind, err)
	}
	provider.mu.Lock()
	s := provider.session
	provider.mu.Unlock()
	if s == nil { t.Fatal("provider session not initialized") }

	end := []byte(`{"type":"audio_stream_end"}`)
	if err := conn.Write(ctx, websocket.MessageText, end); err != nil { t.Fatal(err) }
	select {
	case <-s.streamEnds:
	case <-ctx.Done(): t.Fatal("provider did not receive audio stream end")
	}

	// A second end must not reach the provider. The next echoed PCM acts
	// as an ordering barrier for both previously received messages.
	if err := conn.Write(ctx, websocket.MessageText, end); err != nil { t.Fatal(err) }
	if err := conn.Write(ctx, websocket.MessageBinary, pcm); err != nil { t.Fatal(err) }
	if kind, _, err := conn.Read(ctx); err != nil || kind != websocket.MessageBinary {
		t.Fatalf("audio did not resume after stream end: kind=%v err=%v", kind, err)
	}
	select {
	case <-s.streamEnds: t.Fatal("duplicate end was forwarded to provider")
	default:
	}
	if err := conn.Write(ctx, websocket.MessageText, end); err != nil { t.Fatal(err) }
	select {
	case <-s.streamEnds:
	case <-ctx.Done(): t.Fatal("resumed speech stream did not end")
	}
	select {
	case <-s.streamEnds: t.Fatal("unexpected additional end event")
	default:
	}
}

func TestAudioStreamEndIgnoredWithoutPriorAudio(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider := &fakeVoiceProvider{}
	server := httptest.NewServer(NewRouter(ctx, fstest.MapFS{}, provider, nil, 0))
	defer server.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/live", nil)
	if err != nil { t.Fatal(err) }
	defer conn.CloseNow()

	// The provider opens on connect, but no stream has been started.
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"audio_stream_end"}`)); err != nil { t.Fatal(err) }
	if err := conn.Write(ctx, websocket.MessageBinary, make([]byte, 640)); err != nil { t.Fatal(err) }
	if kind, _, err := conn.Read(ctx); err != nil || kind != websocket.MessageBinary {
		t.Fatalf("connection was disrupted by empty stream end: kind=%v err=%v", kind, err)
	}
	provider.mu.Lock()
	s := provider.session
	provider.mu.Unlock()
	select {
	case <-s.streamEnds: t.Fatal("empty stream sent unnecessary end signal")
	default:
	}
}

func TestAudioStreamEndRejectedDuringHumanTakeover(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider := &fakeVoiceProvider{}
	server := httptest.NewServer(NewRouter(ctx, fstest.MapFS{}, provider, nil, 0))
	defer server.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/live", nil)
	if err != nil { t.Fatal(err) }
	defer conn.CloseNow()
	if err := conn.Write(ctx, websocket.MessageBinary, make([]byte, 640)); err != nil { t.Fatal(err) }
	if _, _, err := conn.Read(ctx); err != nil { t.Fatal(err) }
	provider.mu.Lock()
	s := provider.session
	provider.mu.Unlock()
	s.events <- realtime.Event{
		Type: realtime.EventHandoffRequired,
		Handoff: &realtime.HandoffRequest{
			Reason: realtime.HandoffOwnerRequested,
			Context: realtime.HandoffContext{Purpose: "Test", Summary: "Owner requested"},
		},
	}
	for i := 0; i < 2; i++ {
		if _, _, err := conn.Read(ctx); err != nil { t.Fatal(err) }
	}
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"accept"}`)); err != nil { t.Fatal(err) }
	if _, _, err := conn.Read(ctx); err != nil { t.Fatal(err) }
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"audio_stream_end"}`)); err != nil { t.Fatal(err) }
	kind, body, err := conn.Read(ctx)
	if err != nil { t.Fatal(err) }
	if kind != websocket.MessageText || !bytes.Contains(body, []byte(`"type":"call_error"`)) ||
		!bytes.Contains(body, []byte(`"action":"audio_stream_end"`)) {
		t.Fatalf("expected rejected stream end after takeover, got %s", body)
	}
	select {
	case <-s.streamEnds: t.Fatal("audio stream end forwarded during human takeover")
	default:
	}
}

func TestLiveHandoffMutesGeneratedSpeechAndTranscriptUntilOutcome(t *testing.T) {
  ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
  defer cancel()
  provider := &fakeVoiceProvider{}
  server := httptest.NewServer(NewRouter(ctx, fstest.MapFS{}, provider, nil, realtime.LocaleFrench))
  defer server.Close()
  conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/live", nil)
  if err != nil { t.Fatal(err) }
  defer conn.CloseNow()

  // Initialize the provider and verify that audio normally passes through.
  if err := conn.Write(ctx, websocket.MessageBinary, make([]byte, 640)); err != nil { t.Fatal(err) }
  if kind, _, err := conn.Read(ctx); err != nil || kind != websocket.MessageBinary {
    t.Fatalf("expected initial audio, kind=%v err=%v", kind, err)
  }
  provider.mu.Lock()
  s := provider.session
  provider.mu.Unlock()

  s.events <- realtime.Event{Type: realtime.EventHandoffRequired, Handoff: &realtime.HandoffRequest{
    Reason: realtime.HandoffOwnerRequested,
    Context: realtime.HandoffContext{Purpose: "Recruitment", Summary: "Caller asks for an interview"},
  }}
  for _, expected := range []string{"handoff_required", "call_state"} {
    kind, body, err := conn.Read(ctx)
    if err != nil || kind != websocket.MessageText || !bytes.Contains(body, []byte(`"type":"`+expected+`"`)) {
      t.Fatalf("expected %s, kind=%v body=%s err=%v", expected, kind, body, err)
    }
  }
  // The caller transcript is a deterministic barrier; if any unwanted
  // audio, AI transcript, or turn_end escaped the gate, it arrives first.
  s.events <- realtime.Event{Type: realtime.EventAudio, TurnID: 7, Audio: make([]byte, 960), SampleRate: 24000}
  s.events <- realtime.Event{Type: realtime.EventTranscript, Transcript: &realtime.TranscriptSegment{
    ID: "ai-unwanted", Speaker: realtime.SpeakerAI, Text: "La demande a été notée", Final: true,
  }}
  s.events <- realtime.Event{Type: realtime.EventTurnEnd, TurnID: 7}
  s.events <- realtime.Event{Type: realtime.EventTranscript, Transcript: &realtime.TranscriptSegment{
    ID: "caller-barrier", Speaker: realtime.SpeakerCaller, Text: "Oui", Final: true,
  }}
  kind, body, err := conn.Read(ctx)
  if err != nil || kind != websocket.MessageText ||
    !bytes.Contains(body, []byte(`"type":"transcript"`)) ||
    !bytes.Contains(body, []byte(`"segment_id":"caller-barrier"`)) {
    t.Fatalf("output escaped while handoff pending: kind=%v body=%s err=%v", kind, body, err)
  }
  if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"decline"}`)); err != nil { t.Fatal(err) }
  kind, body, err = conn.Read(ctx)
  if err != nil || kind != websocket.MessageText || !bytes.Contains(body, []byte(`"state":"ai_active"`)) {
    t.Fatalf("decline did not resume session: %s err=%v", body, err)
  }
  // After an explicit outcome, new assistant audio can be forwarded.
  s.events <- realtime.Event{Type: realtime.EventAudio, TurnID: 8, Audio: make([]byte, 960), SampleRate: 24000}
  kind, body, err = conn.Read(ctx)
  if err != nil || kind != websocket.MessageBinary || len(body) != 960 {
    t.Fatalf("assistant did not resume after decline: kind=%v bytes=%d err=%v", kind, len(body), err)
  }
}
