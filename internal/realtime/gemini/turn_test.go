package gemini

import (
	"testing"

	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
	"google.golang.org/genai"
)

func audioContent(size int) *genai.LiveServerContent {
	return &genai.LiveServerContent{ModelTurn: &genai.Content{
		Parts: []*genai.Part{{InlineData: &genai.Blob{Data: make([]byte, size)}}},
	}}
}

func TestTurnState(t *testing.T) {
	t.Run("complete turn and start another", func(t *testing.T) {
		var state turnState
		if err := state.process(audioContent(1920)); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			event, ok := state.next()
			if !ok || event.Type != realtime.EventAudio || event.TurnID != 1 || len(event.Audio) != 960 {
				t.Fatalf("audio chunk %d: %+v (ok=%v)", i, event, ok)
			}
		}
		if err := state.process(&genai.LiveServerContent{TurnComplete: true}); err != nil {
			t.Fatal(err)
		}
		end, ok := state.next()
		if !ok || end.Type != realtime.EventTurnEnd || end.TurnID != 1 {
			t.Fatalf("turn completion: %+v", end)
		}
		if err := state.process(audioContent(960)); err != nil {
			t.Fatal(err)
		}
		event, _ := state.next()
		if event.TurnID != 2 || event.Type != realtime.EventAudio {
			t.Fatalf("next turn: %+v", event)
		}
	})

	t.Run("repeated interruption drops old audio and late completion", func(t *testing.T) {
		var state turnState
		for turn := uint64(1); turn <= 2; turn++ {
			if err := state.process(audioContent(2880)); err != nil {
				t.Fatal(err)
			}
			if err := state.process(&genai.LiveServerContent{Interrupted: true}); err != nil {
				t.Fatal(err)
			}
			event, ok := state.next()
			if !ok || event.Type != realtime.EventInterrupted || event.TurnID != turn {
				t.Fatalf("interruption %d: %+v", turn, event)
			}
			if _, ok := state.next(); ok {
				t.Fatal("stale audio remained after interruption")
			}
			if err := state.process(&genai.LiveServerContent{TurnComplete: true}); err != nil {
				t.Fatal(err)
			}
			if _, ok := state.next(); ok {
				t.Fatal("late completion of interrupted turn was forwarded")
			}
		}
		if err := state.process(audioContent(960)); err != nil {
			t.Fatal(err)
		}
		event, _ := state.next()
		if event.Type != realtime.EventAudio || event.TurnID != 3 {
			t.Fatalf("fresh response after interrupts: %+v", event)
		}
	})

	t.Run("invalid audio rejected", func(t *testing.T) {
		var state turnState
		if err := state.process(audioContent(1)); err == nil {
			t.Fatal("odd PCM16 payload must be rejected")
		}
	})
}

func TestAlternatingTranscriptsWithoutFinishedFlag(t *testing.T) {
	var state turnState

	// Gemini may omit Finished on every transcription update. Each
	// complete assistant turn must still get a different segment ID.
	messages := []*genai.LiveServerContent{
		{OutputTranscription: &genai.Transcription{Text: "Bonjour."}},
		{TurnComplete: true},
		{InputTranscription: &genai.Transcription{Text: "Je voudrais"}},
		{InputTranscription: &genai.Transcription{Text: " un entretien."}},
		{OutputTranscription: &genai.Transcription{Text: "Avec qui ?"}},
		{TurnComplete: true},
		{InputTranscription: &genai.Transcription{Text: "Alexandre."}},
		{OutputTranscription: &genai.Transcription{Text: "Je vous remercie."}},
		{TurnComplete: true},
	}
	for _, message := range messages {
		if err := state.process(message); err != nil {
			t.Fatal(err)
		}
	}
	wantSpeakers := []realtime.Speaker{
		realtime.SpeakerAI, realtime.SpeakerCaller,
		realtime.SpeakerAI, realtime.SpeakerCaller, realtime.SpeakerAI,
	}
	got := state.history.Finalized()
	if len(got) != len(wantSpeakers) {
		t.Fatalf("expected %d alternating transcript bubbles, got %d: %+v", len(wantSpeakers), len(got), got)
	}
	for i, speaker := range wantSpeakers {
		if got[i].Speaker != speaker {
			t.Fatalf("bubble %d: got %s, want %s", i, got[i].Speaker, speaker)
		}
		if i > 0 && got[i].ID == got[i-1].ID {
			t.Fatalf("bubbles %d and %d share an ID", i-1, i)
		}
	}
	if got[1].Text != "Je voudrais un entretien." {
		t.Fatalf("caller chunks not combined properly: %q", got[1].Text)
	}
}

func TestLateCallerTranscriptKeepsLogicalOrder(t *testing.T) {
	var state turnState
	for _, message := range []*genai.LiveServerContent{
		{OutputTranscription: &genai.Transcription{Text: "Bonjour."}},
		{GenerationComplete: true},
		// An AI response may arrive before the input transcription event.
		{OutputTranscription: &genai.Transcription{Text: "Quel entretien ?"}},
		{InputTranscription: &genai.Transcription{Text: "Je voudrais un entretien."}},
		{TurnComplete: true},
	} {
		if err := state.process(message); err != nil {
			t.Fatal(err)
		}
	}
	got := state.history.Snapshot()
	if len(got) != 3 {
		t.Fatalf("expected 3 turns, got %+v", got)
	}
	if got[0].Speaker != realtime.SpeakerAI || got[0].Order != 1 ||
		got[1].Speaker != realtime.SpeakerCaller || got[1].Order != 2 ||
		got[2].Speaker != realtime.SpeakerAI || got[2].Order != 3 {
		t.Fatalf("transcript ordering incorrect: %+v", got)
	}
}
