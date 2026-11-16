// Package logging provides Callio's logging abstraction.
// Only the private adapter knows about the concrete logging library.
package logging

import (
	"io"
	"os"
	"sync/atomic"
)

// Logger is the only logger contract used by application services.
// Key/value arguments are structured attributes; With returns a new logger.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	With(args ...any) Logger
}

type rootEntry struct {
	logger Logger
}

// A slot rather than a concrete adapter keeps this file independent of slog.
// The atomic pointer makes reads safe even during initialization.
var root atomic.Pointer[rootEntry]

// L returns the current root logger, or the JSON fallback before Init.
func L() Logger {
	if entry := root.Load(); entry != nil {
		return entry.logger
	}
	return fallbackLogger
}

// For returns a logger associated with one named application component.
func For(component string) Logger {
	return L().With("component", component)
}

// Init selects a logging level and installs one root JSON logger on stdout.
// It must be called before creating application services.
func Init(level string) error {
	logger, err := New(os.Stdout, level)
	if err != nil {
		return err
	}
	root.Store(&rootEntry{logger: logger})
	return nil
}

// New creates an independent logger, useful for tests and injected dependencies.
func New(output io.Writer, level string) (Logger, error) {
	return newSlogLogger(output, level)
}
