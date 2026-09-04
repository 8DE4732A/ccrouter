package proxy

import (
	"reflect"
	"testing"
)

func TestSniffUsageChunk_AnthropicGLMCompatible(t *testing.T) {
	// Tests GLM / Anthropic-compatible provider where message_start has 0 tokens
	// and message_delta provides the final input_tokens and output_tokens.
	holder := newUsageHolder()

	chunk1 := []byte(`event: message_start
data: {"type":"message_start","message":{"id":"msg_123","type":"message","role":"assistant","model":"glm-5-3-flash","content":[],"usage":{"input_tokens":0,"output_tokens":0}}}

`)
	sniffUsageChunk(chunk1, "anthropic", holder)

	if p, ok := toInt(holder.usage["prompt_tokens"]); !ok || *p != 0 {
		t.Fatalf("expected prompt_tokens 0 after message_start, got %v", holder.usage["prompt_tokens"])
	}

	chunk2 := []byte(`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}

`)
	sniffUsageChunk(chunk2, "anthropic", holder)

	chunk3 := []byte(`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":30694,"output_tokens":34,"cache_read_input_tokens":12,"cache_creation_input_tokens":5}}

`)
	sniffUsageChunk(chunk3, "anthropic", holder)

	wantPrompt := 30694
	wantCompletion := 34
	wantTotal := 30728
	wantCacheRead := 12
	wantCacheWrite := 5

	if p, ok := toInt(holder.usage["prompt_tokens"]); !ok || *p != wantPrompt {
		t.Errorf("prompt_tokens: got %v, want %d", holder.usage["prompt_tokens"], wantPrompt)
	}
	if c, ok := toInt(holder.usage["completion_tokens"]); !ok || *c != wantCompletion {
		t.Errorf("completion_tokens: got %v, want %d", holder.usage["completion_tokens"], wantCompletion)
	}
	if tot, ok := toInt(holder.usage["total_tokens"]); !ok || *tot != wantTotal {
		t.Errorf("total_tokens: got %v, want %d", holder.usage["total_tokens"], wantTotal)
	}
	if cr, ok := toInt(holder.usage["cache_read_tokens"]); !ok || *cr != wantCacheRead {
		t.Errorf("cache_read_tokens: got %v, want %d", holder.usage["cache_read_tokens"], wantCacheRead)
	}
	if cw, ok := toInt(holder.usage["cache_write_tokens"]); !ok || *cw != wantCacheWrite {
		t.Errorf("cache_write_tokens: got %v, want %d", holder.usage["cache_write_tokens"], wantCacheWrite)
	}
}

func TestSniffUsageChunk_AnthropicStandard(t *testing.T) {
	// Standard Anthropic: message_start has input_tokens, message_delta only has output_tokens.
	holder := newUsageHolder()

	chunk1 := []byte(`event: message_start
data: {"type":"message_start","message":{"id":"msg_123","type":"message","role":"assistant","model":"claude-3-5-sonnet","content":[],"usage":{"input_tokens":120,"cache_read_input_tokens":50,"cache_creation_input_tokens":20}}}

`)
	sniffUsageChunk(chunk1, "anthropic", holder)

	chunk2 := []byte(`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":45}}

`)
	sniffUsageChunk(chunk2, "anthropic", holder)

	wantPrompt := 120
	wantCompletion := 45
	wantTotal := 165
	wantCacheRead := 50
	wantCacheWrite := 20

	if p, ok := toInt(holder.usage["prompt_tokens"]); !ok || *p != wantPrompt {
		if ok {
			t.Errorf("prompt_tokens: got %d, want %d", *p, wantPrompt)
		} else {
			t.Errorf("prompt_tokens: got nil, want %d", wantPrompt)
		}
	}
	if c, ok := toInt(holder.usage["completion_tokens"]); !ok || *c != wantCompletion {
		if ok {
			t.Errorf("completion_tokens: got %d, want %d", *c, wantCompletion)
		} else {
			t.Errorf("completion_tokens: got nil, want %d", wantCompletion)
		}
	}
	if tot, ok := toInt(holder.usage["total_tokens"]); !ok || *tot != wantTotal {
		if ok {
			t.Errorf("total_tokens: got %d, want %d", *tot, wantTotal)
		} else {
			t.Errorf("total_tokens: got nil, want %d", wantTotal)
		}
	}
	if cr, ok := toInt(holder.usage["cache_read_tokens"]); !ok || *cr != wantCacheRead {
		t.Errorf("cache_read_tokens: got %v, want %d", holder.usage["cache_read_tokens"], wantCacheRead)
	}
	if cw, ok := toInt(holder.usage["cache_write_tokens"]); !ok || *cw != wantCacheWrite {
		t.Errorf("cache_write_tokens: got %v, want %d", holder.usage["cache_write_tokens"], wantCacheWrite)
	}
}

func TestExtractUsage_Anthropic(t *testing.T) {
	body := []byte(`{"id":"msg_1","type":"message","usage":{"input_tokens":100,"output_tokens":25,"cache_read_input_tokens":10,"cache_creation_input_tokens":5}}`)
	usage := extractUsage(body, "anthropic")

	p, _ := toInt(usage["prompt_tokens"])
	c, _ := toInt(usage["completion_tokens"])
	tot, _ := toInt(usage["total_tokens"])

	if p == nil || *p != 100 {
		t.Errorf("prompt_tokens: got %v, want 100", usage["prompt_tokens"])
	}
	if c == nil || *c != 25 {
		t.Errorf("completion_tokens: got %v, want 25", usage["completion_tokens"])
	}
	if tot == nil || *tot != 125 {
		t.Errorf("total_tokens: got %v, want 125", usage["total_tokens"])
	}
}

func intVal(v int) *int {
	return &v
}

func TestExtractUsage_ExistingFormats(t *testing.T) {
	// OpenAI
	openaiBody := []byte(`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`)
	u := extractUsage(openaiBody, "openai")
	if !reflect.DeepEqual(u["prompt_tokens"], intVal(10)) || !reflect.DeepEqual(u["completion_tokens"], intVal(20)) || !reflect.DeepEqual(u["total_tokens"], intVal(30)) {
		t.Errorf("openai usage mismatch: %v", u)
	}

	// Gemini
	geminiBody := []byte(`{"usageMetadata":{"promptTokenCount":15,"candidatesTokenCount":25,"totalTokenCount":40}}`)
	gu := extractUsage(geminiBody, "gemini")
	if !reflect.DeepEqual(gu["prompt_tokens"], intVal(15)) || !reflect.DeepEqual(gu["completion_tokens"], intVal(25)) || !reflect.DeepEqual(gu["total_tokens"], intVal(40)) {
		t.Errorf("gemini usage mismatch: %v", gu)
	}

	// OpenAI Embeddings (no completion_tokens)
	embBody := []byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":8,"total_tokens":8}}`)
	eu := extractUsage(embBody, "openai-embeddings")
	if !reflect.DeepEqual(eu["prompt_tokens"], intVal(8)) || eu["completion_tokens"] != nil || !reflect.DeepEqual(eu["total_tokens"], intVal(8)) {
		t.Errorf("openai-embeddings usage mismatch: %v", eu)
	}

	// OpenAI Embeddings with missing total_tokens
	embBodyMissingTotal := []byte(`{"object":"list","data":[],"usage":{"prompt_tokens":12}}`)
	eum := extractUsage(embBodyMissingTotal, "openai-embeddings")
	if !reflect.DeepEqual(eum["prompt_tokens"], intVal(12)) || eum["completion_tokens"] != nil || !reflect.DeepEqual(eum["total_tokens"], intVal(12)) {
		t.Errorf("openai-embeddings fallback total_tokens mismatch: %v", eum)
	}
}
