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

	frenchInstructions = "Tu es Callio, un assistant vocal qui répond aux appels. " +
		"Réponds exclusivement en français, à voix haute, naturellement et brièvement. " +
		"Pour le moment, discute avec l'appelant et demande-lui comment tu peux l'aider. " +
		"N'invente pas d'informations, de capacités de transfert d'appel ou d'actions réalisées."
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
	live, err := p.client.Live.Connect(ctx, p.model, &genai.LiveConnectConfig{
		ResponseModalities: []genai.Modality{genai.ModalityAudio},
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{{Text: frenchInstructions}},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("open Gemini Live session: %w", err)
	}

	s := &session{live: live}
	s.stopWatch = context.AfterFunc(ctx, func() { _ = s.Close() })
	return s, nil
}

type session struct {
	live      *genai.Session
	stopWatch func() bool
	once      sync.Once
	closeErr  error

	// Only the receiver goroutine accesses pending.
	pending []realtime.Event
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
		content := message.ServerContent
		if content.ModelTurn != nil {
			for _, part := range content.ModelTurn.Parts {
				if part == nil || part.InlineData == nil || len(part.InlineData.Data) == 0 {
					continue
				}
				// Gemini Live output is PCM16 24 kHz. Normalise variable-sized
				// SDK messages into 20 ms chunks for bounded browser playback.
				audio := part.InlineData.Data
				if len(audio)%2 != 0 {
					return realtime.Event{}, errors.New("invalid Gemini PCM16 output length")
				}
				for offset := 0; offset < len(audio); offset += outputChunkBytes {
					end := min(offset+outputChunkBytes, len(audio))
					s.pending = append(s.pending, realtime.Event{
						Type:       realtime.EventAudio,
						Audio:      append([]byte(nil), audio[offset:end]...),
						SampleRate: outputSampleRate,
					})
				}
			}
		}
		if content.TurnComplete {
			s.pending = append(s.pending, realtime.Event{Type: realtime.EventTurnEnd})
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
