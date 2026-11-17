package call

import "testing"

func TestEndReasonString(t *testing.T) {
	cases := []struct {
		reason EndReason
		want   string
	}{
		{EndUnknown, "unknown"},
		{EndCallerFinished, "caller_finished"},
		{EndIdleTimeout, "idle_timeout"},
		{EndManual, "manual"},
		{EndReason(255), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.reason.String(); got != tc.want {
			t.Fatalf("%d.String() = %q, want %q", tc.reason, got, tc.want)
		}
	}
}
