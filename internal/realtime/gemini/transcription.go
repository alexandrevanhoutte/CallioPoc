package gemini

import (
	"fmt"
	"strings"
	"time"

	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
	"google.golang.org/genai"
)

// transcriptionState receives independent, incremental text for both speakers.
// Only the Gemini receiver goroutine accesses this state.
type transcriptionState struct {
	caller transcriptStream
	ai     transcriptStream
}

type transcriptStream struct {
	sequence uint64
	current  *realtime.TranscriptSegment
}

func (s *transcriptionState) update(speaker realtime.Speaker, value *genai.Transcription) (realtime.Event, bool) {
	if value == nil {
		return realtime.Event{}, false
	}
	stream := &s.caller
	prefix := "caller"
	if speaker == realtime.SpeakerAI {
		stream = &s.ai
		prefix = "ai"
	}
	if stream.current == nil {
		if strings.TrimSpace(value.Text) == "" {
			return realtime.Event{}, false
		}
		stream.sequence++
		stream.current = &realtime.TranscriptSegment{
			ID: fmt.Sprintf("%s-%d", prefix, stream.sequence),
			Speaker: speaker,
			Timestamp: time.Now().UTC(),
		}
	}
	current := stream.current
	current.Text = appendTranscriptText(current.Text, value.Text)
	current.Final = value.Finished
	segment := *current
	if segment.Final {
		stream.current = nil
	}
	return realtime.Event{Type: realtime.EventTranscript, Transcript: &segment}, true
}

func (s *transcriptionState) interruptAI() {
	// Interrupted assistant utterances should not be finalized automatically.
	s.ai.current = nil
}

// Gemini typically sends deltas. Also support retransmitted cumulative prefixes.
func appendTranscriptText(current, incoming string) string {
	if incoming == "" {
		return current
	}
	if strings.HasPrefix(incoming, current) {
		return incoming
	}
	if strings.HasSuffix(current, incoming) {
		return current
	}
	return current + incoming
}
