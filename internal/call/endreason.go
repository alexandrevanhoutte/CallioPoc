package call

// EndReason describes why the simulated call terminated.
type EndReason uint8

const (
	EndUnknown EndReason = iota
	EndCallerFinished
	EndIdleTimeout
	EndManual
)

func (r EndReason) String() string {
	switch r {
	case EndCallerFinished:
		return "caller_finished"
	case EndIdleTimeout:
		return "idle_timeout"
	case EndManual:
		return "manual"
	default:
		return "unknown"
	}
}
