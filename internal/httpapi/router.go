package httpapi

import (
	"io/fs"
	"net/http"
)

// NewRouter registers the HTTP routes. Dependencies are provided by the caller.
func NewRouter(webFiles fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health)
	mux.Handle("GET /", http.FileServer(http.FS(webFiles)))
	return mux
}
