package httpapi

import (
    "encoding/json"
    "net/http"
)

// capabilities shares only public readiness flags. In particular, no API keys,
// model credentials or server environment variables reach the browser.
func capabilities(liveConfigured bool) http.HandlerFunc {
    return func(w http.ResponseWriter, _ *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(struct {
            Status string `json:"status"`
            LiveConfigured bool `json:"live_configured"`
        }{Status: "ok", LiveConfigured: liveConfigured})
    }
}
