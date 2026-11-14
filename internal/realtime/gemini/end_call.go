package gemini

import "google.golang.org/genai"

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
