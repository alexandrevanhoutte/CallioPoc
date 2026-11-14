package httpapi_test

import (
	"bytes"
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/alexandrevanhoutte/CallioPoc/internal/httpapi"
	"github.com/coder/websocket"
)

func echoServer(t *testing.T, ctx context.Context) string {
	t.Helper()
	server := httptest.NewServer(httpapi.NewRouter(ctx, fstest.MapFS{}, nil, nil))
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/audio"
}

func TestAudioEcho(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	uri := echoServer(t, ctx)
	conn, _, err := websocket.Dial(ctx, uri, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	chunk := make([]byte, 640)
	for i := range chunk {
		chunk[i] = byte(i)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, chunk); err != nil {
		t.Fatal(err)
	}
	typ, result, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if typ != websocket.MessageBinary || !bytes.Equal(result, chunk) {
		t.Fatalf("echo mismatch: type=%v bytes=%d", typ, len(result))
	}
}

func TestAudioEchoRejectsInvalidFrames(t *testing.T) {
	for _, test := range []struct {
		name string
		typ  websocket.MessageType
		data []byte
	}{
		{name: "text", typ: websocket.MessageText, data: []byte(strings.Repeat("x", 640))},
		{name: "too short", typ: websocket.MessageBinary, data: []byte{1, 2}},
		{name: "too big", typ: websocket.MessageBinary, data: make([]byte, 641)},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			conn, _, err := websocket.Dial(ctx, echoServer(t, ctx), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			if err := conn.Write(ctx, test.typ, test.data); err != nil {
				t.Fatal(err)
			}
			_, _, err = conn.Read(ctx)
			if err == nil {
				t.Fatal("expected websocket close for invalid frame")
			}
		})
	}
}

func TestAudioEchoClosesOnShutdown(t *testing.T) {
	appCtx, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, echoServer(t, appCtx), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	cancelApp()
	if _, _, err := conn.Read(ctx); err == nil {
		t.Fatal("connection remained open after application shutdown")
	}
}
