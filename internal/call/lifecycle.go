package call

import (
	"context"
	"errors"
	"sync"
)

// State describes the browser-simulated call lifecycle.
type State uint8

const (
	AIActive State = iota
	HandoffRequired
	HumanActive
	Completed
)

func (s State) String() string {
	switch s {
	case AIActive:
		return "ai_active"
	case HandoffRequired:
		return "handoff_required"
	case HumanActive:
		return "human_active"
	case Completed:
		return "completed"
	default:
		return "unknown"
	}
}

var ErrInvalidTransition = errors.New("invalid call state transition")

// HandoffProvider implements the action, not the decision to request it.
// A real telephony provider can replace the simulated implementation later.
type HandoffProvider interface {
	Accept(context.Context) error
	Decline(context.Context) error
	NoAnswer(context.Context) error
}

type Lifecycle struct {
	mu       sync.Mutex
	state    State
	provider HandoffProvider
}

func NewLifecycle(provider HandoffProvider) *Lifecycle {
	return &Lifecycle{state: AIActive, provider: provider}
}

func (c *Lifecycle) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

func (c *Lifecycle) RequestHandoff() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state != AIActive {
		return ErrInvalidTransition
	}
	c.state = HandoffRequired
	return nil
}

func (c *Lifecycle) Accept(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state != HandoffRequired {
		return ErrInvalidTransition
	}
	if err := c.provider.Accept(ctx); err != nil {
		return err
	}
	c.state = HumanActive
	return nil
}

func (c *Lifecycle) Decline(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state != HandoffRequired {
		return ErrInvalidTransition
	}
	if err := c.provider.Decline(ctx); err != nil {
		return err
	}
	c.state = AIActive
	return nil
}

func (c *Lifecycle) NoAnswer(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state != HandoffRequired {
		return ErrInvalidTransition
	}
	if err := c.provider.NoAnswer(ctx); err != nil {
		return err
	}
	c.state = AIActive
	return nil
}

func (c *Lifecycle) Complete() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == Completed {
		return ErrInvalidTransition
	}
	c.state = Completed
	return nil
}
