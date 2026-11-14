package httpapi_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/alexandrevanhoutte/CallioPoc/internal/httpapi"
	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
	"github.com/coder/websocket"
)

type fakeVoiceProvider struct {
	mu sync.Mutex
	session *fakeVoiceSession
}

func (p *fakeVoiceProvider) Open(_ context.Context) (realtime.VoiceSession, error) {
	s := &fakeVoiceSession{
		events: make(chan realtime.Event, 8),
		closed: make(chan struct{}),
	}
	p.mu.Lock()
	p.session = s
	p.mu.Unlock()
	return s, nil
}

type fakeVoiceSession struct {
	events chan realtime.Event
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

func TestLiveBridge(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider := &fakeVoiceProvider{}
	server := httptest.NewServer(httpapi.NewRouter(ctx, fstest.MapFS{}, provider))
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
	server := httptest.NewServer(httpapi.NewRouter(ctx, fstest.MapFS{}, provider))
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

func TestLiveWithoutProvider(t *testing.T) {
	server := httptest.NewServer(httpapi.NewRouter(context.Background(), fstest.MapFS{}, nil))
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
