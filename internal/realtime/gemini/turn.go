package gemini

import (
	"errors"

	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
	"google.golang.org/genai"
)

const (
	outputChunkBytes = 960 // PCM16 mono 24 kHz, 20 ms
	outputSampleRate = 24000
)

// turnState converts ordered Gemini output into provider-independent events.
// Only the receiver goroutine owns it; session controls the SDK connection.
type turnState struct {
	id      uint64
	active  bool
	pending     []realtime.Event
	transcripts transcriptionState
	history     realtime.Transcript
}

func (t *turnState) next() (realtime.Event, bool) {
	if len(t.pending) == 0 {
		return realtime.Event{}, false
	}
	event := t.pending[0]
	t.pending[0] = realtime.Event{}
	t.pending = t.pending[1:]
	return event, true
}

// process clears buffered speech on interruption. Gemini may send a final
// TurnComplete for an interrupted turn, which must not end the next turn.
func (t *turnState) process(content *genai.LiveServerContent) error {
	if content == nil {
		return nil
	}
	if content.Interrupted {
		t.pending = []realtime.Event{{
			Type: realtime.EventInterrupted, TurnID: t.id,
		}}
		t.active = false
		t.transcripts.interruptAI()
	}
	for _, item := range []struct {
		speaker realtime.Speaker
		value   *genai.Transcription
	}{
		{realtime.SpeakerCaller, content.InputTranscription},
		{realtime.SpeakerAI, content.OutputTranscription},
	} {
		if event, ok := t.transcripts.update(item.speaker, item.value); ok {
			t.history.Apply(*event.Transcript)
			t.pending = append(t.pending, event)
		}
	}
	if content.Interrupted {
		return nil
	}

	if content.ModelTurn != nil {
		for _, part := range content.ModelTurn.Parts {
			if part == nil || part.InlineData == nil || len(part.InlineData.Data) == 0 {
				continue
			}
			audio := part.InlineData.Data
			if len(audio)%2 != 0 {
				return errors.New("invalid Gemini PCM16 output length")
			}
			if !t.active {
				t.id++
				t.active = true
			}
			for offset := 0; offset < len(audio); offset += outputChunkBytes {
				end := min(offset+outputChunkBytes, len(audio))
				t.pending = append(t.pending, realtime.Event{
					Type:       realtime.EventAudio,
					TurnID:     t.id,
					Audio:      append([]byte(nil), audio[offset:end]...),
					SampleRate: outputSampleRate,
				})
			}
		}
	}

	if content.TurnComplete && t.active {
		t.pending = append(t.pending, realtime.Event{
			Type:   realtime.EventTurnEnd,
			TurnID: t.id,
		})
		t.active = false
	}
	return nil
}
