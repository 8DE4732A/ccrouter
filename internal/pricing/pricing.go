// Package pricing converts recorded token usage into a USD cost using the
// prices configured on a combo member.
//
// Price semantics follow models.dev: USD per 1M tokens. Token semantics differ
// between upstream APIs, so Normalize reconciles them before Compute is
// applied. In particular, OpenAI/Gemini/Responses prompt token counts include
// cached tokens while Anthropic's input_tokens excludes them.
package pricing

import (
	"math"

	"ccrouter/internal/config"
)

// Tokens is a normalized, billable token breakdown.
type Tokens struct {
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
	// Present reports whether usage contained at least one token value. When
	// false the request has no usage data and its cost must stay unknown.
	Present bool
}

// Normalize reconciles a raw usage map (as produced by proxy.extractUsage /
// sniffUsageChunk) with the upstream API format that produced it.
func Normalize(usage map[string]any, upstreamAPIFormat string) Tokens {
	present := false
	for _, k := range []string{
		"prompt_tokens", "completion_tokens",
		"cache_read_tokens", "cache_write_tokens",
	} {
		if v, ok := usage[k]; ok && v != nil {
			present = true
			break
		}
	}
	if !present {
		return Tokens{}
	}

	prompt := intOf(usage["prompt_tokens"])
	output := intOf(usage["completion_tokens"])
	cacheRead := intOf(usage["cache_read_tokens"])
	cacheWrite := intOf(usage["cache_write_tokens"])

	input := prompt
	if upstreamAPIFormat != "anthropic" {
		// OpenAI, openai-responses, gemini and embeddings report cached
		// tokens inside the prompt count. Anthropic reports them separately.
		input -= cacheRead
	}
	if input < 0 {
		input = 0
	}

	return Tokens{
		Input:      input,
		Output:     output,
		CacheRead:  cacheRead,
		CacheWrite: cacheWrite,
		Present:    true,
	}
}

// Compute applies USD-per-1M prices to normalized tokens. It returns ok=false
// when no price is configured, there is no usage to price, or a category that
// actually consumed tokens has no configured price (the total would be an
// undercount). Explicit zero prices yield ok=true, 0.
func Compute(cost *config.ComboMemberCost, tok Tokens) (float64, bool) {
	if cost.Empty() || !tok.Present {
		return 0, false
	}
	// nil means "unknown", not "free". Only an explicit 0 means free, so a
	// consumed category without a price makes the total unknowable.
	if (tok.Input > 0 && cost.Input == nil) ||
		(tok.Output > 0 && cost.Output == nil) ||
		(tok.CacheRead > 0 && cost.CacheRead == nil) ||
		(tok.CacheWrite > 0 && cost.CacheWrite == nil) {
		return 0, false
	}
	usd := float64(tok.Input)/1e6*price(cost.Input) +
		float64(tok.Output)/1e6*price(cost.Output) +
		float64(tok.CacheRead)/1e6*price(cost.CacheRead) +
		float64(tok.CacheWrite)/1e6*price(cost.CacheWrite)
	return math.Round(usd*1e9) / 1e9, true
}

func price(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func intOf(v any) int64 {
	switch t := v.(type) {
	case int:
		return int64(t)
	case int64:
		return t
	case float64:
		return int64(t)
	case *int:
		if t != nil {
			return int64(*t)
		}
	}
	return 0
}
