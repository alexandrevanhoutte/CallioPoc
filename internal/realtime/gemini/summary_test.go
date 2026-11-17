package gemini

import (
	"context"
	"github.com/alexandrevanhoutte/CallioPoc/internal/realtime"
	"testing"

	"github.com/alexandrevanhoutte/CallioPoc/internal/call"
	"google.golang.org/genai"
)

func TestParseSummary(t *testing.T) {
	raw := `{"caller_name":null,"reason":null,"urgency":"unknown","follow_up_actions":[],"handoff_requested":false,"summary":"Le motif reste inconnu."}`
	summary, err := parseSummary(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	if !summary.HandoffRequested || summary.CallerName != nil || summary.Reason != nil {
		t.Fatalf("summary must preserve unknown fields and trusted handoff: %+v", summary)
	}
	for _, bad := range []string{
		"",
		"null",
		`{"urgency":"unknown","summary":""}`,
		`{"urgency":"urgent","summary":"Appel."}`,
		`{"urgency":"unknown","summary":"Appel.","fake":true}`,
		raw + raw,
	} {
		if _, err := parseSummary(bad, false); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

func TestSummarizerContract(t *testing.T) {
	var _ call.Summarizer = (*Summarizer)(nil)
}

func TestNoFinalizedTranscriptDoesNotCallModel(t *testing.T) {
	for _, locale := range []realtime.Locale{realtime.LocaleFrench, realtime.LocaleEnglish, realtime.LocaleKorean} {
		s := &Summarizer{}
		out, err := s.Summarize(context.Background(), call.SummaryInput{
			Locale: locale,
			Transcript: []realtime.TranscriptSegment{{
				ID: "caller-1", Speaker: realtime.SpeakerCaller, Text: "unfinished",
				Final: false,
			}},
		})
		if err != nil || out.Text != realtime.EmptyTranscriptSummary(locale) || out.Urgency != "unknown" || out.CallerName != nil {
			t.Fatalf("fallback summary must be factual without model: %+v, %v", out, err)
		}
	}
}

// Transient server failures may be retried once; caller/input errors may not.
func TestRetryableSummaryError(t *testing.T) {
	for _, code := range []int{500, 502, 503, 504} {
		if !retryableSummaryError(genai.APIError{Code: code}) {
			t.Fatalf("expected retryable status %d", code)
		}
	}
	for _, code := range []int{400, 401, 403, 404, 429} {
		if retryableSummaryError(genai.APIError{Code: code}) {
			t.Fatalf("unexpected retry on status %d", code)
		}
	}
	if retryableSummaryError(context.DeadlineExceeded) {
		t.Fatal("do not retry after caller context expired")
	}
}
