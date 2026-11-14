package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/alexandrevanhoutte/CallioPoc/internal/config"
	"github.com/alexandrevanhoutte/CallioPoc/internal/httpapi"
	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime/gemini"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load configuration", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Echo remains usable without an API key; only /ws/live needs credentials.
	var provider realtime.VoiceProvider
	if cfg.GeminiAPIKey != "" {
		geminiProvider, err := gemini.New(ctx, cfg.GeminiAPIKey, cfg.GeminiModel)
		if err != nil {
			logger.Error("initialize AI provider", "error", err)
			os.Exit(1)
		}
		provider = geminiProvider
	} else {
		logger.Warn("GEMINI_API_KEY missing; live AI disabled (echo still available)")
	}

	router := httpapi.NewRouter(ctx, os.DirFS("web"), provider)
	server := httpapi.NewServer("127.0.0.1:"+cfg.Port, router)
	if err := httpapi.Run(ctx, server, logger); err != nil {
		logger.Error("HTTP server stopped with error", "error", err)
		os.Exit(1)
	}
}
