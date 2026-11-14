package httpapi

import (
	"context"
	"io/fs"
	"net/http"

	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
)

// NewRouter registers HTTP routes. Dependencies are provided by the caller.
func NewRouter(appCtx context.Context, webFiles fs.FS, provider realtime.VoiceProvider) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health)
	mux.Handle("GET /ws/audio", audioEcho(appCtx))
	mux.Handle("GET /ws/live", audioLive(appCtx, provider))
	mux.Handle("GET /", http.FileServer(http.FS(webFiles)))
	return mux
}
