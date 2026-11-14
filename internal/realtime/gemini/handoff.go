package gemini

import (
	"fmt"
	"strings"

	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
	"google.golang.org/genai"
)

const handoffToolName = "request_handoff"

func handoffTool() *genai.Tool {
	return &genai.Tool{FunctionDeclarations: []*genai.FunctionDeclaration{{
		Name: handoffToolName,
		Description: "Demande de mise en relation avec le propriétaire de l'assistant. À utiliser si l'appelant souhaite lui parler, ou si l'assistant ne peut pas traiter une demande importante. Ne prétend jamais effectuer le transfert.",
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
