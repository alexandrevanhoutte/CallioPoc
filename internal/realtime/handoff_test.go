package realtime_test

import (
	"testing"

	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
)

func TestHandoffValidationAndDeduplication(t *testing.T) {
	valid := realtime.HandoffRequest{
		Reason: realtime.HandoffOwnerRequested,
		Context: realtime.HandoffContext{
			Purpose: "Recruitment",
			Summary: "The caller wants to discuss a job.",
		},
	}
	var detector realtime.OnceHandoffDetector
	ok, err := detector.Detect(valid)
	if err != nil || !ok {
		t.Fatalf("first valid handoff: ok=%v err=%v", ok, err)
	}
	ok, err = detector.Detect(valid)
	if err != nil || ok {
		t.Fatalf("duplicate handoff: ok=%v err=%v", ok, err)
	}
	for _, req := range []realtime.HandoffRequest{
		{Reason: realtime.HandoffUnknown, Context: valid.Context},
		{Reason: realtime.HandoffOwnerRequested, Context: realtime.HandoffContext{Summary: "Summary"}},
		{Reason: realtime.HandoffAIUnable, Context: realtime.HandoffContext{Purpose: "Purpose"}},
	} {
		if err := req.Validate(); err == nil {
			t.Fatalf("expected validation error for %+v", req)
		}
	}
	if realtime.ParseHandoffReason("owner_requested") != realtime.HandoffOwnerRequested ||
		realtime.ParseHandoffReason("ai_unable") != realtime.HandoffAIUnable ||
		realtime.ParseHandoffReason("invalid") != realtime.HandoffUnknown {
		t.Fatal("handoff reason conversion failed")
	}
}
