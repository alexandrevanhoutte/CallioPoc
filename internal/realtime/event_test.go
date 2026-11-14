package realtime_test

import (
	"testing"

	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
)

func TestEventType(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		text string
		want realtime.EventType
	}{
		{"audio", realtime.EventAudio},
		{"transcript", realtime.EventTranscript},
		{"turn_end", realtime.EventTurnEnd},
		{"interrupted", realtime.EventInterrupted},
	} {
		got, err := realtime.NewEventType(tc.text)
		if err != nil {
			t.Fatalf("NewEventType(%q): %v", tc.text, err)
		}
		if got != tc.want || got.String() != tc.text {
			t.Errorf("round trip %q: type=%v, string=%q", tc.text, got, got.String())
		}
	}
	if realtime.EventAudio != 1 || realtime.EventTranscript != 2 || realtime.EventTurnEnd != 3 || realtime.EventInterrupted != 4 {
		t.Fatal("event constants must start at iota + 1")
	}
	if _, err := realtime.NewEventType("unexpected"); err == nil {
		t.Fatal("unknown types must fail")
	}
	if _, err := realtime.NewEventType(""); err == nil {
		t.Fatal("empty type must fail")
	}
	if (realtime.EventType(0)).String() != "unknown" {
		t.Fatal("zero value must be unknown")
	}
}
