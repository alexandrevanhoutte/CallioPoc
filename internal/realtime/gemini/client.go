package gemini

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
	"google.golang.org/genai"
)

const (
	inputChunkBytes  = 640 // PCM16 mono 16 kHz, 20 ms
	outputChunkBytes = 960 // PCM16 mono 24 kHz, 20 ms
	outputSampleRate = 24000

	frenchInstructions = "Tu es Callio, un assistant IA qui décroche les appels entrants pour son correspondant. " +
		"Parle uniquement en français, avec un ton naturel et chaleureux. " +
		"Chaque intervention doit contenir une ou deux phrases courtes, idéalement 20 mots au total. " +
		"Pose au maximum une question à la fois. Pas de listes, de longs préambules ni d'explications non demandées. " +
		"Après l'accueil, écoute l'appelant et cherche simplement à comprendre l'objet de l'appel. " +
		"Ne prétends pas être humain et n'invente jamais de transfert, de rendez-vous ou d'action effectuée."

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

	// Trigger the initial receptionist greeting. The web client waits until
	// playback ends before starting microphone capture.
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

	// Only the receiver goroutine accesses turn state and pending events.
	pending     []realtime.Event
	turnID      uint64
	turnActive  bool
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
		if len(s.pending) > 0 {
			event := s.pending[0]
			s.pending[0] = realtime.Event{}
			s.pending = s.pending[1:]
			return event, nil
		}
		message, err := s.live.Receive()
		if err != nil {
			return realtime.Event{}, err
		}
		if message == nil || message.ServerContent == nil {
			continue
		}
		if err := s.queueContent(message.ServerContent); err != nil {
			return realtime.Event{}, err
		}
	}
}

// queueContent serializes model turns into ordered, provider-independent events.
// Gemini reports an interrupted turn via Interrupted followed by TurnComplete;
// the latter must not be treated as completion of a fresh response.
func (s *session) queueContent(content *genai.LiveServerContent) error {
	if content == nil {
		return nil
	}
	if content.Interrupted {
		// In particular, discard audio chunks from a large SDK message that
		// have not been forwarded to the browser yet.
		s.pending = []realtime.Event{{Type: realtime.EventInterrupted, TurnID: s.turnID}}
		s.turnActive = false
		return nil
	}
	if content.ModelTurn != nil {
		for _, part := range content.ModelTurn.Parts {
			if part == nil || part.InlineData == nil || len(part.InlineData.Data) == 0 {
				continue
			}
			audio := part.InlineData.Data
			if len(audio)%2 != 0 {
				return errors.New("invalid Gemini PCM16 output length")
			}
			if !s.turnActive {
				s.turnID++
				s.turnActive = true
			}
			// Keep the existing 20 ms wire chunks. The frontend can queue
			// them, and a later interruption clears this pending list.
			for offset := 0; offset < len(audio); offset += outputChunkBytes {
				end := min(offset+outputChunkBytes, len(audio))
				s.pending = append(s.pending, realtime.Event{
					Type:       realtime.EventAudio,
					TurnID:     s.turnID,
					Audio:      append([]byte(nil), audio[offset:end]...),
					SampleRate: outputSampleRate,
				})
			}
		}
	}
	if content.TurnComplete && s.turnActive {
		s.pending = append(s.pending, realtime.Event{Type: realtime.EventTurnEnd, TurnID: s.turnID})
		s.turnActive = false
	}
	return nil
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
