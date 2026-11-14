package realtime_test

import (
	"strings"
	"testing"

	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
)

func TestGreetingLocale(t *testing.T) {
	for _, tc := range []struct {
		value string
		greeting string
	}{
		{"fr-FR", "Bonjour"},
		{"en-US", "Hello"},
		{"ko-KR", "안녕하세요"},
	} {
		locale, err := realtime.NewGreetingLocale(tc.value)
		if err != nil { t.Fatal(err) }
		if locale.GreetingText() == "" || !strings.Contains(locale.GreetingText(), tc.greeting) {
			t.Fatalf("missing %s greeting", tc.value)
		}
		prompt := locale.OpeningPrompt()
		if !strings.Contains(prompt, locale.GreetingText()) || !strings.Contains(prompt, "caller’s language") && !strings.Contains(prompt, "caller's language") {
			t.Fatalf("opening prompt does not preserve greeting and language switching: %s", prompt)
		}
	}
	for _, input := range []string{"", "fr", "ko", "en", "de-DE", "KO-KR"} {
		if _, err := realtime.NewGreetingLocale(input); err == nil {
			t.Fatalf("expected invalid locale %q", input)
		}
	}
}

func TestMultilingualInstructions(t *testing.T) {
	for _, clause := range []string{"request_handoff", "request_call_end", "switch with them immediately", "language the caller actually speaks", "noted", "caller wants to hang up"} {
		if !strings.Contains(realtime.ReceptionistInstructions, clause) {
			t.Fatalf("missing instruction: %q", clause)
		}
	}
	for _, outcome := range []string{"decline", "no_answer"} {
		prompt, err := realtime.HandoffOutcomePrompt(outcome)
		if err != nil || !strings.Contains(prompt, "caller's language") { t.Fatalf("outcome %s: %v", outcome, err) }
	}
	if _, err := realtime.HandoffOutcomePrompt("invalid"); err == nil { t.Fatal("invalid outcome accepted") }
}
