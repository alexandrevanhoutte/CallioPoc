package realtime

import (
	"context"
	"fmt"
)

// EventType identifies a provider-independent realtime event.
// Zero is invalid, so uninitialized event types cannot be mistaken for audio.
type EventType uint8

const (
	EventAudio EventType = iota + 1
	EventTranscript
	EventTurnEnd
	EventInterrupted
)

func NewEventType(value string) (EventType, error) {
	switch value {
	case "audio":
		return EventAudio, nil
	case "transcript":
		return EventTranscript, nil
	case "turn_end":
		return EventTurnEnd, nil
	case "interrupted":
		return EventInterrupted, nil
	default:
		return 0, fmt.Errorf("invalid event type: %q", value)
	}
}

func (e EventType) String() string {
	switch e {
	case EventAudio:
		return "audio"
	case EventTranscript:
		return "transcript"
	case EventTurnEnd:
		return "turn_end"
	case EventInterrupted:
		return "interrupted"
	default:
		return "unknown"
	}
}

// Event carries normalised output from any realtime provider.
// Audio must be little-endian, mono, signed PCM16 at SampleRate hertz.
type Event struct {
	Type       EventType
	TurnID     uint64 // Local generation ID for ordering and diagnostics, not transmitted with PCM.
	Audio      []byte
	SampleRate int
	Text       string
}

// VoiceProvider opens one live conversation for each browser session.
type VoiceProvider interface {
	Open(ctx context.Context) (VoiceSession, error)
}

// VoiceSession permits simultaneous SendAudio and Receive calls.
// Implementations must unblock Receive after Close.
type VoiceSession interface {
	SendAudio(ctx context.Context, pcm16 []byte) error
	Receive(ctx context.Context) (Event, error)
	Close() error
}
