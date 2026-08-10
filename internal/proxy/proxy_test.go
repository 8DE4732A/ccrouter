package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ccrouter/internal/combos"
	"ccrouter/internal/config"
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
