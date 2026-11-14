package gemini

import (
	"strings"
	"testing"

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
