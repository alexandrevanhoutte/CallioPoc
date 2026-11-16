package logging

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// The only concrete slog dependency in Callio.
type slogAdapter struct {
	logger *slog.Logger
}

var _ Logger = (*slogAdapter)(nil)

var fallbackLogger Logger = &slogAdapter{
	logger: slog.New(slog.NewJSONHandler(os.Stdout, nil)),
}

func newSlogLogger(output io.Writer, levelName string) (Logger, error) {
	if output == nil {
		return nil, errors.New("logging output must not be nil")
	}
	var level slog.Level
	switch strings.ToLower(strings.TrimSpace(levelName)) {
	case "", "info":
		level = slog.LevelInfo
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		return nil, fmt.Errorf("invalid LOG_LEVEL %q: expected debug, info, warn or error", levelName)
	}
	return &slogAdapter{
		logger: slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{Level: level})),
	}, nil
}

func (s *slogAdapter) Debug(msg string, args ...any) { s.logger.Debug(msg, args...) }
func (s *slogAdapter) Info(msg string, args ...any)  { s.logger.Info(msg, args...) }
func (s *slogAdapter) Warn(msg string, args ...any)  { s.logger.Warn(msg, args...) }
func (s *slogAdapter) Error(msg string, args ...any) { s.logger.Error(msg, args...) }

func (s *slogAdapter) With(args ...any) Logger {
	return &slogAdapter{logger: s.logger.With(args...)}
}
