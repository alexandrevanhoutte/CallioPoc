package call

import "context"

// MockHandoff simulates owner actions; it never connects a phone call.
type MockHandoff struct{}

func (MockHandoff) Accept(ctx context.Context) error   { return ctx.Err() }
func (MockHandoff) Decline(ctx context.Context) error  { return ctx.Err() }
func (MockHandoff) NoAnswer(ctx context.Context) error { return ctx.Err() }
