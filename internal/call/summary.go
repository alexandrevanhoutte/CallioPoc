package call

import (
	"context"
	"errors"

	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
)

// SummaryInput contains only known conversation facts and final transcript segments.
type SummaryInput struct {
	Transcript []realtime.TranscriptSegment
	Handoff    *realtime.HandoffRequest
	EndReason  EndReason
	Locale     realtime.Locale
}

// Summary is provider-independent and suitable for future CI-10 persistence.
// Unknown names and reasons remain null, rather than invented values.
type Summary struct {
	CallerName       *string  `json:"caller_name"`
	Reason           *string  `json:"reason"`
	Urgency          string   `json:"urgency"`
	FollowUpActions  []string `json:"follow_up_actions"`
	HandoffRequested bool     `json:"handoff_requested"`
	Text             string   `json:"summary"`
}

type Summarizer interface {
	Summarize(ctx context.Context, input SummaryInput) (Summary, error)
}

func (s Summary) Validate() error {
	if len(s.Text) == 0 {
		return errors.New("summary text is required")
	}
	switch s.Urgency {
	case "unknown", "low", "normal", "high":
	default:
		return errors.New("invalid summary urgency")
	}
	if s.CallerName != nil && len(*s.CallerName) == 0 {
		return errors.New("empty caller name must be null")
	}
	if s.Reason != nil && len(*s.Reason) == 0 {
		return errors.New("empty reason must be null")
	}
	if len(s.FollowUpActions) > 5 {
		return errors.New("too many follow-up actions")
	}
	for _, action := range s.FollowUpActions {
		if len(action) == 0 {
			return errors.New("follow-up action must be nonempty")
		}
	}
	return nil
}
