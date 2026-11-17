package gemini

import (
	"strings"
	"testing"

	"github.com/alexandrevanhoutte/CallioPoc/internal/conversation"
)

func TestParseCallContext(t *testing.T) {
	category, fields, unavailable, err := parseCallContext(map[string]any{
		"category":           "recruiting",
		"fields":             []any{map[string]any{"key": "caller_name", "value": "Thomas"}},
		"unavailable_fields": []any{"position"},
	})
	if err != nil || category != "recruiting" || len(fields) != 1 || fields[0].Value != "Thomas" || len(unavailable) != 1 {
		t.Fatalf("parseCallContext: category=%q fields=%+v unavailable=%v err=%v", category, fields, unavailable, err)
	}
	for _, bad := range []map[string]any{
		{"category": 42},
		{"category": "recruiting", "fields": "caller_name"},
		{"category": "recruiting", "fields": []any{map[string]any{"key": "name", "value": 3}}},
		{"category": "recruiting", "unavailable_fields": []any{42}},
	} {
		if _, _, _, err := parseCallContext(bad); err == nil {
			t.Fatalf("expected invalid context to fail: %+v", bad)
		}
	}
}

func TestCategoryToolsAndGuard(t *testing.T) {
	declaration := contextTool().FunctionDeclarations[0]
	if declaration.Name != setCallContextName || !strings.Contains(declaration.Description, "hesitation") {
		t.Fatal("context tool must distinguish an answer from a filler")
	}
	s := conversation.NewState(conversation.Default())
	if err := s.Allow(handoffToolName); err == nil {
		t.Fatal("cannot hand off before choosing a category")
	}
	if _, err := s.Update("recruiting", []conversation.FieldUpdate{{Key: "caller_name", Value: "Thomas"}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Allow(handoffToolName); err == nil || !strings.Contains(err.Error(), "position") {
		t.Fatalf("must not hand off without interview position: %v", err)
	}
	if _, err := s.Update("sales", []conversation.FieldUpdate{{Key: "offer_description", Value: "Shoes"}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Allow(handoffToolName); err == nil {
		t.Fatal("sales handoff unexpectedly permitted")
	}
}

func TestMessageAcknowledgementAndClosingGuidance(t *testing.T) {
	message := messageTool().FunctionDeclarations[0].Description
	for _, fragment := range []string{"After a successful tool response", "natural language", "do NOT automatically end the call", "not been sent"} {
		if !strings.Contains(message, fragment) {
			t.Fatalf("message tool missing conversation guidance %q", fragment)
		}
	}
	end := endCallTool().FunctionDeclarations[0].Description
	for _, fragment := range []string{"explicitly says goodbye", "A note being recorded", "subsequent reply"} {
		if !strings.Contains(end, fragment) {
			t.Fatalf("end call tool missing guidance %q", fragment)
		}
	}
	global := conversation.Default().Prompt()
	for _, fragment := range []string{"Do not end the call merely because a note was recorded", "After record_message succeeds", "Never contradict a successful tool acknowledgment", "has NOT been sent"} {
		if !strings.Contains(global, fragment) {
			t.Fatalf("general policy missing guidance %q", fragment)
		}
	}
}
