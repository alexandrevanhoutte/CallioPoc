package gemini

import (
	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
"github.com/alexandrevanhoutte/CallioPoc/internal/conversation"
"github.com/alexandrevanhoutte/CallioPoc/internal/logging"
"google.golang.org/genai"
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
	if len(cfg.Tools) != 4 || cfg.Tools[0].FunctionDeclarations[0].Name != handoffToolName ||
		cfg.Tools[1].FunctionDeclarations[0].Name != endCallToolName ||
		cfg.Tools[2].FunctionDeclarations[0].Name != setCallContextName ||
		cfg.Tools[3].FunctionDeclarations[0].Name != recordMessageName {
		t.Fatal("both Gemini functions must be registered")
	}
	if !strings.Contains(cfg.SystemInstruction.Parts[0].Text, "switch with them immediately") {
		t.Fatal("Gemini must follow the caller's language")
	}
	if !strings.Contains(cfg.Tools[0].FunctionDeclarations[0].Description, "wait silently") ||
		!strings.Contains(cfg.Tools[1].FunctionDeclarations[0].Description, "caller’s language") {
		t.Fatal("Gemini tools need English descriptions")
	}
	for _, locale := range []realtime.Locale{realtime.LocaleFrench, realtime.LocaleEnglish, realtime.LocaleKorean} {
		if !strings.Contains(realtime.OpeningPrompt(locale), realtime.GreetingText(locale)) { t.Fatalf("missing greeting: %s", locale) }
	}
}

func TestToolBehaviorsAreExplicit(t *testing.T) {
  expected := []genai.Behavior{genai.BehaviorNonBlocking, genai.BehaviorBlocking, genai.BehaviorBlocking, genai.BehaviorBlocking}
  for i, tool := range geminiTools() {
    if actual := tool.FunctionDeclarations[0].Behavior; actual != expected[i] {
      t.Errorf("tool %d behavior = %q, want %q", i, actual, expected[i])
    }
  }
}

func newToolTestSession(t *testing.T, category string) *session {
  t.Helper()
  state := conversation.NewState(conversation.Default())
  var fields []conversation.FieldUpdate
  switch category {
  case "recruiting":
    fields = []conversation.FieldUpdate{{Key: "caller_name", Value: "Nora"}, {Key: "position", Value: "Backend Engineer"}}
  case "sales":
    fields = []conversation.FieldUpdate{{Key: "caller_name", Value: "Nora"}, {Key: "offer_description", Value: "Shoes"}}
  }
  if _, err := state.Update(category, fields, nil); err != nil { t.Fatal(err) }
  return &session{context: state, l: logging.For("gemini.test")}
}

func callTool(name, id string) *genai.FunctionCall {
  args := map[string]any{}
  switch name {
  case handoffToolName:
    args = map[string]any{"reason": "owner_requested", "caller_name": "Nora", "purpose": "Recruitment", "summary": "Interview for Backend Engineer"}
  case recordMessageName:
    args = map[string]any{"purpose": "Recruitment", "summary": "Caller requests a follow-up"}
  }
  return &genai.FunctionCall{ID: id, Name: name, Args: args}
}

func TestHandoffResponseIsSilentAndNeverRecordsNote(t *testing.T) {
  s := newToolTestSession(t, "recruiting")
  responses, err := s.buildToolResponses(&genai.LiveServerToolCall{FunctionCalls: []*genai.FunctionCall{
    callTool(handoffToolName, "handoff"),
  }})
  if err != nil || len(responses) != 1 { t.Fatalf("responses=%v err=%v", responses, err) }
  output, _ := responses[0].Response["output"].(string)
  if !s.handoffPending.Load() || s.messageRecorded ||
    responses[0].Scheduling != genai.FunctionResponseSchedulingSilent ||
    !strings.Contains(output, "no message recorded") ||
    len(s.turns.pending) != 1 || s.turns.pending[0].Type != realtime.EventHandoffRequired {
      t.Fatalf("handoff must be silent and pending without recording note: %+v", responses[0])
  }
  next, err := s.buildToolResponses(&genai.LiveServerToolCall{FunctionCalls: []*genai.FunctionCall{
    callTool(recordMessageName, "note"),
    callTool(endCallToolName, "end"),
    callTool(handoffToolName, "again"),
  }})
  if err != nil || len(next) != 3 { t.Fatalf("responses=%v err=%v", next, err) }
  for _, response := range next {
    if response.Response["error"] == nil { t.Errorf("%s succeeded during pending handoff", response.Name) }
  }
  if s.messageRecorded { t.Fatal("messageRecorded after rejected note") }
}

func TestHandoffAndRecordMessageCannotSucceedInSameBatch(t *testing.T) {
  for _, first := range []string{handoffToolName, recordMessageName} {
    t.Run(first, func(t *testing.T) {
      s := newToolTestSession(t, "recruiting")
      second := recordMessageName
      if first == recordMessageName { second = handoffToolName }
      calls := []*genai.FunctionCall{callTool(first, "one"), callTool(second, "two")}
      responses, err := s.buildToolResponses(&genai.LiveServerToolCall{FunctionCalls: calls})
      if err != nil || len(responses) != 2 { t.Fatalf("bad responses: %v, %v", responses, err) }
      for _, response := range responses {
        if response.Name == recordMessageName && response.Response["error"] == nil {
          t.Fatal("note succeeded in a handoff batch")
        }
      }
      if !s.handoffPending.Load() || s.messageRecorded { t.Fatal("handoff batch must not record note") }
    })
  }
}

func TestSalesNoteStillRecordsWithoutHandoff(t *testing.T) {
  s := newToolTestSession(t, "sales")
  responses, err := s.buildToolResponses(&genai.LiveServerToolCall{FunctionCalls: []*genai.FunctionCall{
    {ID: "note", Name: recordMessageName, Args: map[string]any{"purpose": "Shoes", "summary": "Offer for shoes"}},
  }})
  if err != nil || len(responses) != 1 || responses[0].Response["error"] != nil || !s.messageRecorded {
    t.Fatalf("sales note must still work: %+v err=%v", responses, err)
  }
}
