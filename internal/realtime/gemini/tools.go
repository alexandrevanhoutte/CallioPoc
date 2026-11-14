package gemini

import (
	"fmt"
	"strings"

	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
	"google.golang.org/genai"
)

// geminiTools centralizes Gemini function declarations.
// Keep each handler separate so it can be tested independently.
func geminiTools() []*genai.Tool {
	return []*genai.Tool{handoffTool(), endCallTool()}
}

const handoffToolName = "request_handoff"

func handoffTool() *genai.Tool {
	return &genai.Tool{FunctionDeclarations: []*genai.FunctionDeclaration{{
		Name: handoffToolName,
		Description: "Enregistre une demande de mise en relation, sans transfert réel. Appeler uniquement après avoir tenté de demander le nom de l'appelant (sauf s'il est déjà connu) et clarifié le motif. Un nom refusé ou inconnu peut être omis ; ne jamais inventer de coordonnées. Fournir un purpose précis et un summary factuel. Appeler cet outil AVANT de confirmer oralement la prise en compte.",
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
		Description: "Demande la fin naturelle de l'appel UNIQUEMENT quand l'appelant a clairement terminé l'échange. Prononce d'abord un bref au revoir puis appelle cet outil. Ne l'utilise jamais pour une simple pause, une question encore ouverte ou une demande de transfert.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"reason": {Type: genai.TypeString, Enum: []string{"conversation_finished"}},
			},
			Required: []string{"reason"},
		},
	}}}
}
