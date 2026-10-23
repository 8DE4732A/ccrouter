package proxy

import (
	"testing"

	"ccrouter/internal/config"
)

func newRuleTestService(t *testing.T) *Service {
	t.Helper()
	expr, err := compileJSONPath("$.body")
	if err != nil {
		t.Fatal(err)
	}
	rule := &config.HealthCheckRule{
		Description: "Balance limit exceeded",
		JSONPath:    "$.body",
		MatchValue:  "Balance limit exceeded",
		MatchType:   "contains",
	}
	return &Service{providerRules: map[string][]compiledRule{
		"za": {{expr: expr, rule: rule}},
	}}
}

func TestMatchRotationRulesBareJSON(t *testing.T) {
	s := newRuleTestService(t)
	body := []byte(`{"body":"Balance limit exceeded. Current: 2500.00459488, Limit: 2500","status_code":429}`)
	if got := s.matchRotationRules(body, "za", "m", 429); got == nil {
		t.Fatal("expected match for bare JSON body")
	}
}

func TestMatchRotationRulesPlainText(t *testing.T) {
	s := newRuleTestService(t)
	body := []byte("Balance limit exceeded. Current: 2500.00459488, Limit: 2500")
	if got := s.matchRotationRules(body, "za", "m", 429); got == nil {
		t.Fatal("expected match for plain-text body")
	}
}

func TestCheckSSEErrorBareJSON(t *testing.T) {
	s := newRuleTestService(t)
	body := []byte(`{"body":"Balance limit exceeded. Current: 2500.00459488, Limit: 2500","status_code":429}`)
	if got := s.checkSSEError(body, "za", "m", 429); got == nil {
		t.Fatal("expected match for bare JSON first frame")
	}
}

func TestCheckSSEErrorSSEData(t *testing.T) {
	s := newRuleTestService(t)
	body := []byte("event: error\ndata: {\"body\":\"Balance limit exceeded\"}\n\n")
	if got := s.checkSSEError(body, "za", "m", 429); got == nil {
		t.Fatal("expected match for SSE data frame")
	}
}

func TestCheckSSEErrorPlainText(t *testing.T) {
	s := newRuleTestService(t)
	body := []byte("Balance limit exceeded. Current: 2500.00459488, Limit: 2500")
	if got := s.checkSSEError(body, "za", "m", 429); got == nil {
		t.Fatal("expected match for plain-text first frame")
	}
}

func TestCheckSSEErrorNoMatch(t *testing.T) {
	s := newRuleTestService(t)
	if got := s.checkSSEError([]byte(`{"body":"rate limit exceeded"}`), "za", "m", 429); got != nil {
		t.Fatalf("unexpected match: %+v", got)
	}
}

func TestRotationRulesIgnoreEmptyBody(t *testing.T) {
	s := newRuleTestService(t)
	if got := s.matchRotationRules(nil, "za", "m", 429); got != nil {
		t.Fatalf("unexpected match for empty body: %+v", got)
	}
	if got := s.checkSSEError(nil, "za", "m", 429); got != nil {
		t.Fatalf("unexpected match for empty first frame: %+v", got)
	}
}

func TestCheckSSEErrorIgnoresDoneFrame(t *testing.T) {
	s := newRuleTestService(t)
	// "DONE" must not be treated as a plain-text body when there is no data line.
	if got := s.checkSSEError([]byte("event: done\ndata: [DONE]\n\n"), "za", "m", 429); got != nil {
		t.Fatalf("unexpected match for SSE terminator: %+v", got)
	}
}
