package pricing

import (
	"math"
	"testing"

	"ccrouter/internal/config"
)

func f(v float64) *float64 { return &v }

func cost(in, out, cacheRead, cacheWrite float64) *config.ComboMemberCost {
	return &config.ComboMemberCost{
		Input:      f(in),
		Output:     f(out),
		CacheRead:  f(cacheRead),
		CacheWrite: f(cacheWrite),
	}
}

func TestNormalizeAnthropicDoesNotSubtractCache(t *testing.T) {
	tok := Normalize(map[string]any{
		"prompt_tokens":      100,
		"completion_tokens":  40,
		"cache_read_tokens":  50,
		"cache_write_tokens": 20,
	}, "anthropic")
	if tok.Input != 100 || tok.CacheRead != 50 || tok.CacheWrite != 20 || tok.Output != 40 {
		t.Fatalf("unexpected tokens: %+v", tok)
	}
	usd, ok := Compute(cost(3, 15, 0.3, 3.75), tok)
	if !ok {
		t.Fatal("expected cost")
	}
	want := 100.0/1e6*3 + 40.0/1e6*15 + 50.0/1e6*0.3 + 20.0/1e6*3.75
	if math.Abs(usd-want) > 1e-12 {
		t.Fatalf("usd = %v, want %v", usd, want)
	}
}

func TestNormalizeOpenAISubtractsCachedFromInput(t *testing.T) {
	tok := Normalize(map[string]any{
		"prompt_tokens":     100,
		"completion_tokens": 40,
		"cache_read_tokens": 40,
	}, "openai")
	if tok.Input != 60 || tok.CacheRead != 40 {
		t.Fatalf("unexpected tokens: %+v", tok)
	}
	usd, ok := Compute(cost(3, 15, 0.3, 0), tok)
	if !ok {
		t.Fatal("expected cost")
	}
	want := 60.0/1e6*3 + 40.0/1e6*15 + 40.0/1e6*0.3
	if math.Abs(usd-want) > 1e-12 {
		t.Fatalf("usd = %v, want %v", usd, want)
	}
}

func TestNormalizeGeminiAndResponses(t *testing.T) {
	for _, format := range []string{"gemini", "openai-responses", "openai-embeddings", ""} {
		tok := Normalize(map[string]any{
			"prompt_tokens":     10,
			"completion_tokens": 5,
			"cache_read_tokens": 4,
		}, format)
		if tok.Input != 6 {
			t.Fatalf("%s: input = %d, want 6", format, tok.Input)
		}
	}
}

func TestNormalizeClampsNegativeInput(t *testing.T) {
	tok := Normalize(map[string]any{
		"prompt_tokens":     10,
		"cache_read_tokens": 15,
	}, "openai")
	if tok.Input != 0 {
		t.Fatalf("input = %d, want 0", tok.Input)
	}
}

func TestComputeUnknownAndFree(t *testing.T) {
	tok := Tokens{Input: 100, Present: true}

	if _, ok := Compute(nil, tok); ok {
		t.Fatal("nil cost should be unknown")
	}
	if _, ok := Compute(&config.ComboMemberCost{}, tok); ok {
		t.Fatal("empty cost should be unknown")
	}
	zero, ok := Compute(cost(0, 0, 0, 0), tok)
	if !ok || zero != 0 {
		t.Fatalf("explicit zero prices should be free, got %v/%v", zero, ok)
	}
	if _, ok := Compute(cost(1, 1, 1, 1), Tokens{}); ok {
		t.Fatal("missing usage should be unknown")
	}
}

func TestNormalizeEmptyUsage(t *testing.T) {
	if tok := Normalize(nil, "openai"); tok.Present {
		t.Fatalf("nil usage should not be present: %+v", tok)
	}
	if tok := Normalize(map[string]any{"prompt_tokens": nil}, "openai"); tok.Present {
		t.Fatalf("nil token value should not be present: %+v", tok)
	}
}

func TestComputeMissingPriceForConsumedCategoryIsUnknown(t *testing.T) {
	// Configured input/output but no cache_read price: a request that actually
	// read from cache cannot be priced accurately, so it must stay NULL.
	c := &config.ComboMemberCost{Input: f(3), Output: f(15)}
	if _, ok := Compute(c, Tokens{Input: 10, CacheRead: 5, Present: true}); ok {
		t.Fatal("consumed cache tokens without a price must be unknown")
	}
	// No cache tokens consumed: the missing cache price is irrelevant.
	usd, ok := Compute(c, Tokens{Input: 10, Present: true})
	if !ok || math.Abs(usd-30.0/1e6) > 1e-12 {
		t.Fatalf("usd = %v, ok = %v", usd, ok)
	}
}

func TestNormalizeOnlyTotalTokensIsNotPriceable(t *testing.T) {
	tok := Normalize(map[string]any{"total_tokens": 100}, "openai")
	if tok.Present {
		t.Fatalf("total_tokens alone must not be priceable: %+v", tok)
	}
	if _, ok := Compute(cost(1, 1, 1, 1), tok); ok {
		t.Fatal("expected unknown cost")
	}
}
