package httpapi

import (
	"context"
	"io/fs"
	"net/http"

	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
	"github.com/alexandrevanhoutte/CallioPoc/internal/call"
)

// NewRouter registers HTTP routes. Dependencies are provided by the caller.
func NewRouter(appCtx context.Context, webFiles fs.FS, provider realtime.VoiceProvider, summarizer call.Summarizer) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health)
	mux.HandleFunc("GET /capabilities", capabilities(provider != nil))
	mux.Handle("GET /ws/audio", audioEcho(appCtx))
	mux.Handle("GET /ws/live", audioLive(appCtx, provider, summarizer))
	mux.Handle("GET /", http.FileServer(http.FS(webFiles)))
	return mux
}
