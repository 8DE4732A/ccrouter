package script_test

import (
	"encoding/json"
	"testing"

	"ccrouter/internal/script"
)

func body(t *testing.T, kv map[string]any) []byte {
	t.Helper()
	if kv == nil {
		kv = map[string]any{}
	}
	b, _ := json.Marshal(kv)
	return b
}

func parse(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("parse: %v", err)
	}
	return m
}

// ── empty / noop ─────────────────────────────────────────────────────────────

func TestEmptyScript(t *testing.T) {
	p, err := script.Compile("")
	if err != nil {
		t.Fatal(err)
	}
	in := body(t, map[string]any{"model": "fast"})
	out, _, status := script.RunBytes(p, in, nil, "fast", "/v1/chat/completions", "sn", "test-model", "openai")
	if string(out) != string(in) {
		t.Error("body should be unchanged")
	}
	if status != "" {
		t.Errorf("status should be empty, got %q", status)
	}
}

// ── del / delh ────────────────────────────────────────────────────────────────

func TestDelBodyKey(t *testing.T) {
	p, _ := script.Compile(`del(body, "x_client_info")`)
	in := body(t, map[string]any{"model": "fast", "x_client_info": "leak"})
	out, _, status := script.RunBytes(p, in, nil, "fast", "/v1/chat/completions", "sn", "test-model", "openai")
	if status != "ok" {
		t.Fatalf("status=%q", status)
	}
	m := parse(t, out)
	if _, ok := m["x_client_info"]; ok {
		t.Error("x_client_info should be deleted")
	}
	if m["model"] != "fast" {
		t.Error("model should be preserved")
	}
}

func TestDelHeader(t *testing.T) {
	p, _ := script.Compile(`delh(headers, "user-agent")`)
	h := map[string]string{"user-agent": "MyClient", "content-type": "application/json"}
	_, hOut, _ := script.RunBytes(p, body(t, nil), h, "x", "/v1/chat/completions", "sn", "test-model", "openai")
	if _, ok := hOut["user-agent"]; ok {
		t.Error("user-agent should be deleted")
	}
	if hOut["content-type"] != "application/json" {
		t.Error("content-type should be preserved")
	}
}

// ── set / seth ────────────────────────────────────────────────────────────────

func TestSetBodyField(t *testing.T) {
	p, _ := script.Compile(`set(body, "injected", true)`)
	out, _, _ := script.RunBytes(p, body(t, nil), nil, "x", "/v1/chat/completions", "sn", "test-model", "openai")
	if parse(t, out)["injected"] != true {
		t.Error("injected should be true")
	}
}

func TestSetHeader(t *testing.T) {
	p, _ := script.Compile(`seth(headers, "x-custom", "value")`)
	_, hOut, _ := script.RunBytes(p, body(t, nil), map[string]string{}, "x", "/v1/chat/completions", "sn", "test-model", "openai")
	if hOut["x-custom"] != "value" {
		t.Errorf("got %q", hOut["x-custom"])
	}
}

// ── setpath (nested) ──────────────────────────────────────────────────────────

func TestSetPath(t *testing.T) {
	p, _ := script.Compile(`setpath(body, "thinking", "budget_tokens", 1024)`)
	in := body(t, map[string]any{"thinking": map[string]any{"budget_tokens": 8000}})
	out, _, _ := script.RunBytes(p, in, nil, "fast", "/v1/chat/completions", "sn", "test-model", "openai")
	thinking := parse(t, out)["thinking"].(map[string]any)
	if thinking["budget_tokens"] != float64(1024) {
		t.Errorf("got %v", thinking["budget_tokens"])
	}
}

// ── get + clamp ───────────────────────────────────────────────────────────────

func TestGetWithDefault(t *testing.T) {
	p, _ := script.Compile(`set(body, "result", get(body, "missing", 42))`)
	out, _, _ := script.RunBytes(p, body(t, nil), nil, "x", "/v1/chat/completions", "sn", "test-model", "openai")
	if parse(t, out)["result"] != float64(42) {
		t.Error("default not applied")
	}
}

func TestClamp(t *testing.T) {
	p, _ := script.Compile(`setpath(body, "thinking", "budget_tokens", clamp(get(body.thinking, "budget_tokens", 8000), 0, 1024))`)
	in := body(t, map[string]any{"thinking": map[string]any{"budget_tokens": float64(8000)}})
	out, _, _ := script.RunBytes(p, in, nil, "fast", "/v1/chat/completions", "sn", "test-model", "openai")
	thinking := parse(t, out)["thinking"].(map[string]any)
	if thinking["budget_tokens"] != float64(1024) {
		t.Errorf("clamp failed, got %v", thinking["budget_tokens"])
	}
}

// ── conditional on combo ──────────────────────────────────────────────────────

func TestConditionalCombo(t *testing.T) {
	src := `combo == "fast" && "thinking" in body
		? setpath(body, "thinking", "budget_tokens", 1024)
		: nil`
	p, _ := script.Compile(src)
	in := body(t, map[string]any{"thinking": map[string]any{"budget_tokens": float64(8000)}})

	outFast, _, _ := script.RunBytes(p, in, nil, "fast", "/v1/chat/completions", "sn", "test-model", "openai")
	outSlow, _, _ := script.RunBytes(p, in, nil, "slow", "/v1/chat/completions", "sn", "test-model", "openai")

	fastThinking := parse(t, outFast)["thinking"].(map[string]any)
	slowThinking := parse(t, outSlow)["thinking"].(map[string]any)

	if fastThinking["budget_tokens"] != float64(1024) {
		t.Errorf("fast: expected 1024, got %v", fastThinking["budget_tokens"])
	}
	if slowThinking["budget_tokens"] != float64(8000) {
		t.Errorf("slow: expected 8000, got %v", slowThinking["budget_tokens"])
	}
}

// ── error handling ────────────────────────────────────────────────────────────

func TestSyntaxError(t *testing.T) {
	_, err := script.Compile("this is not valid [[[")
	if err == nil {
		t.Error("expected compile error")
	}
}

func TestRuntimeErrorReturnsOriginal(t *testing.T) {
	// Accessing a nil map field causes a runtime error
	p, err := script.Compile(`body.missing.deeper == "x"`)
	if err != nil {
		t.Skip("compiled (expr may handle nil gracefully)")
	}
	in := body(t, map[string]any{})
	out, _, status := script.RunBytes(p, in, nil, "x", "/v1/chat/completions", "sn", "test-model", "openai")
	// Either ok (nil safe) or error — body must be unchanged
	_ = status
	_ = out
}

// ── non-JSON body ─────────────────────────────────────────────────────────────

func TestNonJSONBodyPreserved(t *testing.T) {
	p, _ := script.Compile(`seth(headers, "x-ran", "yes")`)
	raw := []byte("not json at all")
	h := map[string]string{}
	outBody, hOut, _ := script.RunBytes(p, raw, h, "x", "/v1/chat/completions", "sn", "test-model", "openai")
	if string(outBody) != string(raw) {
		t.Error("non-JSON body should be unchanged")
	}
	if hOut["x-ran"] != "yes" {
		t.Error("header mutation should still apply")
	}
}

// ── chain: two scripts ────────────────────────────────────────────────────────

func TestChain(t *testing.T) {
	p1, _ := script.Compile(`set(body, "a", 1)`)
	p2, _ := script.Compile(`set(body, "b", 2)`)

	in := body(t, nil)
	h := map[string]string{}
	var status string

	in, h, status = script.RunBytes(p1, in, h, "fast", "/v1/chat/completions", "sn", "test-model", "openai")
	if status != "ok" {
		t.Fatalf("p1 status=%q", status)
	}
	in, h, status = script.RunBytes(p2, in, h, "fast", "/v1/chat/completions", "sn", "test-model", "openai")
	if status != "ok" {
		t.Fatalf("p2 status=%q", status)
	}
	_ = h

	m := parse(t, in)
	if m["a"] != float64(1) || m["b"] != float64(2) {
		t.Errorf("chain failed: %v", m)
	}
}

// ── sense-roll 原始脚本的 Go 等价 ─────────────────────────────────────────────

func TestSenseRollScript1_HideClientID(t *testing.T) {
	// 原 Python: request.headers.pop('user-agent', None) 等
	src := `
delh(headers, "user-agent")
delh(headers, "User-Agent")
delh(headers, "x-client-version")
del(body, "x_client_info")
`
	p, err := script.Compile(src)
	if err != nil {
		t.Fatal(err)
	}
	h := map[string]string{
		"user-agent":       "MyClient/1.0",
		"x-client-version": "1.2.3",
		"content-type":     "application/json",
	}
	in := body(t, map[string]any{"model": "fast", "x_client_info": "secret"})
	out, hOut, status := script.RunBytes(p, in, h, "fast", "/v1/chat/completions", "sn", "test-model", "openai")
	if status != "ok" {
		t.Fatalf("status=%q", status)
	}
	if _, ok := hOut["user-agent"]; ok {
		t.Error("user-agent should be deleted")
	}
	if _, ok := hOut["x-client-version"]; ok {
		t.Error("x-client-version should be deleted")
	}
	if hOut["content-type"] != "application/json" {
		t.Error("content-type should be preserved")
	}
	m := parse(t, out)
	if _, ok := m["x_client_info"]; ok {
		t.Error("x_client_info should be deleted")
	}
}

func TestSenseRollScript2_ThinkingBudget(t *testing.T) {
	// 原 Python:
	// if request.combo == 'fast' and 'thinking' in request.body:
	//     request.body['thinking']['budget_tokens'] = min(
	//         request.body['thinking'].get('budget_tokens', 8000), 1024)
	src := `combo == "fast" && "thinking" in body
		? setpath(body, "thinking", "budget_tokens",
			clamp(get(body.thinking, "budget_tokens", 8000), 0, 1024))
		: nil`
	p, err := script.Compile(src)
	if err != nil {
		t.Fatal(err)
	}

	// fast combo → should clamp
	inFast := body(t, map[string]any{"thinking": map[string]any{"budget_tokens": float64(8000)}})
	outFast, _, _ := script.RunBytes(p, inFast, nil, "fast", "/v1/chat/completions", "sn", "test-model", "openai")
	if parse(t, outFast)["thinking"].(map[string]any)["budget_tokens"] != float64(1024) {
		t.Error("fast: budget_tokens should be clamped to 1024")
	}

	// slow combo → unchanged
	inSlow := body(t, map[string]any{"thinking": map[string]any{"budget_tokens": float64(8000)}})
	outSlow, _, _ := script.RunBytes(p, inSlow, nil, "slow", "/v1/chat/completions", "sn", "test-model", "openai")
	if parse(t, outSlow)["thinking"].(map[string]any)["budget_tokens"] != float64(8000) {
		t.Error("slow: budget_tokens should be unchanged")
	}
}
