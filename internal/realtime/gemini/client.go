package gemini

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
	"google.golang.org/genai"
)

const (
	inputChunkBytes = 640 // PCM16 mono 16 kHz, 20 ms

	frenchInstructions = "Tu es Callio, un assistant IA qui décroche les appels entrants pour son correspondant. " +
		"Parle uniquement en français, avec un ton naturel et chaleureux. " +
		"Chaque intervention doit contenir une ou deux phrases courtes, idéalement 20 mots au total. " +
		"Pose au maximum une question à la fois. Pas de listes, de longs préambules ni d'explications non demandées. " +
		"Après l'accueil, écoute l'appelant et cherche simplement à comprendre l'objet de l'appel. " +
		"Ne prétends pas être humain et n'invente jamais de transfert, de rendez-vous ou d'action effectuée. " +
		"Si l'appelant souhaite parler au propriétaire, ou si tu ne peux pas traiter une demande importante, " +
		"ne déclenche pas immédiatement le transfert : recueille d'abord les informations utiles. " +
		"Demande une fois le nom de l'appelant s'il ne l'a pas déjà donné. Un refus ou une absence de réponse " +
		"ne doit pas bloquer la demande : laisse caller_name absent dans ce cas. " +
		"Vérifie que tu connais le motif précis de l'appel ; s'il est vague, pose une question courte " +
		"pour le clarifier. Ne repose pas de questions dont tu connais déjà la réponse. " +
		"Tu peux demander l'entreprise pour un recrutement, mais email et téléphone restent facultatifs. " +
		"Ne demande jamais toutes les coordonnées systématiquement et n'invente aucune information. " +
		"Quand tu as tenté de demander le nom et que le motif est suffisamment clair, appelle obligatoirement " +
		"request_handoff avec le motif et un résumé fidèle, AVANT d'annoncer que la demande est notée. " +
		"Une annonce orale ne remplace jamais l'appel de l'outil. " +
		"Après confirmation par l'outil, indique seulement que la demande a été enregistrée ; " +
		"ne prétends pas avoir transféré l'appel ni connaître la disponibilité du propriétaire. " +
		"Quand l'appelant indique clairement que tout est terminé ou dit au revoir, réponds une seule fois brièvement, " +
		"puis appelle obligatoirement request_call_end. Ne continue pas les échanges de politesses. " +
		"Ne termine jamais l'appel s'il reste une question ouverte ou une demande de transfert en attente."

	openingPrompt = "Un nouvel appel entrant vient d'être décroché. " +
		"Accueille immédiatement l'appelant à voix haute, en une seule phrase, " +
		"en te présentant comme assistant IA et en lui demandant le motif de son appel."
)

type Provider struct {
	client *genai.Client
	model  string
}

func New(ctx context.Context, apiKey, model string) (*Provider, error) {
	if apiKey == "" {
		return nil, errors.New("GEMINI_API_KEY is missing")
	}
	if model == "" {
		return nil, errors.New("GEMINI_MODEL is missing")
	}
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize Gemini client: %w", err)
	}
	return &Provider{client: client, model: model}, nil
}

func (p *Provider) Open(ctx context.Context) (realtime.VoiceSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	live, err := p.client.Live.Connect(ctx, p.model, liveConfig())
	if err != nil {
		return nil, fmt.Errorf("open Gemini Live session: %w", err)
	}

	// Trigger the receptionist greeting. The browser starts its microphone
	// immediately so that the caller can interrupt this opening turn.
	if err := live.SendClientContent(genai.LiveClientContentInput{
		Turns: []*genai.Content{{
			Role:  "user",
			Parts: []*genai.Part{{Text: openingPrompt}},
		}},
	}); err != nil {
		_ = live.Close()
		return nil, fmt.Errorf("start Gemini greeting: %w", err)
	}

	s := &session{live: live}
	s.stopWatch = context.AfterFunc(ctx, func() { _ = s.Close() })
	return s, nil
}

func liveConfig() *genai.LiveConnectConfig {
	silenceDuration := int32(500)
	return &genai.LiveConnectConfig{
		ResponseModalities: []genai.Modality{genai.ModalityAudio},
		InputAudioTranscription:  &genai.AudioTranscriptionConfig{},
		OutputAudioTranscription: &genai.AudioTranscriptionConfig{},
		Tools: geminiTools(),
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{{Text: frenchInstructions}},
		},
		RealtimeInputConfig: &genai.RealtimeInputConfig{
			AutomaticActivityDetection: &genai.AutomaticActivityDetection{
				SilenceDurationMs: &silenceDuration,
			},
		},
	}
}

type session struct {
	live      *genai.Session
	stopWatch func() bool
	once      sync.Once
	closeErr  error

	// Only Receive accesses this; the send goroutine never touches it.
	turns turnState
	handoff realtime.OnceHandoffDetector
}

func (s *session) SendAudio(ctx context.Context, pcm16 []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(pcm16) != inputChunkBytes {
		return fmt.Errorf("expected %d PCM16 input bytes, got %d", inputChunkBytes, len(pcm16))
	}
	// The Go SDK's SendRealtimeInput does not take a context. Closing the
	// session when ctx is cancelled interrupts the underlying connection.
	return s.live.SendRealtimeInput(genai.LiveRealtimeInput{
		Audio: &genai.Blob{Data: pcm16, MIMEType: "audio/pcm;rate=16000"},
	})
}

func (s *session) Receive(ctx context.Context) (realtime.Event, error) {
	for {
		if err := ctx.Err(); err != nil {
			return realtime.Event{}, err
		}
		if event, ok := s.turns.next(); ok {
			return event, nil
		}
		message, err := s.live.Receive()
		if err != nil {
			return realtime.Event{}, err
		}
		if message == nil {
			continue
		}
		if message.ToolCall != nil {
			slog.Info("Gemini tool call message received", "calls", len(message.ToolCall.FunctionCalls))
			if err := s.handleToolCalls(message.ToolCall); err != nil {
				return realtime.Event{}, err
			}
		}
		if message.ServerContent == nil {
			continue
		}
		if err := s.turns.process(message.ServerContent); err != nil {
			return realtime.Event{}, err
		}
	}
}

func (s *session) Close() error {
	s.once.Do(func() {
		if s.stopWatch != nil {
			s.stopWatch()
		}
		s.closeErr = s.live.Close()
	})
	return s.closeErr
}

// handleToolCalls acknowledges every Gemini tool call, including invalid requests.
// It only emits a validated handoff once per session.
func (s *session) handleToolCalls(toolCall *genai.LiveServerToolCall) error {
	responses := make([]*genai.FunctionResponse, 0, len(toolCall.FunctionCalls))
	for _, call := range toolCall.FunctionCalls {
		if call == nil {
			continue
		}
		slog.Info("Gemini function requested", "name", call.Name, "call_id", call.ID)
		response := &genai.FunctionResponse{ID: call.ID, Name: call.Name}
		switch call.Name {
		case endCallToolName:
			if call.Args["reason"] != "conversation_finished" {
				response.Response = map[string]any{"error": "invalid call end reason"}
			} else {
				// Lifecycle handles duplicates; a later request may be valid
				// if the caller interrupted an earlier farewell.
				s.turns.pending = append(s.turns.pending, realtime.Event{Type: realtime.EventCallEndRequested})
				slog.Info("Gemini natural call completion requested", "call_id", call.ID)
				response.Response = map[string]any{"output": "call completion requested; final audio playback must finish"}
			}
		case handoffToolName:
			request, err := parseHandoff(call.Args)
			if err != nil {
				slog.Warn("Gemini handoff arguments rejected", "error", err)
				response.Response = map[string]any{"error": "invalid handoff arguments"}
			} else {
				emit, err := s.handoff.Detect(request)
				if err != nil { return err }
				if emit {
					slog.Info("handoff accepted", "reason", request.Reason.String(), "call_id", call.ID)
					s.turns.pending = append(s.turns.pending, realtime.Event{Type: realtime.EventHandoffRequired, Handoff: &request})
				} else {
					slog.Info("duplicate handoff ignored", "call_id", call.ID)
				}
				response.Response = map[string]any{"output": "handoff request recorded; no transfer performed"}
			}
		default:
			response.Response = map[string]any{"error": "unsupported tool"}
		}
		responses = append(responses, response)
	}
	if len(responses) == 0 {
		return nil
	}
	if err := s.live.SendToolResponse(genai.LiveToolResponseInput{FunctionResponses: responses}); err != nil {
		return fmt.Errorf("send Gemini tool responses: %w", err)
	}
	return nil
}

func (s *session) NotifyHandoffOutcome(ctx context.Context, outcome string) error {
	if err := ctx.Err(); err != nil { return err }
	var instruction string
	switch outcome {
	case "decline":
		instruction = "Information interne : le propriétaire a refusé la prise en main simulée. "
	case "no_answer":
		instruction = "Information interne : le propriétaire ne répond pas à la demande de prise en main simulée. "
	default:
		return fmt.Errorf("unsupported handoff outcome: %q", outcome)
	}
	instruction += "Explique brièvement à l'appelant que le propriétaire n'est pas disponible et propose de prendre un message. " +
		"Ne laisse pas croire que l'appel a été réellement transféré."
	if err := s.live.SendClientContent(genai.LiveClientContentInput{
		Turns: []*genai.Content{{
			Role: "user",
			Parts: []*genai.Part{{Text: instruction}},
		}},
	}); err != nil {
		return fmt.Errorf("notify Gemini of handoff outcome: %w", err)
	}
	return nil
}
