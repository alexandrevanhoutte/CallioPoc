package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
	"strings"

	"github.com/alexandrevanhoutte/CallioPoc/internal/call"
	"github.com/alexandrevanhoutte/CallioPoc/internal/logging"
	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
	"google.golang.org/genai"
)

// Summarizer uses a separate text generation request; it never changes the
// state or lifecycle of the live voice session.
type Summarizer struct {
	client *genai.Client
	model  string
	l      logging.Logger
}

func NewSummarizer(ctx context.Context, apiKey, model string) (*Summarizer, error) {
	if apiKey == "" || model == "" {
		return nil, errors.New("summary API key and model are required")
	}
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: apiKey, Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize summarizer: %w", err)
	}
	return &Summarizer{client: client, model: model, l: logging.For("gemini.summary")}, nil
}

func (s *Summarizer) Summarize(ctx context.Context, input call.SummaryInput) (call.Summary, error) {
	transcript := make([]struct {
		Speaker string `json:"speaker"`
		Text    string `json:"text"`
	}, 0, len(input.Transcript))
	for _, segment := range input.Transcript {
		if !segment.Final || len(segment.Text) == 0 {
			continue
		}
		if segment.Speaker != realtime.SpeakerCaller && segment.Speaker != realtime.SpeakerAI {
			continue
		}
		transcript = append(transcript, struct {
			Speaker string `json:"speaker"`
			Text    string `json:"text"`
		}{Speaker: segment.Speaker.String(), Text: segment.Text})
	}
	if len(transcript) == 0 && input.Handoff == nil {
		return call.Summary{
			Urgency: "unknown", FollowUpActions: []string{},
			HandoffRequested: input.Handoff != nil,
			Text: realtime.EmptyTranscriptSummary(input.Locale),
		}, nil
	}
	var handoff any
	if input.Handoff != nil {
		handoff = map[string]any{
			"reason": input.Handoff.Reason.String(),
			"context": input.Handoff.Context,
		}
	}
	contextJSON, err := json.Marshal(struct {
		Transcript any `json:"transcript"`
		Handoff any `json:"handoff,omitempty"`
		EndReason string `json:"end_reason"`
	}{Transcript: transcript, Handoff: handoff, EndReason: input.EndReason.String()})
	if err != nil {
		return call.Summary{}, fmt.Errorf("encode summary input: %w", err)
	}
	prompt := realtime.SummaryPrompt(contextJSON, input.Locale)

	// Retry one transient upstream 5xx error. The total budget is enforced
	// by the caller, including any backoff and the second attempt.
	var response *genai.GenerateContentResponse
	for attempt := 0; attempt < 2; attempt++ {
		response, err = s.client.Models.GenerateContent(ctx, s.model,
			[]*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: prompt}}}},
			&genai.GenerateContentConfig{
			ResponseMIMEType: "application/json",
			ResponseSchema: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"caller_name": {Type: genai.TypeString},
					"reason": {Type: genai.TypeString},
					"urgency": {Type: genai.TypeString, Enum: []string{"unknown", "low", "normal", "high"}},
					"follow_up_actions": {Type: genai.TypeArray, Items: &genai.Schema{Type: genai.TypeString}},
					"handoff_requested": {Type: genai.TypeBoolean},
					"summary": {Type: genai.TypeString},
				},
				Required: []string{"urgency", "follow_up_actions", "handoff_requested", "summary"},
			},
		},
		)
		if err == nil { break }
		if !retryableSummaryError(err) || attempt == 1 {
			return call.Summary{}, fmt.Errorf("Gemini summary generation: %w", err)
		}
		s.l.Warn("transient Gemini summary failure; retrying once", "error", err)
		timer := time.NewTimer(time.Second)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return call.Summary{}, ctx.Err()
		}
	}

	if response == nil || len(response.Text()) == 0 {
		return call.Summary{}, errors.New("Gemini returned an empty summary")
	}
	return parseSummary(response.Text(), input.Handoff != nil)
}

// parseSummary accepts one JSON object only. The backend owns factual
// handoff status, which must not be inferred or invented by the model.
func parseSummary(raw string, handoff bool) (call.Summary, error) {
	var summary call.Summary
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&summary); err != nil {
		return call.Summary{}, fmt.Errorf("decode Gemini summary: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return call.Summary{}, errors.New("unexpected extra JSON after summary")
	} else if !errors.Is(err, io.EOF) {
		return call.Summary{}, fmt.Errorf("trailing summary content: %w", err)
	}
	summary.HandoffRequested = handoff
	if summary.FollowUpActions == nil {
		summary.FollowUpActions = []string{}
	}
	if err := summary.Validate(); err != nil {
		return call.Summary{}, fmt.Errorf("validate Gemini summary: %w", err)
	}
	return summary, nil
}

func retryableSummaryError(err error) bool {
	var apiErr genai.APIError
	if !errors.As(err, &apiErr) { return false }
	switch apiErr.Code {
	case 500, 502, 503, 504:
		return true
	default:
		return false
	}
}
