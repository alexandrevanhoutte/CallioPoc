package call_test

import (
	"testing"

	"github.com/alexandrevanhoutte/CallioPoc/internal/call"
)

func TestSummaryValidate(t *testing.T) {
	name := "Marc"
	good := call.Summary{
		CallerName: &name,
		Urgency: "unknown",
		Text: "Le correspondant a demandé des informations.",
		FollowUpActions: []string{},
	}
	if err := good.Validate(); err != nil { t.Fatal(err) }
	for _, tc := range []struct {
		name string
		mutate func(*call.Summary)
	}{
		{"empty text", func(s *call.Summary) { s.Text = "" }},
		{"invalid urgency", func(s *call.Summary) { s.Urgency = "critical" }},
		{"empty name", func(s *call.Summary) { v := ""; s.CallerName = &v }},
		{"empty reason", func(s *call.Summary) { v := ""; s.Reason = &v }},
		{"empty action", func(s *call.Summary) { s.FollowUpActions = []string{""} }},
		{"too many actions", func(s *call.Summary) { s.FollowUpActions = []string{"a","b","c","d","e","f"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := good
			tc.mutate(&s)
			if err := s.Validate(); err == nil { t.Fatal("expected invalid summary") }
		})
	}
}

func TestSummaryWhitespaceIsNotEmpty(t *testing.T) {
	value := " "
	s := call.Summary{
		CallerName: &value,
		Reason: &value,
		Urgency: "unknown",
		Text: value,
		FollowUpActions: []string{value},
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("whitespace-only values should pass length checks: %v", err)
	}
}
