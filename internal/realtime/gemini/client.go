package gemini

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/alexandrevanhoutte/CallioPoc/internal/conversation"
	"github.com/alexandrevanhoutte/CallioPoc/internal/logging"
	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
	"google.golang.org/genai"
)

const inputChunkBytes = 640 // PCM16 mono 16 kHz, 20 ms

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

func (p *Provider) Open(ctx context.Context, locale realtime.Locale) (realtime.VoiceSession, error) {
	if _, err := realtime.NewLocale(locale.String()); err != nil { return nil, err }
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
			Parts: []*genai.Part{{Text: realtime.OpeningPrompt(locale)}},
		}},
	}); err != nil {
		_ = live.Close()
		return nil, fmt.Errorf("start Gemini greeting: %w", err)
	}

	s := &session{live: live, context: conversation.NewState(conversation.Default()), l: logging.For("gemini.live")}
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
			Parts: []*genai.Part{{Text: realtime.ReceptionistInstructions}},
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
	l         logging.Logger

	// Only Receive accesses this; the send goroutine never touches it.
	turns turnState
	handoff realtime.OnceHandoffDetector
	context *conversation.State
	messageRecorded bool
	notePurpose string
	noteSummary string
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

// EndAudioStream prompts Gemini to finalize the input. New audio reopens the
// stream while Gemini's automatic activity detection remains enabled.
func (s *session) EndAudioStream(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.live.SendRealtimeInput(genai.LiveRealtimeInput{AudioStreamEnd: true})
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
			s.l.Info("Gemini tool call message received", "calls", len(message.ToolCall.FunctionCalls))
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
	if s.context == nil { s.context = conversation.NewState(conversation.Default()) }
	responses := make([]*genai.FunctionResponse, 0, len(toolCall.FunctionCalls))
	contextUpdated := false
	hasContextCall := false
	hasMessageCall := false
	for _, candidate := range toolCall.FunctionCalls {
		if candidate != nil && candidate.Name == setCallContextName { hasContextCall = true }
		if candidate != nil && candidate.Name == recordMessageName { hasMessageCall = true }
	}
	for _, call := range toolCall.FunctionCalls {
		if call == nil { continue }
		s.l.Info("Gemini function requested", "name", call.Name, "call_id", call.ID)
		response := &genai.FunctionResponse{ID: call.ID, Name: call.Name}
		switch call.Name {
		case setCallContextName:
			category, fields, unavailable, err := parseCallContext(call.Args)
			if err == nil {
				var policy conversation.Policy
				policy, err = s.context.Update(category, fields, unavailable)
				if err == nil {
					response.Response = map[string]any{
						"output": "context updated; ask one question at a time for any missing field",
						"category": policy.ID,
						"missing_fields": s.context.Missing(),
						"available_actions": policy.Actions,
						"instructions": policy.Instructions,
					}
					contextUpdated = true
				}
			}
			if err != nil {
				response.Response = map[string]any{"error": err.Error(), "output": "correct the call context before requesting action"}
			}
		case endCallToolName:
			if hasMessageCall {
				response.Response = map[string]any{"error": "wait for a subsequent caller reply after recording a note; do not conclude this call immediately"}
			} else if call.Args["reason"] != "conversation_finished" {
				response.Response = map[string]any{"error": "invalid call end reason"}
			} else {
				s.turns.pending = append(s.turns.pending, realtime.Event{Type: realtime.EventCallEndRequested})
				s.l.Info("Gemini natural call completion requested", "call_id", call.ID)
				response.Response = map[string]any{"output": "call completion requested; final audio playback must finish"}
			}
		case handoffToolName:
			switch {
			case hasContextCall || contextUpdated:
				response.Response = map[string]any{"error": "do not request an action in the same tool batch as updating call context; wait for the next turn"}
			case s.context.Allow(handoffToolName) != nil:
				response.Response = map[string]any{"error": s.context.Allow(handoffToolName).Error()}
			default:
				request, err := parseHandoff(call.Args)
				if err != nil {
					s.l.Warn("Gemini handoff arguments rejected", "error", err)
					response.Response = map[string]any{"error": "invalid handoff arguments"}
					break
				}
				if name, ok := s.context.Value("caller_name"); ok {
					if request.Context.CallerName != nil && *request.Context.CallerName != name {
						response.Response = map[string]any{"error": "caller_name does not match collected call context"}
						break
					}
					if request.Context.CallerName == nil { request.Context.CallerName = &name }
				}
				emit, err := s.handoff.Detect(request)
				if err != nil { return err }
				if emit {
					s.l.Info("handoff accepted", "reason", request.Reason.String(), "call_id", call.ID)
					s.turns.pending = append(s.turns.pending, realtime.Event{Type: realtime.EventHandoffRequired, Handoff: &request})
				} else {
					s.l.Info("duplicate handoff ignored", "call_id", call.ID)
				}
				response.Response = map[string]any{"output": "handoff request recorded; no transfer performed"}
			}
		case recordMessageName:
			switch {
			case hasContextCall || contextUpdated:
				response.Response = map[string]any{"error": "wait until the context update is acknowledged before recording a message"}
			case s.context.Allow(recordMessageName) != nil:
				response.Response = map[string]any{"error": s.context.Allow(recordMessageName).Error()}
			default:
				purpose, pOK := call.Args["purpose"].(string)
				summary, sOK := call.Args["summary"].(string)
				if !pOK || !sOK || strings.TrimSpace(purpose) == "" || strings.TrimSpace(summary) == "" {
					response.Response = map[string]any{"error": "purpose and summary are required"}
					break
				}
				s.messageRecorded = true
				s.notePurpose = strings.TrimSpace(purpose)
				s.noteSummary = strings.TrimSpace(summary)
				response.Response = map[string]any{"output": "note recorded in temporary memory for this call; you may say you have noted the caller\u0027s proposal, but do not claim delivery or persistence; ask if they wish to add anything and wait for a caller reply"}
			}
		default:
			response.Response = map[string]any{"error": "unsupported tool"}
		}
		responses = append(responses, response)
	}
	if len(responses) == 0 { return nil }
	if err := s.live.SendToolResponse(genai.LiveToolResponseInput{FunctionResponses: responses}); err != nil {
		return fmt.Errorf("send Gemini tool responses: %w", err)
	}
	return nil
}

func (s *session) NotifyHandoffOutcome(ctx context.Context, outcome string) error {
	if err := ctx.Err(); err != nil { return err }
	instruction, err := realtime.HandoffOutcomePrompt(outcome)
	if err != nil {
		return err
	}
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
