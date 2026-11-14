package gemini

import (
	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
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

func TestMultilingualGeminiConfig(t *testing.T) {
	cfg := liveConfig()
	if len(cfg.Tools) != 2 || cfg.Tools[0].FunctionDeclarations[0].Name != handoffToolName ||
		cfg.Tools[1].FunctionDeclarations[0].Name != endCallToolName {
		t.Fatal("both Gemini functions must be registered")
	}
	if !strings.Contains(cfg.SystemInstruction.Parts[0].Text, "switch with them immediately") {
		t.Fatal("Gemini must follow the caller's language")
	}
	if !strings.Contains(cfg.Tools[0].FunctionDeclarations[0].Description, "BEFORE verbally confirming") ||
		!strings.Contains(cfg.Tools[1].FunctionDeclarations[0].Description, "caller’s language") {
		t.Fatal("Gemini tools need English descriptions")
	}
	for _, locale := range []realtime.Locale{realtime.LocaleFrench, realtime.LocaleEnglish, realtime.LocaleKorean} {
		if !strings.Contains(realtime.OpeningPrompt(locale), realtime.GreetingText(locale)) { t.Fatalf("missing greeting: %s", locale) }
	}
}
