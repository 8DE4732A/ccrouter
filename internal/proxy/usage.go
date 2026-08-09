package proxy

import (
	"encoding/json"
	"strings"
)

// extractUsage parses the usage field from a completed (non-streaming) response.
func extractUsage(body []byte, apiFormat string) map[string]any {
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return map[string]any{}
	}

	// Gemini uses "usageMetadata" with different field names.
	if apiFormat == "gemini" {
		u, _ := data["usageMetadata"].(map[string]any)
		if u == nil {
			return map[string]any{}
		}
		inp := keep(u["promptTokenCount"])
		out := keep(u["candidatesTokenCount"])
		total := keep(u["totalTokenCount"])
		cached := keep(u["cachedContentTokenCount"])
		return map[string]any{
			"prompt_tokens":      inp,
			"completion_tokens":  out,
			"total_tokens":       total,
			"cache_read_tokens":  cached,
			"cache_write_tokens": nil,
		}
	}

	// openai-responses non-stream: usage is nested in response.completed event
	if apiFormat == "openai-responses" {
		// response.completed event format: {"type":"response.completed","response":{...,"usage":{...}}}
		if typ, _ := data["type"].(string); typ == "response.completed" || typ == "response.incomplete" {
			resp, _ := data["response"].(map[string]any)
			data = resp
		}
		// Direct response object: {"object":"response","usage":{...}}
		// (or unwrapped from the event above)
		u, _ := data["usage"].(map[string]any)
		if u == nil {
			return map[string]any{}
		}
		inp := keep(u["input_tokens"])
		out := keep(u["output_tokens"])
		total := keep(u["total_tokens"])
		var cached any
		if details, _ := u["input_tokens_details"].(map[string]any); details != nil {
			cached = keep(details["cached_tokens"])
		}
		return map[string]any{
			"prompt_tokens":      inp,
			"completion_tokens":  out,
			"total_tokens":       total,
			"cache_read_tokens":  cached,
			"cache_write_tokens": nil,
		}
	}

	u, _ := data["usage"].(map[string]any)
	if u == nil {
		return map[string]any{}
	}
	if apiFormat == "anthropic" {
		inp, _ := toInt(u["input_tokens"])
		out, _ := toInt(u["output_tokens"])
		var total any
		if inp != nil && out != nil {
			t := *inp + *out
			total = t
		}
		return map[string]any{
			"prompt_tokens":      inp,
			"completion_tokens":  out,
			"total_tokens":       total,
			"cache_read_tokens":  keep(u["cache_read_input_tokens"]),
			"cache_write_tokens": keep(u["cache_creation_input_tokens"]),
		}
	}
	details, _ := u["prompt_tokens_details"].(map[string]any)
	var cached any
	if details != nil {
		cached = keep(details["cached_tokens"])
	}
	return map[string]any{
		"prompt_tokens":      keep(u["prompt_tokens"]),
		"completion_tokens":  keep(u["completion_tokens"]),
		"total_tokens":       keep(u["total_tokens"]),
		"cache_read_tokens":  cached,
		"cache_write_tokens": nil,
	}
}

// toInt converts a JSON-ish number to *int, nil if absent/non-numeric.
func toInt(v any) (*int, bool) {
	switch t := v.(type) {
	case float64:
		x := int(t)
		return &x, true
	case int:
		return &t, true
	case int64:
		x := int(t)
		return &x, true
	case json.Number:
		var f float64
		if err := json.Unmarshal([]byte(t.String()), &f); err == nil {
			x := int(f)
			return &x, true
		}
	}
	return nil, false
}

// keep returns a numeric value as int when possible, else nil.
func keep(v any) any {
	if p, ok := toInt(v); ok {
		return p
	}
	return nil
}

// usageHolder tracks cross-chunk SSE usage sniffing state.
type usageHolder struct {
	pending []byte
	usage   map[string]any
}

func newUsageHolder() *usageHolder {
	return &usageHolder{usage: map[string]any{}}
}

// sniffUsageChunk parses SSE lines from a chunk and updates holder usage in place.
func sniffUsageChunk(chunk []byte, apiFormat string, holder *usageHolder) {
	holder.pending = append(holder.pending, chunk...)
	text := string(holder.pending)
	lines := strings.Split(text, "\n")
	holder.pending = []byte(lines[len(lines)-1])

	for _, raw := range lines[:len(lines)-1] {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		jsonStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if jsonStr == "[DONE]" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(jsonStr), &event); err != nil {
			continue
		}

		if apiFormat == "gemini" {
			// Gemini SSE chunks contain "usageMetadata" in the final chunk.
			u, _ := event["usageMetadata"].(map[string]any)
			if u != nil {
				holder.usage["prompt_tokens"] = keep(u["promptTokenCount"])
				holder.usage["completion_tokens"] = keep(u["candidatesTokenCount"])
				holder.usage["total_tokens"] = keep(u["totalTokenCount"])
				holder.usage["cache_read_tokens"] = keep(u["cachedContentTokenCount"])
			}
		} else if apiFormat == "openai-responses" {
			// OpenAI Responses API SSE: usage in response.completed event
			if event["type"] == "response.completed" {
				resp, _ := event["response"].(map[string]any)
				u, _ := resp["usage"].(map[string]any)
				if u != nil {
					holder.usage["prompt_tokens"] = keep(u["input_tokens"])
					holder.usage["completion_tokens"] = keep(u["output_tokens"])
					holder.usage["total_tokens"] = keep(u["total_tokens"])
					if details, _ := u["input_tokens_details"].(map[string]any); details != nil {
						holder.usage["cache_read_tokens"] = keep(details["cached_tokens"])
					}
				}
			}
		} else if apiFormat == "anthropic" {
			if event["type"] == "message_start" {
				m, _ := event["message"].(map[string]any)
				u, _ := m["usage"].(map[string]any)
				if u != nil {
					if v, ok := toInt(u["input_tokens"]); ok {
						holder.usage["prompt_tokens"] = v
					}
					if v, ok := toInt(u["cache_read_input_tokens"]); ok {
						holder.usage["cache_read_tokens"] = v
					}
					if v, ok := toInt(u["cache_creation_input_tokens"]); ok {
						holder.usage["cache_write_tokens"] = v
					}
				}
			} else if event["type"] == "message_delta" {
				u, _ := event["usage"].(map[string]any)
				if u != nil {
					if v, ok := toInt(u["output_tokens"]); ok {
						holder.usage["completion_tokens"] = v
						if inp, ok := toInt(holder.usage["prompt_tokens"]); ok {
							t := *inp + *v
							holder.usage["total_tokens"] = t
						} else {
							holder.usage["total_tokens"] = v
						}
					}
				}
			}
		} else {
			u, _ := event["usage"].(map[string]any)
			if u != nil {
				if _, ok := toInt(u["total_tokens"]); ok {
					holder.usage["prompt_tokens"] = keep(u["prompt_tokens"])
					holder.usage["completion_tokens"] = keep(u["completion_tokens"])
					holder.usage["total_tokens"] = keep(u["total_tokens"])
					details, _ := u["prompt_tokens_details"].(map[string]any)
					if details != nil {
						if cached, ok := toInt(details["cached_tokens"]); ok {
							holder.usage["cache_read_tokens"] = cached
						}
					}
				}
			}
		}
	}
}
