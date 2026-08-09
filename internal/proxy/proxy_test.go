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
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	svc.Handle(rec, req, "openai", false)
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
