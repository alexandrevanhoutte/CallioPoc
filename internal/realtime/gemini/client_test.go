package gemini

import (
	"bytes"
	"strings"
	"testing"

	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
	"google.golang.org/genai"
)

func TestLiveConfig(t *testing.T) {
	cfg := liveConfig()
	if len(cfg.ResponseModalities) != 1 || cfg.ResponseModalities[0] != genai.ModalityAudio {
		t.Fatal("live session must respond with audio")
	}
	if cfg.SystemInstruction == nil || len(cfg.SystemInstruction.Parts) == 0 ||
		!strings.Contains(cfg.SystemInstruction.Parts[0].Text, "français") {
		t.Fatal("French receptionist system instructions missing")
	}
	if !strings.Contains(openingPrompt, "appel entrant") {
		t.Fatal("initial greeting prompt missing")
	}

	if cfg.RealtimeInputConfig == nil {
		t.Fatal("realtime input config missing")
	}
	vad := cfg.RealtimeInputConfig.AutomaticActivityDetection
	if vad == nil || vad.Disabled {
		t.Fatal("automatic voice activity detection should be enabled")
	}
	if vad.SilenceDurationMs == nil || *vad.SilenceDurationMs != 500 {
		t.Fatal("end-of-speech silence must be 500ms")
	}
}

func audioContent(size int) *genai.LiveServerContent {
	return &genai.LiveServerContent{
		ModelTurn: &genai.Content{
			Parts: []*genai.Part{{
				InlineData: &genai.Blob{Data: bytes.Repeat([]byte{7, 3}, size/2)},
			}},
		},
	}
}

func TestTurnLifecycle(t *testing.T) {
	s := &session{}
	if err := s.queueContent(audioContent(1920)); err != nil {
		t.Fatal(err)
	}
	if len(s.pending) != 2 || s.pending[0].TurnID != 1 || s.pending[0].Type != realtime.EventAudio {
		t.Fatalf("first turn was not queued correctly: %+v", s.pending)
	}
	if err := s.queueContent(&genai.LiveServerContent{TurnComplete: true}); err != nil {
		t.Fatal(err)
	}
	if len(s.pending) != 3 || s.pending[2].Type != realtime.EventTurnEnd || s.pending[2].TurnID != 1 {
		t.Fatal("expected completion of turn 1")
	}
	s.pending = nil
	if err := s.queueContent(audioContent(960)); err != nil {
		t.Fatal(err)
	}
	if len(s.pending) != 1 || s.pending[0].TurnID != 2 {
		t.Fatal("new model audio should start turn 2")
	}
}

func TestInterruptionClearsStaleAudioAndStaysConnected(t *testing.T) {
	s := &session{}
	if err := s.queueContent(audioContent(4800)); err != nil {
		t.Fatal(err)
	}
	if len(s.pending) != 5 {
		t.Fatalf("expected 5 queued chunks, got %d", len(s.pending))
	}
	if err := s.queueContent(&genai.LiveServerContent{Interrupted: true}); err != nil {
		t.Fatal(err)
	}
	if len(s.pending) != 1 || s.pending[0].Type != realtime.EventInterrupted || s.pending[0].TurnID != 1 {
		t.Fatalf("stale audio not cleared: %+v", s.pending)
	}
	s.pending = nil
	// Gemini can send TurnComplete after Interrupted for the cancelled turn.
	if err := s.queueContent(&genai.LiveServerContent{TurnComplete: true}); err != nil {
		t.Fatal(err)
	}
	if len(s.pending) != 0 {
		t.Fatal("interrupted turn completion must be ignored")
	}
	if err := s.queueContent(audioContent(960)); err != nil {
		t.Fatal(err)
	}
	if len(s.pending) != 1 || s.pending[0].TurnID != 2 {
		t.Fatal("next model response must use a fresh turn ID")
	}
}

func TestRepeatedInterruptions(t *testing.T) {
	s := &session{}
	for i := uint64(1); i <= 5; i++ {
		if err := s.queueContent(audioContent(1920)); err != nil {
			t.Fatal(err)
		}
		if err := s.queueContent(&genai.LiveServerContent{Interrupted: true}); err != nil {
			t.Fatal(err)
		}
		if len(s.pending) != 1 || s.pending[0].TurnID != i || s.pending[0].Type != realtime.EventInterrupted {
			t.Fatalf("interruption %d failed: %+v", i, s.pending)
		}
		s.pending = nil
		if err := s.queueContent(&genai.LiveServerContent{TurnComplete: true}); err != nil {
			t.Fatal(err)
		}
		if len(s.pending) != 0 {
			t.Fatalf("unexpected stale completion after interruption %d", i)
		}
	}
}

func TestOddPCM16OutputRejected(t *testing.T) {
	s := &session{}
	err := s.queueContent(&genai.LiveServerContent{
		ModelTurn: &genai.Content{Parts: []*genai.Part{{InlineData: &genai.Blob{Data: []byte{1}}}}},
	})
	if err == nil {
		t.Fatal("expected invalid PCM16 output error")
	}
}
