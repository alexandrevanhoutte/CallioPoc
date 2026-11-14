package realtime_test

import (
	"testing"
	"time"

	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
)

func TestTranscriptUpdates(t *testing.T) {
	var history realtime.Transcript
	at := time.Date(2026, time.November, 14, 12, 0, 0, 0, time.UTC)
	first := realtime.TranscriptSegment{ID: "caller-1", Speaker: realtime.SpeakerCaller, Text: "Bon", Timestamp: at}
	if !history.Apply(first) {
		t.Fatal("first partial update must be accepted")
	}
	first.Text, first.Final = "Bonjour", true
	if !history.Apply(first) {
		t.Fatal("final update should replace the same segment")
	}
	if history.Apply(first) {
		t.Fatal("duplicate finalized segment must be ignored")
	}
	if !history.Apply(realtime.TranscriptSegment{ID: "ai-1", Speaker: realtime.SpeakerAI, Text: "Comment puis-je vous aider ?"}) {
		t.Fatal("AI segment should be accepted")
	}
	if got := history.Snapshot(); len(got) != 2 || got[0].Text != "Bonjour" || !got[0].Timestamp.Equal(at) {
		t.Fatalf("unexpected snapshot: %+v", got)
	}
	if got := history.Finalized(); len(got) != 1 || got[0].Speaker != realtime.SpeakerCaller {
		t.Fatalf("only finalized speech should be consumable: %+v", got)
	}
	if history.Apply(realtime.TranscriptSegment{ID: "", Speaker: realtime.SpeakerCaller, Text: "invalid"}) {
		t.Fatal("empty segment ID must be rejected")
	}
}
