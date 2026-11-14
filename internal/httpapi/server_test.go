package httpapi_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/alexandrevanhoutte/CallioPoc/internal/httpapi"
)

func TestRun(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("listen error", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()

		server := httpapi.NewServer(listener.Addr().String(), http.NewServeMux())
		err = httpapi.Run(context.Background(), server, logger)
		if err == nil || !strings.Contains(err.Error(), "listen:") {
			t.Fatalf("Run() error = %v, want listen error", err)
		}
	})

	t.Run("graceful shutdown", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		started := make(chan string, 1)
		server := httpapi.NewServer("127.0.0.1:0", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		server.BaseContext = func(listener net.Listener) context.Context {
			started <- listener.Addr().String()
			return context.Background()
		}

		result := make(chan error, 1)
		go func() { result <- httpapi.Run(ctx, server, logger) }()

		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("server did not start")
		}
		cancel()
		select {
		case err := <-result:
			if err != nil {
				t.Fatalf("Run() = %v, want nil", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("server did not shut down")
		}
	})
}
