package httpapi

import (
	"context"
	"io/fs"
	"net/http"
)

// NewRouter registers the HTTP routes. Dependencies are provided by the caller.
func NewRouter(appCtx context.Context, webFiles fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health)
	mux.Handle("GET /ws/audio", audioEcho(appCtx))
	mux.Handle("GET /", http.FileServer(http.FS(webFiles)))
	return mux
}
