package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
)

// capabilities shares only public readiness flags. No credentials are returned.
func capabilities(liveConfigured bool, defaultLocale realtime.Locale) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Status         string `json:"status"`
			LiveConfigured bool   `json:"live_configured"`
			GreetingLocale string `json:"greeting_locale"`
		}{
			Status:         "ok",
			LiveConfigured: liveConfigured,
			GreetingLocale: defaultLocale.String(),
		})
	}
}
