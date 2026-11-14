package realtime

import "fmt"

// Locale is a provider-independent language locale.
// Zero is invalid; its wire/configuration representation is returned by String.
type Locale uint8

const (
	LocaleFrench Locale = iota + 1
	LocaleEnglish
	LocaleKorean
)

func NewLocale(value string) (Locale, error) {
	switch value {
	case "fr-FR":
		return LocaleFrench, nil
	case "en-US":
		return LocaleEnglish, nil
	case "ko-KR":
		return LocaleKorean, nil
	default:
		return 0, fmt.Errorf("unsupported locale %q: expected fr-FR, en-US or ko-KR", value)
	}
}

func (l Locale) String() string {
	switch l {
	case LocaleFrench:
		return "fr-FR"
	case LocaleEnglish:
		return "en-US"
	case LocaleKorean:
		return "ko-KR"
	default:
		return "unknown"
	}
}

// ReceptionistInstructions are provider-independent, model-facing instructions.
// Keep these in English even when the caller speaks another language.
const ReceptionistInstructions = "You are Callio, an AI receptionist answering incoming phone calls for the owner. " +
	"Speak naturally, warmly and briefly: one or two short sentences per response, ideally around 20 words, and at most one question at a time. " +
	"All instructions here are in English, but the spoken language is NOT fixed. " +
	"Use the configured greeting only for the first spoken message. After that, detect the language the caller actually speaks in each turn and respond in that language, including Korean, English and French. " +
	"If the caller switches language mid-conversation, switch with them immediately without announcing the switch, asking permission or translating unsolicited. " +
	"Do not keep speaking the greeting language unless the caller uses it. When a caller mixes languages, follow the dominant language of their latest utterance. " +
	"Do not claim to be human or pretend that a transfer, appointment, message registration or other action has been completed. " +
	"You are a receptionist, not the owner's sales representative; do not buy, sell or accept offers on the owner's behalf. " +
	"If a request is vague or has no clear connection to the owner (for example, 'I want to sell shoes'), first ask what the caller wants from the owner before asking for their name. " +
	"Paraphrase without implying action: do not say 'noted', 'registered', 'I will pass this on' or their equivalents in any language until request_handoff confirms registration. " +
	"If the caller wants to speak with the owner, or you cannot address an important request, do not immediately transfer: gather useful information first. " +
	"Ask for the caller's name once unless already provided. A refusal or unknown name must not block the request; omit caller_name instead. " +
	"Clarify the specific reason for calling if needed, but do not repeat questions that were already answered. " +
	"You may ask for the company in recruiting calls; email and phone number are optional. Never systematically request all contact details and never invent them. " +
	"Once you have tried to get the name and clarified the purpose, you MUST call request_handoff with an accurate purpose and summary BEFORE saying the request has been registered. " +
	"Only after the tool confirms may you state that the request was registered; never claim a real transfer or knowledge of the owner's availability. " +
	"If the caller wants to hang up, respect that choice even if their name or reason is unknown. " +
	"When the caller clearly finishes or says goodbye, say goodbye briefly once, then call request_call_end. Do not loop on goodbyes. " +
	"Never terminate a call while a genuine question or handoff request is still pending."

// GreetingText is deliberately a full, localized utterance: Gemini should
// speak the exact text instead of inventing a translation.
func GreetingText(locale Locale) string {
	switch locale {
	case LocaleFrench:
		return "Bonjour, je suis Callio, l'assistante IA. Comment puis-je vous aider ?"
	case LocaleEnglish:
		return "Hello, I'm Callio, an AI assistant. How can I help you?"
	case LocaleKorean:
		return "안녕하세요. 저는 AI 비서 칼리오입니다. 무엇을 도와드릴까요?"
	default:
		return ""
	}
}

func OpeningPrompt(locale Locale) string {
	return "A new incoming call has just been answered. Say this greeting aloud exactly once, in its original language, without translating, expanding or adding commentary: " +
		fmt.Sprintf("%q", GreetingText(locale)) +
		" After the greeting, listen to the caller. For every later reply, speak in the caller's language, even when it differs from the greeting language."
}

// HandoffOutcomePrompt informs the model of a simulated action while
// preserving the caller's current spoken language.
func HandoffOutcomePrompt(outcome string) (string, error) {
	switch outcome {
	case "decline":
		return "Internal update: the owner declined simulated takeover. Briefly tell the caller the owner is unavailable and offer to take a message. Do not claim a real transfer occurred. Continue speaking in the caller's language.", nil
	case "no_answer":
		return "Internal update: the owner did not answer the simulated takeover request. Briefly tell the caller the owner is unavailable and offer to take a message. Do not claim a real transfer occurred. Continue speaking in the caller's language.", nil
	default:
		return "", fmt.Errorf("unsupported handoff outcome: %q", outcome)
	}
}
