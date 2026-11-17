package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/alexandrevanhoutte/CallioPoc/internal/logging"
)

const shutdownTimeout = 10 * time.Second

func NewServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

// Run serves until an error or context cancellation, then closes gracefully.
func Run(ctx context.Context, server *http.Server, logger logging.Logger) error {
	if logger == nil {
		logger = logging.For("httpapi.server")
	}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()
	logger.Info("HTTP server started", "addr", listener.Addr().String())

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
		logger.Info("HTTP server stopping")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	shutdownErr := server.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		_ = server.Close()
	}
	serveErrResult := <-serveErr
	if shutdownErr != nil {
		return fmt.Errorf("shutdown: %w", shutdownErr)
	}
	if serveErrResult != nil && !errors.Is(serveErrResult, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", serveErrResult)
	}
	logger.Info("HTTP server stopped")
	return nil
}
