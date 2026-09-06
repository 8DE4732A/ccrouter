package translate

import (
	"context"
	"encoding/json"
	"testing"
)

func TestGeminiTranslations(t *testing.T) {
	// 1. Check NeedTranslate for OpenAI -> Gemini
	if !NeedTranslate("openai", "gemini") {
		t.Fatalf("expected NeedTranslate('openai', 'gemini') to be true")
	}

	// 2. Check NeedTranslate for Anthropic -> Gemini
	if !NeedTranslate("anthropic", "gemini") {
		t.Fatalf("expected NeedTranslate('anthropic', 'gemini') to be true")
	}

	// 3. Check NeedTranslate for OpenAI-Responses -> Gemini
	if !NeedTranslate("openai-responses", "gemini") {
		t.Fatalf("expected NeedTranslate('openai-responses', 'gemini') to be true")
	}

	// 4. Test OpenAI Chat -> Gemini Request translation
	openAIReq := []byte(`{
		"model": "gemini-1.5-pro",
		"messages": [
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "user", "content": "Hello, Gemini!"}
		],
		"temperature": 0.7,
		"max_tokens": 100
	}`)
	geminiReq := TranslateRequest("openai", "gemini", "gemini-1.5-pro", openAIReq, false)
	t.Logf("OpenAI -> Gemini Request:\n%s", string(geminiReq))
	if len(geminiReq) == 0 {
		t.Fatalf("expected non-empty translated Gemini request")
	}

	// 5. Test Gemini Response -> OpenAI Chat NonStream translation
	geminiResp := []byte(`{
		"candidates": [
			{
				"content": {
					"parts": [
						{"text": "Hello! How can I help you today?"}
					],
					"role": "model"
				},
				"finishReason": "STOP",
				"index": 0
			}
		],
		"usageMetadata": {
			"promptTokenCount": 15,
			"candidatesTokenCount": 9,
			"totalTokenCount": 24
		}
	}`)
	var param any
	openAIResp := TranslateResponseNonStream(context.Background(), "openai", "gemini", "gemini-1.5-pro", openAIReq, geminiReq, geminiResp, &param)
	t.Logf("Gemini -> OpenAI NonStream Response:\n%s", string(openAIResp))
	if len(openAIResp) == 0 {
		t.Fatalf("expected non-empty translated OpenAI response")
	}

	// 6. Test Gemini Response -> OpenAI Chat Stream translation
	geminiStreamChunk := []byte(`data: {"candidates":[{"content":{"parts":[{"text":"Hello"}],"role":"model"},"index":0}]}`)
	var streamParam any
	streamChunks := TranslateResponseStream(context.Background(), "openai", "gemini", "gemini-1.5-pro", openAIReq, geminiReq, geminiStreamChunk, &streamParam)
	t.Logf("Gemini -> OpenAI Stream Chunks count: %d", len(streamChunks))
	for i, c := range streamChunks {
		t.Logf("  chunk %d: %s", i, string(c))
	}
	if len(streamChunks) == 0 {
		t.Fatalf("expected non-empty stream chunks from Gemini SSE")
	}

	// 7. Test Anthropic (Claude) -> Gemini Request translation
	claudeReq := []byte(`{
		"model": "gemini-1.5-pro",
		"system": "Be concise.",
		"messages": [
			{"role": "user", "content": "Hi"}
		],
		"max_tokens": 50
	}`)
	geminiFromClaude := TranslateRequest("anthropic", "gemini", "gemini-1.5-pro", claudeReq, false)
	t.Logf("Claude -> Gemini Request:\n%s", string(geminiFromClaude))
	if len(geminiFromClaude) == 0 {
		t.Fatalf("expected non-empty translated request from Claude to Gemini")
	}

	// 8. Test Gemini Response -> Anthropic (Claude) Response translation
	var claudeParam any
	claudeResp := TranslateResponseNonStream(context.Background(), "anthropic", "gemini", "gemini-1.5-pro", claudeReq, geminiFromClaude, geminiResp, &claudeParam)
	t.Logf("Gemini -> Claude NonStream Response:\n%s", string(claudeResp))
	if len(claudeResp) == 0 {
		t.Fatalf("expected non-empty translated response from Gemini to Claude")
	}

	// 9. Check Reverse: client = Gemini, upstream = OpenAI
	if !NeedTranslate("gemini", "openai") {
		t.Fatalf("expected NeedTranslate('gemini', 'openai') to be true")
	}
	geminiReqSample := []byte(`{
		"contents": [
			{"role": "user", "parts": [{"text": "Hello from Gemini client"}]}
		],
		"generationConfig": {
			"temperature": 0.5,
			"maxOutputTokens": 200
		}
	}`)
	openaiFromGemini := TranslateRequest("gemini", "openai", "gpt-4o", geminiReqSample, false)
	t.Logf("Gemini -> OpenAI Request:\n%s", string(openaiFromGemini))
	if len(openaiFromGemini) == 0 {
		t.Fatalf("expected non-empty translated request from Gemini to OpenAI")
	}

	// 10. Check Reverse: client = Gemini, upstream = Anthropic (Claude)
	if !NeedTranslate("gemini", "anthropic") {
		t.Fatalf("expected NeedTranslate('gemini', 'anthropic') to be true")
	}
	claudeFromGemini := TranslateRequest("gemini", "anthropic", "claude-3-5-sonnet", geminiReqSample, false)
	t.Logf("Gemini -> Claude Request:\n%s", string(claudeFromGemini))
	if len(claudeFromGemini) == 0 {
		t.Fatalf("expected non-empty translated request from Gemini to Claude")
	}

	// 11. Test OpenAI Response -> Gemini Response
	openAIRespSample := []byte(`{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"created": 1677652288,
		"model": "gpt-4o",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "Hello! I am GPT-4o answering a Gemini client."
			},
			"finish_reason": "stop"
		}],
		"usage": {
			"prompt_tokens": 9,
			"completion_tokens": 12,
			"total_tokens": 21
		}
	}`)
	var geminiFromOpenAIParam any
	geminiRespFromOpenAI := TranslateResponseNonStream(context.Background(), "gemini", "openai", "gpt-4o", geminiReqSample, openaiFromGemini, openAIRespSample, &geminiFromOpenAIParam)
	t.Logf("OpenAI -> Gemini NonStream Response:\n%s", string(geminiRespFromOpenAI))
	if len(geminiRespFromOpenAI) == 0 {
		t.Fatalf("expected non-empty response translated from OpenAI to Gemini")
	}
	// 12. Test OpenAI Stream -> Gemini Stream
	openaiStreamChunk := []byte(`data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1694268190,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`)
	var geminiStreamParam any
	geminiChunks := TranslateResponseStream(context.Background(), "gemini", "openai", "gpt-4o", nil, nil, openaiStreamChunk, &geminiStreamParam)
	t.Logf("OpenAI -> Gemini Stream Chunks count: %d", len(geminiChunks))
	for i, c := range geminiChunks {
		t.Logf("  gemini chunk %d: %s", i, string(c))
	}

	openaiStopChunk := []byte(`data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1694268190,"model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)
	geminiStopChunks := TranslateResponseStream(context.Background(), "gemini", "openai", "gpt-4o", nil, nil, openaiStopChunk, &geminiStreamParam)
	t.Logf("OpenAI Stop -> Gemini Stream Chunks count: %d", len(geminiStopChunks))
	for i, c := range geminiStopChunks {
		t.Logf("  gemini stop chunk %d: %s", i, string(c))
	}

	openaiDoneChunk := []byte(`data: [DONE]`)
	geminiDoneChunks := TranslateResponseStream(context.Background(), "gemini", "openai", "gpt-4o", nil, nil, openaiDoneChunk, &geminiStreamParam)
	t.Logf("OpenAI [DONE] -> Gemini Stream Chunks count: %d", len(geminiDoneChunks))
	for i, c := range geminiDoneChunks {
		t.Logf("  gemini done chunk %d: %s", i, string(c))
	}

	// 13. Test Claude Response -> Gemini Response
	claudeRespSample := []byte(`{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"model": "claude-3-5-sonnet",
		"content": [{"type": "text", "text": "Hello Claude to Gemini!"}],
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 10, "output_tokens": 5}
	}`)
	var geminiFromClaudeParam any
	geminiRespFromClaude := TranslateResponseNonStream(context.Background(), "gemini", "anthropic", "claude-3-5-sonnet", geminiReqSample, claudeFromGemini, claudeRespSample, &geminiFromClaudeParam)
	t.Logf("Claude JSON -> Gemini NonStream Response:\n%s", string(geminiRespFromClaude))
}

func TestGeminiClaudeToolUseTranslation(t *testing.T) {
	claudeResp := []byte(`{
		"id": "msg_tool_123",
		"type": "message",
		"role": "assistant",
		"model": "claude-3-5-sonnet",
		"content": [
			{"type": "text", "text": "I will get the current weather for you."},
			{
				"type": "tool_use",
				"id": "toolu_01Test123",
				"name": "get_weather",
				"input": {
					"location": "San Francisco, CA",
					"unit": "celsius"
				}
			}
		],
		"stop_reason": "tool_use",
		"usage": {"input_tokens": 12, "output_tokens": 30}
	}`)

	var param any
	geminiResp := TranslateResponseNonStream(
		context.Background(),
		"gemini",
		"anthropic",
		"claude-3-5-sonnet",
		nil,
		nil,
		claudeResp,
		&param,
	)

	t.Logf("Converted Gemini Response:\n%s", string(geminiResp))

	var parsed struct {
		Candidates []struct {
			Content struct {
				Role  string `json:"role"`
				Parts []struct {
					Text         string `json:"text"`
					FunctionCall *struct {
						Name string                 `json:"name"`
						Args map[string]interface{} `json:"args"`
						ID   string                 `json:"id"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
	}

	if err := json.Unmarshal(geminiResp, &parsed); err != nil {
		t.Fatalf("failed to unmarshal gemini response: %v", err)
	}

	if len(parsed.Candidates) == 0 {
		t.Fatalf("expected at least one candidate, got 0")
	}

	parts := parsed.Candidates[0].Content.Parts
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts (text and functionCall), got %d: %+v", len(parts), parts)
	}

	if parts[0].Text != "I will get the current weather for you." {
		t.Errorf("part[0] text mismatch: got %q", parts[0].Text)
	}

	if parts[1].FunctionCall == nil {
		t.Fatalf("expected part[1] to have functionCall, got nil")
	}

	fc := parts[1].FunctionCall
	if fc.Name != "get_weather" {
		t.Errorf("functionCall name mismatch: got %q, want %q", fc.Name, "get_weather")
	}
	if fc.ID != "toolu_01Test123" {
		t.Errorf("functionCall id mismatch: got %q, want %q", fc.ID, "toolu_01Test123")
	}
	if loc, ok := fc.Args["location"].(string); !ok || loc != "San Francisco, CA" {
		t.Errorf("functionCall args location mismatch: got %v", fc.Args["location"])
	}
	if unit, ok := fc.Args["unit"].(string); !ok || unit != "celsius" {
		t.Errorf("functionCall args unit mismatch: got %v", fc.Args["unit"])
	}
}
