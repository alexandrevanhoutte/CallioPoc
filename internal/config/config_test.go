package config_test

import (
	"testing"

	"github.com/alexandrevanhoutte/CallioPoc/internal/config"
)

func TestLoad(t *testing.T) {
	for _, tc := range []struct {
		name    string
		port    string
		want    string
		wantErr bool
	}{
		{name: "default", want: "8080"},
		{name: "custom", port: "9090", want: "9090"},
		{name: "non numeric", port: "oops", wantErr: true},
		{name: "out of range", port: "65536", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PORT", tc.port)
			t.Setenv("AI_PROVIDER", "gemini")
			t.Setenv("GEMINI_API_KEY", "")
			t.Setenv("GEMINI_MODEL", "")
			t.Setenv("CALL_GREETING_LOCALE", "")
			cfg, err := config.Load()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Load() error = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && cfg.Port != tc.want {
				t.Errorf("Port = %q, want %q", cfg.Port, tc.want)
			}
			if !tc.wantErr && cfg.GeminiModel != "gemini-3.8-live" {
				t.Errorf("GeminiModel = %q", cfg.GeminiModel)
			}
		})
	}
}

func TestRejectUnknownProvider(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("AI_PROVIDER", "unknown")
	if _, err := config.Load(); err == nil {
		t.Fatal("expected unsupported provider error")
	}
}

func TestGreetingLocaleConfig(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("AI_PROVIDER", "gemini")
	for _, locale := range []string{"", "fr-FR", "ko-KR", "en-US", "es-ES"} {
		t.Run(locale, func(t *testing.T) {
			t.Setenv("CALL_GREETING_LOCALE", locale)
			cfg, err := config.Load()
			if locale == "es-ES" {
				if err == nil { t.Fatal("unsupported locale must fail") }
				return
			}
			if err != nil { t.Fatal(err) }
			want := locale
			if want == "" { want = "fr-FR" }
			if string(cfg.GreetingLocale) != want { t.Fatalf("got %q, want %q", cfg.GreetingLocale, want) }
		})
	}
}
