package realtime_test

import (
	"bytes"
	"encoding/json"
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

func TestSpeakerRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		value   string
		speaker realtime.Speaker
	}{
		{"CALLER", realtime.SpeakerCaller},
		{"AI", realtime.SpeakerAI},
	} {
		got, err := realtime.NewSpeaker(tc.value)
		if err != nil || got != tc.speaker || got.String() != tc.value {
			t.Fatalf("NewSpeaker(%q) = %v, %v", tc.value, got, err)
		}
		data, err := json.Marshal(got)
		if err != nil || string(data) != `"`+tc.value+`"` {
			t.Fatalf("MarshalJSON(%q) = %s, %v", tc.value, data, err)
		}
		var decoded realtime.Speaker
		if err := json.Unmarshal(data, &decoded); err != nil || decoded != got {
			t.Fatalf("UnmarshalJSON(%q) = %v, %v", data, decoded, err)
		}
	}
}

func TestSpeakerRejectsInvalidValues(t *testing.T) {
	if realtime.SpeakerCaller != 1 || realtime.SpeakerAI != 2 {
		t.Fatal("Speaker enum must start at iota + 1")
	}
	if realtime.Speaker(0).String() != "unknown" {
		t.Fatal("zero value must be invalid")
	}
	for _, value := range []string{"", "caller", "UNKNOWN"} {
		if _, err := realtime.NewSpeaker(value); err == nil {
			t.Fatalf("NewSpeaker(%q) should fail", value)
		}
	}
	for _, data := range []string{`"UNKNOWN"`, `"caller"`, `null`, `1`} {
		var speaker realtime.Speaker
		if err := json.Unmarshal([]byte(data), &speaker); err == nil {
			t.Fatalf("UnmarshalJSON(%s) should fail", data)
		}
	}
	if _, err := json.Marshal(realtime.Speaker(0)); err == nil {
		t.Fatal("invalid speaker should not marshal")
	}
}

func TestTranscriptSpeakerJSONCompatibility(t *testing.T) {
	segment := realtime.TranscriptSegment{
		ID:      "speaker-1",
		Speaker: realtime.SpeakerCaller,
		Text:    "Bonjour",
	}
	data, err := json.Marshal(segment)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"speaker":"CALLER"`)) {
		t.Fatalf("speaker JSON contract changed: %s", data)
	}
	var decoded realtime.TranscriptSegment
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Speaker != realtime.SpeakerCaller {
		t.Fatalf("decoded speaker: %v", decoded.Speaker)
	}
}
