package config_test

import (
	"testing"

	"calliopoc/internal/config"
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
			cfg, err := config.Load()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Load() error = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && cfg.Port != tc.want {
				t.Errorf("Port = %q, want %q", cfg.Port, tc.want)
			}
		})
	}
}
