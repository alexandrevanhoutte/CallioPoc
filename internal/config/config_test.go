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
			t.Setenv("CALLIO_BIND_HOST", "")
			t.Setenv("AI_PROVIDER", "gemini")
			t.Setenv("GEMINI_API_KEY", "")
			t.Setenv("GEMINI_MODEL", "")
			t.Setenv("GEMINI_SUMMARY_MODEL", "")

			cfg, err := config.Load()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Load() error = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && cfg.Port != tc.want {
				t.Errorf("Port = %q, want %q", cfg.Port, tc.want)
			}
			if !tc.wantErr && cfg.BindHost != "127.0.0.1" {
				t.Errorf("BindHost = %q, want 127.0.0.1", cfg.BindHost)
			}
			if !tc.wantErr && cfg.SummaryModel != "gemini-3.5-flash-lite" {
				t.Errorf("SummaryModel = %q", cfg.SummaryModel)
			}
			if !tc.wantErr && cfg.GeminiModel != "gemini-3.8-live" {
				t.Errorf("GeminiModel = %q", cfg.GeminiModel)
			}
		})
	}
}

func TestBindHost(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("AI_PROVIDER", "gemini")
	for _, tc := range []struct {
		name    string
		host    string
		want    string
		wantErr bool
	}{
		{name: "local default", want: "127.0.0.1"},
		{name: "container", host: "0.0.0.0", want: "0.0.0.0"},
		{name: "ipv6", host: "::1", want: "::1"},
		{name: "invalid", host: "not-an-ip", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CALLIO_BIND_HOST", tc.host)
			cfg, err := config.Load()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Load() error = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && cfg.BindHost != tc.want {
				t.Errorf("BindHost = %q, want %q", cfg.BindHost, tc.want)
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

func TestSummaryModelOverride(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("AI_PROVIDER", "gemini")
	t.Setenv("GEMINI_SUMMARY_MODEL", "gemini-3.5-flash-lite-lite")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SummaryModel != "gemini-3.5-flash-lite-lite" {
		t.Fatalf("wrong summary model: %q", cfg.SummaryModel)
	}
}
