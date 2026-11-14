package gemini

import (
	"fmt"
	"time"

	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
	"google.golang.org/genai"
)

// Gemini sends input transcription independently of output. Reserve caller
// positions so delayed caller text can precede the assistant's next reply.
type transcriptionState struct {
	caller transcriptStream
	ai     transcriptStream

	nextOrder  uint64
	callerSlot uint64
	aiOrder    uint64
	aiActive   bool
}

type transcriptStream struct {
	sequence uint64
	current  *realtime.TranscriptSegment
}

func (s *transcriptionState) beginAI() {
	if s.aiActive {
		return
	}
	s.aiActive = true
	s.nextOrder++
	s.aiOrder = s.nextOrder
}

func (s *transcriptionState) endAI() (realtime.Event, bool) {
	event, ok := s.finish(realtime.SpeakerAI)
	if s.aiActive {
		s.aiActive = false
		s.nextOrder++
		s.callerSlot = s.nextOrder
	}
	return event, ok
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
		if len(value.Text) == 0 {
			return realtime.Event{}, false
		}
		stream.sequence++
		var order uint64
		if speaker == realtime.SpeakerAI {
			s.beginAI()
			order = s.aiOrder
		} else if s.callerSlot != 0 {
			order = s.callerSlot
			s.callerSlot = 0
		} else {
			s.nextOrder++
			order = s.nextOrder
		}
		stream.current = &realtime.TranscriptSegment{
			ID: fmt.Sprintf("%s-%d", prefix, stream.sequence),
			Order: order,
			Speaker: speaker,
			Timestamp: time.Now().UTC(),
		}
	}
	current := stream.current
	// Treat Gemini transcription text as a delta. Repeated words may be
	// intentional, so don't infer snapshot updates from common prefixes.
	current.Text += value.Text
	current.Final = value.Finished
	segment := *current
	if segment.Final {
		stream.current = nil
	}
	return realtime.Event{Type: realtime.EventTranscript, Transcript: &segment}, true
}

func (s *transcriptionState) finish(speaker realtime.Speaker) (realtime.Event, bool) {
	stream := &s.caller
	if speaker == realtime.SpeakerAI {
		stream = &s.ai
	}
	if stream.current == nil {
		return realtime.Event{}, false
	}
	segment := *stream.current
	segment.Final = true
	stream.current = nil
	return realtime.Event{Type: realtime.EventTranscript, Transcript: &segment}, true
}

func (s *transcriptionState) interruptAI() {
	// Keep already emitted partial text, but never finalize interrupted
	// assistant speech as a completed statement.
	s.ai.current = nil
	if s.aiActive {
		s.aiActive = false
		s.nextOrder++
		s.callerSlot = s.nextOrder
	}
}
