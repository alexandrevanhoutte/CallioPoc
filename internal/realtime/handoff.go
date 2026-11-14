package realtime

import (
	"errors"
)

// HandoffReason is an internal enum; its string form is the wire format.
type HandoffReason uint8

const (
	HandoffUnknown HandoffReason = iota
	HandoffOwnerRequested
	HandoffAIUnable
)

func (r HandoffReason) String() string {
	switch r {
	case HandoffOwnerRequested:
		return "owner_requested"
	case HandoffAIUnable:
		return "ai_unable"
	default:
		return "unknown"
	}
}

func ParseHandoffReason(value string) HandoffReason {
	switch value {
	case "owner_requested":
		return HandoffOwnerRequested
	case "ai_unable":
		return HandoffAIUnable
	default:
		return HandoffUnknown
	}
}

// Nullable fields represent information the caller did not provide.
type HandoffContext struct {
	CallerName  *string `json:"caller_name"`
	Purpose     string  `json:"purpose"`
	Summary     string  `json:"summary"`
	Company     *string `json:"company,omitempty"`
	Email       *string `json:"email,omitempty"`
	PhoneNumber *string `json:"phone_number,omitempty"`
}

type HandoffRequest struct {
	Reason  HandoffReason
	Context HandoffContext
}

func (r HandoffRequest) Validate() error {
	if r.Reason != HandoffOwnerRequested && r.Reason != HandoffAIUnable {
		return errors.New("invalid handoff reason")
	}
	if len(r.Context.Purpose) == 0 {
		return errors.New("handoff purpose is required")
	}
	if len(r.Context.Summary) == 0 {
		return errors.New("handoff summary is required")
	}
	return nil
}

// HandoffDetector decides whether a normalized request becomes an event.
type HandoffDetector interface {
	Detect(HandoffRequest) (bool, error)
}

// OnceHandoffDetector emits at most one handoff per live session.
// Only the session's receive goroutine accesses this implementation.
type OnceHandoffDetector struct {
	emitted bool
}

func (d *OnceHandoffDetector) Detect(request HandoffRequest) (bool, error) {
	if err := request.Validate(); err != nil {
		return false, err
	}
	if d.emitted {
		return false, nil
	}
	d.emitted = true
	return true, nil
}
