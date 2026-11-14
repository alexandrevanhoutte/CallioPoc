package gemini

import (
	"testing"

	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
)

func TestParseHandoff(t *testing.T) {
	request, err := parseHandoff(map[string]any{
		"reason": "owner_requested",
		"purpose": "Speak to owner",
		"summary": "Caller wants to discuss an interview.",
		"email": "caller@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if request.Reason != realtime.HandoffOwnerRequested || request.Context.CallerName != nil ||
		request.Context.PhoneNumber != nil || request.Context.Email == nil ||
		*request.Context.Email != "caller@example.com" {
		t.Fatalf("unexpected handoff: %+v", request)
	}
	for _, args := range []map[string]any{
		{"reason": "other", "purpose": "Purpose", "summary": "Summary"},
		{"reason": "owner_requested", "purpose": " ", "summary": "Summary"},
		{"reason": "owner_requested", "purpose": "Purpose"},
		{"reason": "owner_requested", "purpose": "Purpose", "summary": "Summary", "email": 5},
	} {
		if _, err := parseHandoff(args); err == nil {
			t.Fatalf("expected parse error for %+v", args)
		}
	}
}
