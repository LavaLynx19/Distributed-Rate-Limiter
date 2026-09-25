package redis

import "testing"

func TestParseDecision(t *testing.T) {
	allowed, err := parseDecision([]any{"ALLOWED", int64(42), int64(60)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed.Allowed || allowed.Remaining != 42 || allowed.ResetTTL != 60 {
		t.Errorf("got %+v, want allowed with remaining 42, ttl 60", allowed)
	}
	if allowed.FailedOpen {
		t.Error("a parsed decision must never be marked fail-open")
	}

	blocked, err := parseDecision([]any{"BLOCKED", int64(0), int64(17)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if blocked.Allowed {
		t.Error("BLOCKED status must not be allowed")
	}
	if blocked.ResetTTL != 17 {
		t.Errorf("ResetTTL = %d, want 17", blocked.ResetTTL)
	}
}

func TestParseDecisionRejectsMalformedReplies(t *testing.T) {
	cases := map[string][]any{
		"too few elements":      {"ALLOWED", int64(1)},
		"status not string":     {int64(1), int64(1), int64(1)},
		"remaining not integer": {"ALLOWED", "nope", int64(1)},
		"ttl not integer":       {"ALLOWED", int64(1), "nope"},
	}

	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseDecision(raw); err == nil {
				t.Error("expected an error so the caller fails open")
			}
		})
	}
}

func TestFailOpenAllows(t *testing.T) {
	d := failOpen()
	if !d.Allowed || !d.FailedOpen {
		t.Errorf("got %+v, want allowed and flagged fail-open", d)
	}
}
