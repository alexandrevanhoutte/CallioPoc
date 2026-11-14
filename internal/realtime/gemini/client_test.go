package gemini

import "testing"

// Guard the latency setting that materially affects conversation flow.
func TestEndOfSpeechTimeout(t *testing.T) {
	vad := liveConfig().RealtimeInputConfig.AutomaticActivityDetection
	if vad == nil || vad.Disabled || vad.SilenceDurationMs == nil || *vad.SilenceDurationMs != 500 {
		t.Fatal("Gemini automatic end-of-speech timeout must be 500 ms")
	}
}
