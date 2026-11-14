package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/coder/websocket"
)

// Each binary WebSocket message is one 20 ms PCM16 LE mono 16 kHz chunk.
const audioChunkBytes = 640

func audioEcho(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil) // Same-origin by default.
		if err != nil {
			return // Accept already writes an HTTP error response.
		}
		defer conn.CloseNow()
		conn.SetReadLimit(audioChunkBytes)

		// WebSocket connections are hijacked: http.Server.Shutdown does not close
		// them, so stop them explicitly when the application context is cancelled.
		sessionCtx, cancel := context.WithCancel(r.Context())
		defer cancel()
		stopShutdown := context.AfterFunc(ctx, cancel)
		defer stopShutdown()

		for {
			typ, data, err := conn.Read(sessionCtx)
			if err != nil {
				if ctx.Err() == nil && websocket.CloseStatus(err) == -1 && r.Context().Err() == nil {
					slog.Debug("audio websocket read ended", "error", err)
				}
				return
			}
			if typ != websocket.MessageBinary || len(data) != audioChunkBytes {
				_ = conn.Close(websocket.StatusUnsupportedData, "expected 640-byte PCM16 binary chunk")
				return
			}
			if err := conn.Write(sessionCtx, websocket.MessageBinary, data); err != nil {
				slog.Debug("audio websocket write ended", "error", err)
				return
			}
		}
	}
}
