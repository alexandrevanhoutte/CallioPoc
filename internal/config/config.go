package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
)

const (
	defaultPort  = "8080"
	defaultModel = "gemini-3.8-live"
)

type Config struct {
	Port         string
	AIProvider   string
	GeminiAPIKey string
	GeminiModel  string
	GreetingLocale realtime.GreetingLocale
}

func Load() (Config, error) {
	// Load only if the file exists. Existing process environment wins.
	if err := godotenv.Load(".env"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("load .env: %w", err)
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
	localeValue := os.Getenv("CALL_GREETING_LOCALE")
	if localeValue == "" {
		localeValue = string(realtime.GreetingFrench)
	}
	locale, err := realtime.NewGreetingLocale(localeValue)
	if err != nil {
		return Config{}, fmt.Errorf("invalid CALL_GREETING_LOCALE: %w", err)
	}
	return Config{
		Port: port, AIProvider: provider,
		GeminiAPIKey: os.Getenv("GEMINI_API_KEY"),
		GeminiModel: model, GreetingLocale: locale,
	}, nil
}
