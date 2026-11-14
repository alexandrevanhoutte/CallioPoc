package gemini

import (
	"testing"

	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
	"google.golang.org/genai"
)

func TestTranscriptionState(t *testing.T) {
	var s transcriptionState
	next := func(speaker realtime.Speaker, text string, finished bool) realtime.TranscriptSegment {
		t.Helper()
		event, ok := s.update(speaker, &genai.Transcription{Text: text, Finished: finished})
		if !ok || event.Type != realtime.EventTranscript || event.Transcript == nil {
			t.Fatalf("transcription update failed: %+v", event)
		}
		return *event.Transcript
	}
	caller1 := next(realtime.SpeakerCaller, "Bon", false)
	ai1 := next(realtime.SpeakerAI, "Bonjour", false)
	callerFinal := next(realtime.SpeakerCaller, "Bonjour", true)
	if callerFinal.ID != caller1.ID || callerFinal.Text != "Bonjour" || !callerFinal.Final {
		t.Fatalf("caller final update: %+v", callerFinal)
	}
	aiFinal := next(realtime.SpeakerAI, " à vous", true)
	if aiFinal.ID != ai1.ID || aiFinal.Text != "Bonjour à vous" || !aiFinal.Final {
		t.Fatalf("AI final update: %+v", aiFinal)
	}
	caller2 := next(realtime.SpeakerCaller, "Merci", true)
	if caller2.ID == caller1.ID {
		t.Fatal("new caller utterance must have a fresh ID")
	}
	if _, ok := s.update(realtime.SpeakerCaller, nil); ok {
		t.Fatal("nil transcription must be ignored")
	}
}

func TestTranscriptionInterrupt(t *testing.T) {
	var s transcriptionState
	first, _ := s.update(realtime.SpeakerAI, &genai.Transcription{Text: "Je peux"})
	s.interruptAI()
	second, _ := s.update(realtime.SpeakerAI, &genai.Transcription{Text: "Bien sûr", Finished: true})
	if first.Transcript == nil || second.Transcript == nil ||
		first.Transcript.ID == second.Transcript.ID || first.Transcript.Final {
		t.Fatal("interruption must start a new assistant segment")
	}
}
