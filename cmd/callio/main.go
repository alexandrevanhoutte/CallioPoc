package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"calliopoc/internal/config"
	"calliopoc/internal/httpapi"
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

	router := httpapi.NewRouter(os.DirFS("web"))
	server := httpapi.NewServer("127.0.0.1:"+cfg.Port, router)
	if err := httpapi.Run(ctx, server, logger); err != nil {
		logger.Error("HTTP server stopped with error", "error", err)
		os.Exit(1)
	}
}
