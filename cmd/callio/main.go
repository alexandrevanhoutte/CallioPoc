package main

import (
	"context"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/alexandrevanhoutte/CallioPoc/internal/call"
	"github.com/alexandrevanhoutte/CallioPoc/internal/config"
	"github.com/alexandrevanhoutte/CallioPoc/internal/httpapi"
	"github.com/alexandrevanhoutte/CallioPoc/internal/logging"
	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime/gemini"
)

func main() {
	logger := logging.For("main")
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load configuration", "error", err)
		os.Exit(1)
	}

	if err := logging.Init(os.Getenv("LOG_LEVEL")); err != nil {
		logger.Error("initialize logging", "error", err)
		os.Exit(1)
	}
	logger = logging.For("main")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Echo remains usable without an API key; only /ws/live needs credentials.
	var provider realtime.VoiceProvider
	var summarizer call.Summarizer
	if cfg.GeminiAPIKey != "" {
		geminiProvider, err := gemini.New(ctx, cfg.GeminiAPIKey, cfg.GeminiModel)
		if err != nil {
			logger.Error("initialize AI provider", "error", err)
			os.Exit(1)
		}
		provider = geminiProvider
		summarizer, err = gemini.NewSummarizer(ctx, cfg.GeminiAPIKey, cfg.SummaryModel)
		if err != nil {
			logger.Error("initialize call summarizer", "error", err)
			os.Exit(1)
		}
	} else {
		logger.Warn("GEMINI_API_KEY missing; live AI disabled (echo still available)")
	}

	router := httpapi.NewRouter(ctx, os.DirFS("web"), provider, summarizer, realtime.LocaleFrench)
	server := httpapi.NewServer(net.JoinHostPort(cfg.BindHost, cfg.Port), router)
	if err := httpapi.Run(ctx, server, logging.For("httpapi.server")); err != nil {
		logger.Error("HTTP server stopped with error", "error", err)
		os.Exit(1)
	}
}
