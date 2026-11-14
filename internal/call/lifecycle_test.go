package call

import (
	"context"
	"errors"
	"testing"
)

type failingHandoff struct{ err error }
func (f failingHandoff) Accept(context.Context) error { return f.err }
func (f failingHandoff) Decline(context.Context) error { return f.err }
func (f failingHandoff) NoAnswer(context.Context) error { return f.err }

func TestLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name string
		respond func(*Lifecycle) error
		want State
	}{
		{"accept", func(c *Lifecycle) error { return c.Accept(context.Background()) }, HumanActive},
		{"decline", func(c *Lifecycle) error { return c.Decline(context.Background()) }, AIActive},
		{"no_answer", func(c *Lifecycle) error { return c.NoAnswer(context.Background()) }, AIActive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewLifecycle(MockHandoff{})
			if c.State() != AIActive || c.State().String() != "ai_active" {
				t.Fatal("invalid initial state")
			}
			if err := c.Accept(context.Background()); !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("accept before handoff: %v", err)
			}
			if err := c.RequestHandoff(); err != nil { t.Fatal(err) }
			if err := c.RequestHandoff(); !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("repeated request: %v", err)
			}
			if err := tc.respond(c); err != nil { t.Fatal(err) }
			if c.State() != tc.want { t.Fatalf("got %s want %s", c.State(), tc.want) }
			if err := tc.respond(c); !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("duplicate action: %v", err)
			}
			if err := c.Complete(); err != nil { t.Fatal(err) }
			if err := c.Complete(); !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("duplicate completion: %v", err)
			}
		})
	}
}

func TestProviderErrorPreservesState(t *testing.T) {
	c := NewLifecycle(failingHandoff{err: errors.New("unavailable")})
	if err := c.RequestHandoff(); err != nil { t.Fatal(err) }
	for _, act := range []func(context.Context) error{c.Accept, c.Decline, c.NoAnswer} {
		if err := act(context.Background()); err == nil { t.Fatal("expected mock failure") }
		if c.State() != HandoffRequired { t.Fatal("state changed on provider failure") }
	}
}

func TestCancelledContext(t *testing.T) {
	c := NewLifecycle(MockHandoff{})
	if err := c.RequestHandoff(); err != nil { t.Fatal(err) }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Accept(ctx); !errors.Is(err, context.Canceled) { t.Fatal(err) }
	if c.State() != HandoffRequired { t.Fatal("cancelled operation transitioned") }
}
