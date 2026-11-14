package gemini

import (
	"strings"
	"testing"
)

// Guard the latency setting that materially affects conversation flow.
func TestEndOfSpeechTimeout(t *testing.T) {
	cfg := liveConfig()
	if cfg.InputAudioTranscription == nil || cfg.OutputAudioTranscription == nil {
		t.Fatal("both caller and assistant transcription must be enabled")
	}
	vad := cfg.RealtimeInputConfig.AutomaticActivityDetection
	if vad == nil || vad.Disabled || vad.SilenceDurationMs == nil || *vad.SilenceDurationMs != 500 {
		t.Fatal("Gemini automatic end-of-speech timeout must be 500 ms")
	}
}

func TestHandoffInstructions(t *testing.T) {
	for _, required := range []string{
		"Demande une fois le nom",
		"Un refus ou une absence de réponse",
		"motif précis",
		"request_handoff",
		"AVANT d'annoncer",
	} {
		if !strings.Contains(frenchInstructions, required) {
			t.Fatalf("handoff prompt missing %q", required)
		}
	}
	config := liveConfig()
	if len(config.Tools) != 1 || len(config.Tools[0].FunctionDeclarations) != 1 {
		t.Fatal("expected one registered Gemini handoff tool")
	}
	decl := config.Tools[0].FunctionDeclarations[0]
	if decl.Name != handoffToolName || !strings.Contains(decl.Description, "tenté de demander le nom") {
		t.Fatal("handoff tool must document information-gathering requirement")
	}
}
