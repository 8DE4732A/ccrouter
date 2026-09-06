package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"ccrouter/internal/combos"
	"ccrouter/internal/config"
	"ccrouter/internal/db"
	"ccrouter/internal/keys"
)

func healthRule(matchValue, matchType string) config.HealthCheckRule {
	return config.HealthCheckRule{
		Description:     "quota",
		JSONPath:        "$.error.type",
		MatchValue:      matchValue,
		MatchType:       matchType,
		Action:          "rotate",
		CooldownSeconds: 60,
	}
}

func mustBuildService(t *testing.T, keysByProvider map[string][]string,
	comboMembers [][2]string, comboStrategy string, maxRetries int, upstreamURL string) *Service {
	t.Helper()
	cfg := &config.AppConfig{}
	for name, ks := range keysByProvider {
		p := config.ProviderConfig{
			Name:             name,
			APIs:             []config.ApiEndpoint{{APIFormat: "openai", BaseURL: upstreamURL}},
			MaxRetries:       maxRetries,
			KeyStrategy:      "fill-first",
			HealthCheckRules: []config.HealthCheckRule{healthRule("quota_exceeded_error", "equals")},
		}
		for _, k := range ks {
			p.Keys = append(p.Keys, config.KeyConfig{Key: k})
		}
		cfg.Providers = append(cfg.Providers, p)
	}
	c := config.ComboConfig{Name: "my-combo", APIFormat: "openai", Strategy: comboStrategy}
	for _, m := range comboMembers {
		c.Members = append(c.Members, config.ComboMember{Provider: m[0], Model: m[1]})
	}
	cfg.Combos = []config.ComboConfig{c}

	kms := map[string]*keys.Manager{}
	for name, ks := range keysByProvider {
		kms[name] = keys.NewManager(name, ks, "fill-first")
	}
	svc, err := New(cfg, kms, combos.NewRouter(cfg.Combos), map[string]*http.Client{}, nil, nil)
	if err != nil {
		t.Fatalf("build service: %v", err)
	}
	return svc
}

func doRequest(t *testing.T, svc *Service, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return doRequestFmt(t, svc, body, "openai")
}

func doRequestFmt(t *testing.T, svc *Service, body map[string]any, apiFormat string) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	svc.Handle(rec, req, apiFormat, false)
	return rec
}

func TestUnknownComboReturns400(t *testing.T) {
	svc := mustBuildService(t, map[string][]string{"sn": {"key-1"}}, [][2]string{{"sn", "m"}}, "fill-first", 3, "http://x")
	rec := doRequest(t, svc, map[string]any{"model": "ghost"})
	if rec.Code != 400 {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestNonStreamingRotatesKeyOnQuotaError(t *testing.T) {
	var calls []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Header.Get("Authorization"))
		if r.Header.Get("Authorization") == "Bearer key-1" {
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"error":{"type":"quota_exceeded_error"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer up.Close()
	svc := mustBuildService(t, map[string][]string{"sn": {"key-1", "key-2"}}, [][2]string{{"sn", "m"}}, "fill-first", 3, up.URL)
	rec := doRequest(t, svc, map[string]any{"model": "my-combo"})
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(calls) != 2 || calls[0] != "Bearer key-1" || calls[1] != "Bearer key-2" {
		t.Fatalf("expected rotation key-1→key-2, calls=%v", calls)
	}
}

func TestModelRewritten(t *testing.T) {
	var received []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		received = append(received, body["model"].(string))
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer up.Close()
	svc := mustBuildService(t, map[string][]string{"sn": {"key-1"}}, [][2]string{{"sn", "test-model"}}, "fill-first", 3, up.URL)
	doRequest(t, svc, map[string]any{"model": "my-combo"})
	if len(received) != 1 || received[0] != "test-model" {
		t.Fatalf("expected model rewrite to test-model, got %v", received)
	}
}

func TestAllKeysExhaustedReturns503(t *testing.T) {
	calls := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":{"type":"quota_exceeded_error"}}`))
	}))
	defer up.Close()
	svc := mustBuildService(t, map[string][]string{"sn": {"key-1"}}, [][2]string{{"sn", "m"}}, "fill-first", 1, up.URL)
	rec := doRequest(t, svc, map[string]any{"model": "my-combo"})
	if rec.Code != 503 {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
	if calls != 1 {
		t.Fatalf("expected 1 upstream call, got %d", calls)
	}
}

func TestFallbackToSecondMember(t *testing.T) {
	var calls []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Header.Get("Authorization"))
		if r.Header.Get("Authorization") == "Bearer key-sn" {
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"error":{"type":"quota_exceeded_error"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer up.Close()
	svc := mustBuildService(t,
		map[string][]string{"sn": {"key-sn"}, "ds": {"key-ds"}},
		[][2]string{{"sn", "flash"}, {"ds", "chat"}}, "fill-first", 3, up.URL)
	rec := doRequest(t, svc, map[string]any{"model": "my-combo"})
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(strings.Join(calls, ","), "Bearer key-sn") {
		t.Fatalf("expected key-sn attempted, calls=%v", calls)
	}
	if !strings.Contains(strings.Join(calls, ","), "Bearer key-ds") {
		t.Fatalf("expected key-ds attempted, calls=%v", calls)
	}
}

func TestStreamingRotatesOnJSONErrorResponse(t *testing.T) {
	var calls []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Header.Get("Authorization"))
		if r.Header.Get("Authorization") == "Bearer key-1" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"error":{"type":"quota_exceeded_error"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"ok": true}

`))
	}))
	defer up.Close()
	svc := mustBuildService(t, map[string][]string{"sn": {"key-1", "key-2"}}, [][2]string{{"sn", "m"}}, "fill-first", 3, up.URL)
	rec := doRequest(t, svc, map[string]any{"model": "my-combo", "stream": true})
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "data: {\"ok\": true}") {
		t.Fatalf("unexpected body: %q", rec.Body.String())
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %v", calls)
	}
}

func TestResponseFiltersCompressionHeaders(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Length", "10")
		w.Header().Set("X-Custom-Header", "test-value")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer up.Close()
	svc := mustBuildService(t, map[string][]string{"sn": {"key-1"}}, [][2]string{{"sn", "m"}}, "fill-first", 3, up.URL)
	rec := doRequest(t, svc, map[string]any{"model": "my-combo"})
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatalf("expected content-encoding filtered, got %q", rec.Header().Get("Content-Encoding"))
	}
	if rec.Header().Get("X-Custom-Header") != "test-value" {
		t.Fatalf("expected x-custom-header preserved, got %q", rec.Header().Get("X-Custom-Header"))
	}
}

func TestQuotaRuleWithModelFilterDoesNotAffectOtherModel(t *testing.T) {
	calls := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] == "flash" {
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"error":{"type":"quota_exceeded_error"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer up.Close()

	// Rule only applies to model "flash"
	rule := config.HealthCheckRule{
		Description:     "flash quota",
		JSONPath:        "$.error.type",
		MatchValue:      "quota_exceeded_error",
		MatchType:       "equals",
		Action:          "rotate",
		CooldownSeconds: 3600,
		Models:          []string{"flash"},
	}
	cfg := &config.AppConfig{
		Providers: []config.ProviderConfig{{
			Name:             "sn",
			APIs:             []config.ApiEndpoint{{APIFormat: "openai", BaseURL: up.URL}},
			MaxRetries:       1,
			KeyStrategy:      "fill-first",
			Keys:             []config.KeyConfig{{Key: "key-1"}},
			HealthCheckRules: []config.HealthCheckRule{rule},
		}},
		Combos: []config.ComboConfig{
			{Name: "flash-combo", APIFormat: "openai", Strategy: "fill-first",
				Members: []config.ComboMember{{Provider: "sn", Model: "flash"}}},
			{Name: "other-combo", APIFormat: "openai", Strategy: "fill-first",
				Members: []config.ComboMember{{Provider: "sn", Model: "other"}}},
		},
	}
	kms := map[string]*keys.Manager{
		"sn": keys.NewManager("sn", []string{"key-1"}, "fill-first"),
	}
	svc, err := New(cfg, kms, combos.NewRouter(cfg.Combos), map[string]*http.Client{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// flash-combo: quota error → key cooling for flash → 503
	recFlash := doRequest(t, svc, map[string]any{"model": "flash-combo"})
	if recFlash.Code != 503 {
		t.Fatalf("flash-combo: expected 503, got %d", recFlash.Code)
	}

	// other-combo: key NOT in cooldown for "other" → 200
	recOther := doRequest(t, svc, map[string]any{"model": "other-combo"})
	if recOther.Code != 200 {
		t.Fatalf("other-combo: expected 200, got %d body=%s", recOther.Code, recOther.Body.String())
	}
}

func TestHTTPStatusCodeRuleRotatesOn429(t *testing.T) {
	// Simulates the real-world case where the upstream returns 429 with a body
	// that has no usable error field (e.g. sensenova wrapping a completed
	// response object). The http_status_codes rule must still trigger rotation.
	var calls []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Header.Get("Authorization"))
		if r.Header.Get("Authorization") == "Bearer key-1" {
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"body":{"status":"completed","error":null},"status_code":429}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer up.Close()

	cfg := &config.AppConfig{
		Providers: []config.ProviderConfig{{
			Name:             "sn",
			APIs:             []config.ApiEndpoint{{APIFormat: "openai", BaseURL: up.URL}},
			MaxRetries:       3,
			KeyStrategy:      "fill-first",
			Keys:             []config.KeyConfig{{Key: "key-1"}, {Key: "key-2"}},
			HealthCheckRules: []config.HealthCheckRule{{
				Description:     "any 429",
				HTTPStatusCodes: []int{429},
				Action:          "rotate",
				CooldownSeconds: 60,
			}},
		}},
		Combos: []config.ComboConfig{{
			Name: "my-combo", APIFormat: "openai", Strategy: "fill-first",
			Members: []config.ComboMember{{Provider: "sn", Model: "m"}},
		}},
	}
	kms := map[string]*keys.Manager{
		"sn": keys.NewManager("sn", []string{"key-1", "key-2"}, "fill-first"),
	}
	svc, err := New(cfg, kms, combos.NewRouter(cfg.Combos), map[string]*http.Client{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	rec := doRequest(t, svc, map[string]any{"model": "my-combo"})
	if rec.Code != 200 {
		t.Fatalf("expected 200 after rotation, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(calls) != 2 || calls[0] != "Bearer key-1" || calls[1] != "Bearer key-2" {
		t.Fatalf("expected rotation key-1→key-2, calls=%v", calls)
	}
}

func TestBuildErrorBodyFormats(t *testing.T) {
	openAI := buildErrorBody("openai", 429, "rate limited")
	var oa map[string]any
	if err := json.Unmarshal(openAI, &oa); err != nil {
		t.Fatalf("openai error body not valid JSON: %v", err)
	}
	e, _ := oa["error"].(map[string]any)
	if e == nil || e["message"] != "rate limited" || e["type"] != "proxy_error" {
		t.Fatalf("unexpected openai error body: %s", openAI)
	}

	anthropic := buildErrorBody("anthropic", 429, "rate limited")
	var an map[string]any
	if err := json.Unmarshal(anthropic, &an); err != nil {
		t.Fatalf("anthropic error body not valid JSON: %v", err)
	}
	ae, _ := an["error"].(map[string]any)
	if ae == nil || ae["message"] != "rate limited" || ae["type"] != "error" {
		t.Fatalf("unexpected anthropic error body: %s", anthropic)
	}

	responses := buildErrorBody("openai-responses", 429, "")
	var rr map[string]any
	if err := json.Unmarshal(responses, &rr); err != nil {
		t.Fatalf("openai-responses error body not valid JSON: %v", err)
	}
	re, _ := rr["error"].(map[string]any)
	if re == nil || re["message"] == "" || re["code"] != float64(429) {
		t.Fatalf("unexpected openai-responses error body: %s", responses)
	}
}

// TestMaskSecretScalesWithLength verifies that maskSecret doesn't leak most of
// a short token (fixed-length truncation would expose short keys entirely)
// and still redacts long tokens down to a small prefix.
func TestMaskSecretScalesWithLength(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"short bearer token", "Bearer sk-1"},
		{"short raw key", "sk-abc"},
		{"long bearer token", "Bearer sk-1234567890abcdef1234567890abcdef"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			masked := maskSecret(tc.input)
			if masked == tc.input {
				t.Fatalf("expected masking to change the value, got unchanged %q", masked)
			}
			if !strings.Contains(masked, "*") {
				t.Fatalf("expected masked value to contain redaction marker, got %q", masked)
			}
			// The masked value must never reveal the full secret/token portion.
			scheme := ""
			token := tc.input
			if i := strings.IndexByte(tc.input, ' '); i > 0 {
				scheme = tc.input[:i+1]
				token = tc.input[i+1:]
			}
			if strings.Contains(masked, token) {
				t.Fatalf("masked value leaks the full token: %q (masked=%q)", token, masked)
			}
			if scheme != "" && !strings.HasPrefix(masked, scheme) {
				t.Fatalf("expected scheme prefix %q preserved, got %q", scheme, masked)
			}
		})
	}
}

func TestHeaderToMapMasksSensitiveHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer sk-secret-12345")
	h.Set("X-Api-Key", "sk-ant-api03-abcdefg")
	h.Set("x-goog-api-key", "AIzaSySecretGoogleKey123")
	h.Set("api-key", "azure-secret-key")
	h.Set("Content-Type", "application/json")

	m := headerToMap(h)
	if m["Content-Type"] != "application/json" {
		t.Fatalf("expected Content-Type to remain unmasked, got %q", m["Content-Type"])
	}
	for _, key := range []string{"Authorization", "X-Api-Key", "x-goog-api-key", "api-key"} {
		v := m[http.CanonicalHeaderKey(key)]
		if !strings.Contains(v, "*") {
			t.Fatalf("expected header %q to be masked, got %q", key, v)
		}
		if strings.Contains(v, "12345") || strings.Contains(v, "abcdefg") || strings.Contains(v, "SecretGoogleKey") {
			t.Fatalf("expected secret in %q to be redacted, got %q", key, v)
		}
	}
}

// TestErrorResponseNotSilentlyEmptiedByTranslator verifies that when the client
// format differs from the upstream format and the upstream returns an HTTP error
// (e.g. 500 with an OpenAI-shaped {"error": {...}} body), the proxy does NOT run
// the body through the upstream→client format translator. Format translators are
// built for success-shaped bodies (they look for "choices", etc.) and, given an
// error body, can silently produce a well-formed but semantically empty message
// (empty content, no stop reason) instead of failing — which would hide the real
// error message from the client. The proxy must always construct the error
// directly in the client's format so the message survives.
func TestErrorResponseNotSilentlyEmptiedByTranslator(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"error":{"message":"upstream exploded","type":"server_error"}}`))
	}))
	defer up.Close()

	cfg := &config.AppConfig{
		Providers: []config.ProviderConfig{{
			Name:        "op",
			APIs:        []config.ApiEndpoint{{APIFormat: "openai", BaseURL: up.URL}},
			MaxRetries:  0,
			KeyStrategy: "fill-first",
			Keys:        []config.KeyConfig{{Key: "key-1"}},
			// No health check rules — this test is only about error body handling,
			// not rotation.
		}},
		Combos: []config.ComboConfig{{
			// Client speaks anthropic; upstream provider only has an "openai" endpoint,
			// so the request/response is translated anthropic<->openai.
			Name: "my-combo", APIFormat: "anthropic", Strategy: "fill-first",
			Members: []config.ComboMember{{Provider: "op", Model: "m"}},
		}},
	}
	kms := map[string]*keys.Manager{
		"op": keys.NewManager("op", []string{"key-1"}, "fill-first"),
	}
	svc, err := New(cfg, kms, combos.NewRouter(cfg.Combos), map[string]*http.Client{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	rec := doRequestFmt(t, svc, map[string]any{
		"model":      "my-combo",
		"messages":   []map[string]any{{"role": "user", "content": "hi"}},
		"max_tokens": 100,
	}, "anthropic")

	if rec.Code != 500 {
		t.Fatalf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response not valid JSON: %v body=%s", err, rec.Body.String())
	}
	errObj, _ := got["error"].(map[string]any)
	if errObj == nil {
		t.Fatalf("expected anthropic-style error object, got %s", rec.Body.String())
	}
	msg, _ := errObj["message"].(string)
	if !strings.Contains(msg, "upstream exploded") {
		t.Fatalf("expected error message to contain original upstream error text, got %q (full body=%s)", msg, rec.Body.String())
	}
	// Must NOT look like a translated (empty) success message.
	if _, hasContent := got["content"]; hasContent {
		t.Fatalf("error response should not contain a 'content' field (that would mean the translator ran on the error body): %s", rec.Body.String())
	}
}

func TestBuildHeadersMirrorsClientAuthScheme(t *testing.T) {
	newReq := func(setXAPIKey, setBearer bool) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
		if setXAPIKey {
			r.Header.Set("X-Api-Key", "client-original-key")
		}
		if setBearer {
			r.Header.Set("Authorization", "Bearer client-original-token")
		}
		return r
	}

	t.Run("client sent only x-api-key: upstream gets x-api-key with real key, no Authorization", func(t *testing.T) {
		h := buildHeaders(newReq(true, false), "real-upstream-key", "anthropic")
		if got := h.Get("X-Api-Key"); got != "real-upstream-key" {
			t.Errorf("X-Api-Key = %q, want %q", got, "real-upstream-key")
		}
		if got := h.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want empty", got)
		}
	})

	t.Run("client sent only Bearer: upstream gets Bearer with real key, no x-api-key", func(t *testing.T) {
		h := buildHeaders(newReq(false, true), "real-upstream-key", "anthropic")
		if got := h.Get("Authorization"); got != "Bearer real-upstream-key" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer real-upstream-key")
		}
		if got := h.Get("X-Api-Key"); got != "" {
			t.Errorf("X-Api-Key = %q, want empty", got)
		}
	})

	t.Run("client sent both: upstream gets both with real key", func(t *testing.T) {
		h := buildHeaders(newReq(true, true), "real-upstream-key", "anthropic")
		if got := h.Get("X-Api-Key"); got != "real-upstream-key" {
			t.Errorf("X-Api-Key = %q, want %q", got, "real-upstream-key")
		}
		if got := h.Get("Authorization"); got != "Bearer real-upstream-key" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer real-upstream-key")
		}
	})

	t.Run("client sent neither: falls back to per-format default (anthropic -> x-api-key)", func(t *testing.T) {
		h := buildHeaders(newReq(false, false), "real-upstream-key", "anthropic")
		if got := h.Get("X-Api-Key"); got != "real-upstream-key" {
			t.Errorf("X-Api-Key = %q, want %q", got, "real-upstream-key")
		}
		if got := h.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want empty", got)
		}
	})

	t.Run("client sent neither: falls back to per-format default (openai -> Bearer)", func(t *testing.T) {
		h := buildHeaders(newReq(false, false), "real-upstream-key", "openai")
		if got := h.Get("Authorization"); got != "Bearer real-upstream-key" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer real-upstream-key")
		}
		if got := h.Get("X-Api-Key"); got != "" {
			t.Errorf("X-Api-Key = %q, want empty", got)
		}
	})

	t.Run("gemini: no auth header set even if client sent x-api-key/Bearer", func(t *testing.T) {
		h := buildHeaders(newReq(true, true), "real-upstream-key", "gemini")
		if got := h.Get("X-Api-Key"); got != "" {
			t.Errorf("X-Api-Key = %q, want empty", got)
		}
		if got := h.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want empty", got)
		}
	})
}

func TestProxyOwnedByRouting(t *testing.T) {
	var requestedUpstreamModel string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if m, ok := req["model"].(string); ok {
			requestedUpstreamModel = m
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer up.Close()

	cfg := &config.AppConfig{
		Providers: []config.ProviderConfig{{
			Name:        "p1",
			APIs:        []config.ApiEndpoint{{APIFormat: "openai", BaseURL: up.URL}},
			KeyStrategy: "fill-first",
			Keys:        []config.KeyConfig{{Key: "key-1"}},
		}},
		Combos: []config.ComboConfig{
			{
				Name:      "fast",
				OwnedBy:   "default",
				IsDefault: true,
				APIFormat: "openai",
				Strategy:  "fill-first",
				Members:   []config.ComboMember{{Provider: "p1", Model: "model-default"}},
			},
			{
				Name:      "fast",
				OwnedBy:   "team-a",
				IsDefault: false,
				APIFormat: "openai",
				Strategy:  "fill-first",
				Members:   []config.ComboMember{{Provider: "p1", Model: "model-teama"}},
			},
		},
	}

	kms := map[string]*keys.Manager{
		"p1": keys.NewManager("p1", []string{"key-1"}, "fill-first"),
	}
	svc, err := New(cfg, kms, combos.NewRouter(cfg.Combos), map[string]*http.Client{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Request to default combo "fast" (no prefix)
	rec := doRequest(t, svc, map[string]any{"model": "fast", "messages": []map[string]any{{"role": "user", "content": "hi"}}})
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if requestedUpstreamModel != "model-default" {
		t.Fatalf("expected model-default, got %q", requestedUpstreamModel)
	}

	// 2. Request to team-a combo "team-a/fast" (with prefix)
	rec = doRequest(t, svc, map[string]any{"model": "team-a/fast", "messages": []map[string]any{{"role": "user", "content": "hi"}}})
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if requestedUpstreamModel != "model-teama" {
		t.Fatalf("expected model-teama, got %q", requestedUpstreamModel)
	}

	// 3. Request to unknown combo "team-b/fast" -> 400
	rec = doRequest(t, svc, map[string]any{"model": "team-b/fast", "messages": []map[string]any{{"role": "user", "content": "hi"}}})
	if rec.Code != 400 {
		t.Fatalf("expected 400 for unknown combo, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestEmbeddingsProxy(t *testing.T) {
	var receivedPath string
	var receivedAuth string
	var receivedBody map[string]any

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"object": "list",
			"data": [
				{"object": "embedding", "index": 0, "embedding": [0.01, 0.02, 0.03]}
			],
			"model": "text-embedding-3-small",
			"usage": {
				"prompt_tokens": 5,
				"total_tokens": 5
			}
		}`))
	}))
	defer upstream.Close()

	cfg := &config.AppConfig{
		Providers: []config.ProviderConfig{
			{
				Name:        "sn-emb",
				APIs:        []config.ApiEndpoint{{APIFormat: "openai-embeddings", BaseURL: upstream.URL}},
				MaxRetries:  1,
				KeyStrategy: "fill-first",
				Keys:        []config.KeyConfig{{Key: "sk-emb-key"}},
			},
		},
		Combos: []config.ComboConfig{
			{
				Name:      "emb-combo",
				APIFormat: "openai-embeddings",
				Strategy:  "fill-first",
				Members: []config.ComboMember{
					{Provider: "sn-emb", Model: "text-embedding-3-small"},
				},
			},
		},
	}

	kms := map[string]*keys.Manager{
		"sn-emb": keys.NewManager("sn-emb", []string{"sk-emb-key"}, "fill-first"),
	}
	recRecorder, _ := db.NewRecorder(filepath.Join(t.TempDir(), "test.db"))
	svc, err := New(cfg, kms, combos.NewRouter(cfg.Combos), map[string]*http.Client{}, recRecorder, nil)
	if err != nil {
		t.Fatalf("New service: %v", err)
	}

	reqBody := `{"model":"emb-combo","input":"test embedding"}`
	req := httptest.NewRequest("POST", "/v1/embeddings", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer client-key")
	w := httptest.NewRecorder()

	svc.Handle(w, req, "openai-embeddings", true)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if receivedPath != "/embeddings" {
		t.Fatalf("expected upstream path /embeddings, got %q", receivedPath)
	}
	if receivedAuth != "Bearer sk-emb-key" {
		t.Fatalf("expected upstream auth Bearer sk-emb-key, got %q", receivedAuth)
	}
	if receivedBody["model"] != "text-embedding-3-small" {
		t.Fatalf("expected model to be rewritten to text-embedding-3-small, got %v", receivedBody["model"])
	}
	if !strings.Contains(w.Body.String(), "0.01, 0.02, 0.03") {
		t.Fatalf("unexpected response body: %s", w.Body.String())
	}
}

func TestExtractModelAndRewriteMultipart(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("model", "original-combo")
	_ = mw.WriteField("prompt", "draw a hat on cat")
	part, err := mw.CreateFormFile("image", "cat.png")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	_, _ = part.Write([]byte("fake-png-data"))
	_ = mw.Close()

	ct := mw.FormDataContentType()
	if !isMultipart(ct) {
		t.Fatalf("expected isMultipart to be true for %s", ct)
	}

	gotModel := extractModel(buf.Bytes(), ct)
	if gotModel != "original-combo" {
		t.Fatalf("expected original-combo, got %q", gotModel)
	}

	rewritten, newCT, err := rewriteModelMultipart(buf.Bytes(), ct, "upstream-model")
	if err != nil {
		t.Fatalf("rewriteModelMultipart failed: %v", err)
	}
	if !isMultipart(newCT) {
		t.Fatalf("expected newCT to be multipart, got %s", newCT)
	}

	// Verify rewritten body model
	newModel := extractModel(rewritten, newCT)
	if newModel != "upstream-model" {
		t.Fatalf("expected upstream-model after rewrite, got %q", newModel)
	}

	// Verify other fields & file still exist
	_, params, _ := mime.ParseMediaType(newCT)
	mr := multipart.NewReader(bytes.NewReader(rewritten), params["boundary"])
	foundPrompt := false
	foundImage := false
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("reading rewritten part: %v", err)
		}
		if p.FormName() == "prompt" {
			val, _ := io.ReadAll(p)
			if string(val) == "draw a hat on cat" {
				foundPrompt = true
			}
		}
		if p.FormName() == "image" && p.FileName() == "cat.png" {
			val, _ := io.ReadAll(p)
			if string(val) == "fake-png-data" {
				foundImage = true
			}
		}
	}
	if !foundPrompt || !foundImage {
		t.Fatalf("rewritten multipart lost data: prompt=%v, image=%v", foundPrompt, foundImage)
	}
}

func TestSanitizeBodyForLog(t *testing.T) {
	// Multipart sanitization
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("model", "dall-e-2")
	_ = mw.WriteField("prompt", "a hat")
	part, _ := mw.CreateFormFile("image", "sample.png")
	_, _ = part.Write([]byte("1234567890"))
	_ = mw.Close()

	sanitized := sanitizeBodyForLog(buf.Bytes(), mw.FormDataContentType())
	smap, ok := sanitized.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", sanitized)
	}
	if smap["_type"] != "multipart/form-data" {
		t.Fatalf("expected _type multipart/form-data, got %v", smap["_type"])
	}
	fields := smap["fields"].(map[string]any)
	if fields["prompt"] != "a hat" || fields["model"] != "dall-e-2" {
		t.Fatalf("unexpected fields: %v", fields)
	}
	files := smap["files"].([]map[string]any)
	if len(files) != 1 || files[0]["field"] != "image" || files[0]["filename"] != "sample.png" || files[0]["size_bytes"] != int64(10) {
		t.Fatalf("unexpected files summary: %v", files)
	}

	// JSON response b64_json truncation
	hugeB64 := strings.Repeat("A", 300)
	respJSON := fmt.Sprintf(`{"data":[{"b64_json":"%s"}]}`, hugeB64)
	sanitizedResp := sanitizeResponseBodyForLog([]byte(respJSON))
	respMap := sanitizedResp.(map[string]any)
	dataArr := respMap["data"].([]any)
	firstItem := dataArr[0].(map[string]any)
	b64Val := firstItem["b64_json"].(string)
	if !strings.Contains(b64Val, "... [truncated 300 chars]") {
		t.Fatalf("expected b64_json to be truncated, got %q", b64Val)
	}
}

func TestImageEditsProxyMultipart(t *testing.T) {
	var receivedPath string
	var receivedAuth string
	var receivedModel string
	var receivedPrompt string
	var receivedFileBytes []byte

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedAuth = r.Header.Get("Authorization")
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err == nil && strings.HasPrefix(mediaType, "multipart/") {
			mr := multipart.NewReader(r.Body, params["boundary"])
			for {
				p, err := mr.NextPart()
				if err == io.EOF {
					break
				}
				if err != nil {
					break
				}
				if p.FormName() == "model" {
					val, _ := io.ReadAll(p)
					receivedModel = string(val)
				} else if p.FormName() == "prompt" {
					val, _ := io.ReadAll(p)
					receivedPrompt = string(val)
				} else if p.FormName() == "image" {
					receivedFileBytes, _ = io.ReadAll(p)
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"created": 1589478378,
			"data": [
				{"url": "https://upstream.test/output.png"}
			]
		}`))
	}))
	defer upstream.Close()

	// Provider has openai-images. Combo has openai-images.
	// Both should auto-inherit openai-image-edits!
	cfg := &config.AppConfig{
		Providers: []config.ProviderConfig{
			{
				Name:        "sn-img",
				APIs:        []config.ApiEndpoint{{APIFormat: "openai-images", BaseURL: upstream.URL}},
				MaxRetries:  1,
				KeyStrategy: "fill-first",
				Keys:        []config.KeyConfig{{Key: "sk-img-key"}},
			},
		},
		Combos: []config.ComboConfig{
			{
				Name:      "img-edit-combo",
				APIFormat: "openai-images", // auto-inherits openai-image-edits
				Strategy:  "fill-first",
				Members: []config.ComboMember{
					{Provider: "sn-img", Model: "dall-e-2"},
				},
			},
		},
	}

	kms := map[string]*keys.Manager{
		"sn-img": keys.NewManager("sn-img", []string{"sk-img-key"}, "fill-first"),
	}
	recRecorder, _ := db.NewRecorder(filepath.Join(t.TempDir(), "test.db"))
	svc, err := New(cfg, kms, combos.NewRouter(cfg.Combos), map[string]*http.Client{}, recRecorder, nil)
	if err != nil {
		t.Fatalf("New service: %v", err)
	}

	var reqBody bytes.Buffer
	mw := multipart.NewWriter(&reqBody)
	_ = mw.WriteField("model", "img-edit-combo")
	_ = mw.WriteField("prompt", "make background blue")
	part, _ := mw.CreateFormFile("image", "input.png")
	_, _ = part.Write([]byte("raw-png-bytes-12345"))
	_ = mw.Close()

	req := httptest.NewRequest("POST", "/v1/images/edits", &reqBody)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer client-key")
	w := httptest.NewRecorder()

	svc.Handle(w, req, "openai-image-edits", true)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if receivedPath != "/images/edits" {
		t.Fatalf("expected upstream path /images/edits, got %q", receivedPath)
	}
	if receivedAuth != "Bearer sk-img-key" {
		t.Fatalf("expected upstream auth Bearer sk-img-key, got %q", receivedAuth)
	}
	if receivedModel != "dall-e-2" {
		t.Fatalf("expected upstream model dall-e-2, got %q", receivedModel)
	}
	if receivedPrompt != "make background blue" {
		t.Fatalf("expected prompt 'make background blue', got %q", receivedPrompt)
	}
	if string(receivedFileBytes) != "raw-png-bytes-12345" {
		t.Fatalf("expected image bytes 'raw-png-bytes-12345', got %q", string(receivedFileBytes))
	}
	if !strings.Contains(w.Body.String(), "https://upstream.test/output.png") {
		t.Fatalf("unexpected client response: %s", w.Body.String())
	}
}



