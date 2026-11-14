package httpapi

import (
	"testing"
	"time"

	"github.com/alexandrevanhoutte/CallioPoc/internal/call"
)

func TestIdleExpired(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct{
		name string
		last time.Time
		state call.State
		speaking bool
		want bool
	}{
		{"silent for 61 seconds", now.Add(-61*time.Second), call.AIActive, false, true},
		{"recent speech", now.Add(-20*time.Second), call.AIActive, false, false},
		{"AI still speaking", now.Add(-70*time.Second), call.AIActive, true, false},
		{"handoff pending", now.Add(-70*time.Second), call.HandoffRequired, false, false},
		{"human active", now.Add(-70*time.Second), call.HumanActive, false, false},
		{"completed", now.Add(-70*time.Second), call.Completed, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := idleExpired(now, tc.last, time.Minute, tc.state, tc.speaking); got != tc.want {
				t.Fatalf("idleExpired()=%t want %t", got, tc.want)
			}
		})
	}
}

func TestConfiguredIdleTimeout(t *testing.T) {
	t.Setenv("CALL_IDLE_TIMEOUT_SECONDS", "90")
	if got := configuredIdleTimeout(); got != 90*time.Second { t.Fatalf("got %s", got) }
	t.Setenv("CALL_IDLE_TIMEOUT_SECONDS", "1")
	if got := configuredIdleTimeout(); got != defaultIdleTimeout { t.Fatalf("invalid timeout got %s", got) }
}
