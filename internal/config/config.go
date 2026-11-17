package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

const (
	defaultBindHost     = "127.0.0.1"
	defaultPort         = "8080"
	defaultModel        = "gemini-3.8-live"
	defaultSummaryModel = "gemini-3.5-flash-lite"
)

type Config struct {
	BindHost     string
	Port         string
	AIProvider   string
	GeminiAPIKey string
	GeminiModel  string
	SummaryModel string
}

func Load() (Config, error) {
	// Load only if the file exists. Existing process environment wins.
	if err := godotenv.Load(".env"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("load .env: %w", err)
	}

	bindHost := os.Getenv("CALLIO_BIND_HOST")
	if bindHost == "" {
		bindHost = defaultBindHost
	}
	if net.ParseIP(bindHost) == nil {
		return Config{}, fmt.Errorf("invalid CALLIO_BIND_HOST %q: expected an IP address", bindHost)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return Config{}, fmt.Errorf("invalid PORT %q: expected a number from 1 to 65535", port)
	}

	provider := os.Getenv("AI_PROVIDER")
	if provider == "" {
		provider = "gemini"
	}
	if provider != "gemini" {
		return Config{}, fmt.Errorf("unsupported AI_PROVIDER %q (only gemini is implemented)", provider)
	}

	model := os.Getenv("GEMINI_MODEL")
	if model == "" {
		model = defaultModel
	}

	summaryModel := os.Getenv("GEMINI_SUMMARY_MODEL")
	if summaryModel == "" {
		summaryModel = defaultSummaryModel
	}

	return Config{
		BindHost:     bindHost,
		Port:         port,
		AIProvider:   provider,
		GeminiAPIKey: os.Getenv("GEMINI_API_KEY"),
		GeminiModel:  model,
		SummaryModel: summaryModel,
	}, nil
}
