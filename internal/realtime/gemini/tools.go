package gemini

import (
	"fmt"
	"strings"

	"github.com/alexandrevanhoutte/CallioPoc/internal/conversation"
	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
	"google.golang.org/genai"
)

// geminiTools centralizes Gemini function declarations.
// Keep each handler separate so it can be tested independently.
func geminiTools() []*genai.Tool {
	return []*genai.Tool{handoffTool(), endCallTool(), contextTool(), messageTool()}
}

const handoffToolName = "request_handoff"

func handoffTool() *genai.Tool {
	return &genai.Tool{FunctionDeclarations: []*genai.FunctionDeclaration{{
		Name: handoffToolName,
		Behavior: genai.BehaviorNonBlocking,
		Description: "Request the owner\u0027s attention for a handoff. Do not claim a completed transfer unless separately acknowledged. If the caller's name is unknown, ask for it and wait for a subsequent caller turn BEFORE calling this function. Never call in the same assistant turn as the name question. A brief pause, silence or partial transcription does not count as a refusal or an answer. If the caller explicitly refuses to give a name in a later turn, omit caller_name. If already known, do not ask again. Clarify the specific purpose and never invent contact details. First call set_call_context with the caller's facts and wait for its response to confirm the category and required fields; the Go backend rejects premature or disallowed handoffs. Supply an accurate purpose and factual summary. Request owner approval and wait silently; a handoff is not a recorded message or completed transfer. Only record_message may record a note.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"reason": {Type: genai.TypeString, Enum: []string{"owner_requested", "ai_unable"}},
				"caller_name": {Type: genai.TypeString},
				"purpose": {Type: genai.TypeString},
				"summary": {Type: genai.TypeString},
				"company": {Type: genai.TypeString},
				"email": {Type: genai.TypeString},
				"phone_number": {Type: genai.TypeString},
			},
			Required: []string{"reason", "purpose", "summary"},
		},
	}}}
}

// Gemini tool arguments are untrusted. Never synthesize missing caller details.
func parseHandoff(args map[string]any) (realtime.HandoffRequest, error) {
	get := func(key string) (string, error) {
		value, exists := args[key]
		if !exists || value == nil {
			return "", nil
		}
		text, ok := value.(string)
		if !ok {
			return "", fmt.Errorf("%s must be a string", key)
		}
		return strings.TrimSpace(text), nil
	}
	fields := []string{"reason", "caller_name", "purpose", "summary", "company", "email", "phone_number"}
	values := make(map[string]string, len(fields))
	for _, key := range fields {
		value, err := get(key)
		if err != nil {
			return realtime.HandoffRequest{}, err
		}
		values[key] = value
	}
	optional := func(key string) *string {
		if values[key] == "" {
			return nil
		}
		value := values[key]
		return &value
	}
	request := realtime.HandoffRequest{
		Reason: realtime.ParseHandoffReason(values["reason"]),
		Context: realtime.HandoffContext{
			CallerName: optional("caller_name"),
			Purpose: values["purpose"],
			Summary: values["summary"],
			Company: optional("company"),
			Email: optional("email"),
			PhoneNumber: optional("phone_number"),
		},
	}
	if err := request.Validate(); err != nil {
		return realtime.HandoffRequest{}, err
	}
	return request, nil
}

const endCallToolName = "request_call_end"

// The tool is a request, not a direct SDK disconnect. The Go call lifecycle
// decides when to complete and the browser acknowledges final audio playback.
func endCallTool() *genai.Tool {
	return &genai.Tool{FunctionDeclarations: []*genai.FunctionDeclaration{{
		Name: endCallToolName,
		Behavior: genai.BehaviorBlocking,
		Description: "Request natural call completion ONLY after the caller explicitly says goodbye or clearly confirms there is nothing else to add. A note being recorded, a simple okay, a pause, an ambiguous phrase, or an unanswered follow-up question is NOT sufficient. Wait for the caller\u2019s subsequent reply after recording a note. First say a brief goodbye in the caller’s language, then call this tool. Never call for a pause, an unresolved question, or a pending handoff.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"reason": {Type: genai.TypeString, Enum: []string{"conversation_finished"}},
			},
			Required: []string{"reason"},
		},
	}}}
}


const setCallContextName = "set_call_context"

// Context updates are declarative suggestions from Gemini, not verified facts.
// The backend verifies field names and action eligibility; audio transcripts
// remain best-effort and must not be treated as a perfect source of truth.
func contextTool() *genai.Tool {
	return &genai.Tool{FunctionDeclarations: []*genai.FunctionDeclaration{{
		Name: setCallContextName,
		Behavior: genai.BehaviorBlocking,
		Description: "Select the current category as soon as the purpose is clear; use unknown if not clear. Update it whenever intent changes. Supply only newly confirmed facts that the caller actually spoke, as fields key/value pairs. An explicit refusal or statement of not knowing a field may be listed in unavailable_fields. NEVER use silence, hesitation, filler, a partial utterance or your own assumption as evidence. This tool returns category-specific instructions and missing required fields; follow them one question at a time before taking action.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"category": {Type: genai.TypeString, Enum: conversation.Default().Categories()},
				"fields": {Type: genai.TypeArray, Items: &genai.Schema{Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"key": {Type: genai.TypeString}, "value": {Type: genai.TypeString},
					}, Required: []string{"key", "value"}}},
				"unavailable_fields": {Type: genai.TypeArray, Items: &genai.Schema{Type: genai.TypeString}},
			},
			Required: []string{"category"},
		},
	}}}
}

func parseCallContext(args map[string]any) (string, []conversation.FieldUpdate, []string, error) {
	category, ok := args["category"].(string)
	if !ok || category == "" { return "", nil, nil, fmt.Errorf("category is required") }
	var fields []conversation.FieldUpdate
	if raw, exists := args["fields"]; exists && raw != nil {
		values, ok := raw.([]any)
		if !ok { return "", nil, nil, fmt.Errorf("fields must be an array") }
		for _, value := range values {
			item, ok := value.(map[string]any)
			if !ok { return "", nil, nil, fmt.Errorf("invalid field entry") }
			key, keyOK := item["key"].(string)
			text, textOK := item["value"].(string)
			if !keyOK || !textOK { return "", nil, nil, fmt.Errorf("invalid field key/value") }
			fields = append(fields, conversation.FieldUpdate{Key: key, Value: text})
		}
	}
	var unavailable []string
	if raw, exists := args["unavailable_fields"]; exists && raw != nil {
		values, ok := raw.([]any)
		if !ok { return "", nil, nil, fmt.Errorf("unavailable_fields must be an array") }
		for _, value := range values {
			key, ok := value.(string)
			if !ok { return "", nil, nil, fmt.Errorf("invalid unavailable field") }
			unavailable = append(unavailable, key)
		}
	}
	return category, fields, unavailable, nil
}

const recordMessageName = "record_message"

func messageTool() *genai.Tool {
	return &genai.Tool{FunctionDeclarations: []*genai.FunctionDeclaration{{
		Name: recordMessageName,
		Behavior: genai.BehaviorBlocking,
		Description: "Record a note for the current call. It is not automatically sent to the owner. Never do this in the same batch as request_handoff or while a handoff is pending. Only use after set_call_context and after all required fields are answered, declined, or explicitly unknown. For sales calls, NEVER request_handoff. After a successful tool response, acknowledge the actual proposal in natural language (I have noted your offer), without claiming it was sent. Do not discuss technical details or implementation limitations unprompted. If asked about delivery, say truthfully it has not been sent. Ask if the caller wishes to add anything; do NOT automatically end the call. Call BEFORE verbally confirming the note was recorded.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"purpose": {Type: genai.TypeString},
				"summary": {Type: genai.TypeString},
			},
			Required: []string{"purpose", "summary"},
		},
	}}}
}
