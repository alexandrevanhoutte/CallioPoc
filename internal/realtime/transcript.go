package realtime

import "time"

// Speaker is intentionally provider-independent.
type Speaker string

const (
	SpeakerCaller Speaker = "CALLER"
	SpeakerAI     Speaker = "AI"
)

// TranscriptSegment is a full snapshot of a single utterance. Updates with
// the same ID replace the previous partial text rather than duplicating it.
type TranscriptSegment struct {
	ID        string    `json:"segment_id"`
	Speaker   Speaker   `json:"speaker"`
	Text      string    `json:"text"`
	Final     bool      `json:"final"`
	Timestamp time.Time `json:"timestamp"`
}

// Transcript is a per-call, in-memory collector of structured segments.
// It is owned by the one receive goroutine; CI-09/10 can consume Finalized().
type Transcript struct {
	segments []TranscriptSegment
	index    map[string]int
}

// Apply accepts an update to an in-progress segment, or a new segment.
// Finalized segments are immutable; duplicated final updates are harmless.
func (t *Transcript) Apply(segment TranscriptSegment) bool {
	if segment.ID == "" || segment.Text == "" ||
		(segment.Speaker != SpeakerCaller && segment.Speaker != SpeakerAI) {
		return false
	}
	if i, ok := t.index[segment.ID]; ok {
		old := t.segments[i]
		if old.Final || old.Speaker != segment.Speaker {
			return false
		}
		segment.Timestamp = old.Timestamp
		t.segments[i] = segment
		return true
	}
	if t.index == nil {
		t.index = make(map[string]int)
	}
	if segment.Timestamp.IsZero() {
		segment.Timestamp = time.Now().UTC()
	}
	t.index[segment.ID] = len(t.segments)
	t.segments = append(t.segments, segment)
	return true
}

func (t *Transcript) Snapshot() []TranscriptSegment {
	return append([]TranscriptSegment(nil), t.segments...)
}

func (t *Transcript) Finalized() []TranscriptSegment {
	var result []TranscriptSegment
	for _, segment := range t.segments {
		if segment.Final {
			result = append(result, segment)
		}
	}
	return result
}
