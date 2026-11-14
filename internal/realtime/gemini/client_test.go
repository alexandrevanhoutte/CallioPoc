package gemini

import "testing"

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
