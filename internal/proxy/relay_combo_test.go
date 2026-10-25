package proxy

import (
	"encoding/json"
	"io"
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

func newComboRelayService(t *testing.T, apiFormat, baseURL string, rec *db.Recorder,
	memberCost *config.ComboMemberCost) *Service {
	t.Helper()
	cfg := &config.AppConfig{
		Providers: []config.ProviderConfig{{
			Name:        "sn",
			APIs:        []config.ApiEndpoint{{APIFormat: apiFormat, BaseURL: baseURL}},
			MaxRetries:  0,
			KeyStrategy: "fill-first",
			Keys:        []config.KeyConfig{{Key: "sk-1"}},
		}},
		Combos: []config.ComboConfig{{
			Name:      "byok",
			Relay:     true,
			APIFormat: apiFormat,
			Strategy:  "fill-first",
			Members:   []config.ComboMember{{Provider: "sn", Cost: memberCost}},
		}},
	}
	kms := map[string]*keys.Manager{"sn": keys.NewManager("sn", []string{"sk-1"}, "fill-first")}
	svc, err := New(cfg, kms, combos.NewRouter(cfg.Combos), map[string]*http.Client{}, rec, nil)
	if err != nil {
		t.Fatalf("build service: %v", err)
	}
	return svc
}

func TestRelayComboForwardsRequestedModel(t *testing.T) {
	var gotModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotModel, _ = body["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":100,"completion_tokens":40,"total_tokens":140}}`)
	}))
	defer upstream.Close()

	dbPath := filepath.Join(t.TempDir(), "relay-combo.db")
	rec, err := db.NewRecorder(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()

	f := func(v float64) *float64 { return &v }
	svc := newComboRelayService(t, "openai", upstream.URL, rec,
		&config.ComboMemberCost{Input: f(3), Output: f(15)})

	req := chatRequest(t, "/v1/chat/completions", map[string]any{"model": "byok/gpt-4o"})
	w := httptest.NewRecorder()
	svc.Handle(w, req, "openai", false)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if gotModel != "gpt-4o" {
		t.Fatalf("upstream model = %q, want gpt-4o", gotModel)
	}

	rec.Flush()
	res, err := db.QueryList(dbPath, 10, 0, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	items := res["items"].([]map[string]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 row, got %d", len(items))
	}
	if items[0]["combo"] != "byok" {
		t.Fatalf("combo = %v", items[0]["combo"])
	}
	if items[0]["model"] != "gpt-4o" {
		t.Fatalf("model = %v", items[0]["model"])
	}
	cost, ok := items[0]["cost_usd"].(float64)
	if !ok {
		t.Fatalf("cost_usd = %v", items[0]["cost_usd"])
	}
	want := 100.0/1e6*3 + 40.0/1e6*15
	if diff := cost - want; diff > 1e-12 || diff < -1e-12 {
		t.Fatalf("cost = %v, want %v", cost, want)
	}
}

func TestRelayComboGeminiUsesModelFromPath(t *testing.T) {
	var gotPath, gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`)
	}))
	defer upstream.Close()

	svc := newComboRelayService(t, "gemini", upstream.URL, nil, nil)
	req := chatRequest(t, "/v1beta/models/byok/gemini-2.5-pro:generateContent", map[string]any{"contents": []any{}})
	w := httptest.NewRecorder()
	svc.Handle(w, req, "gemini", false)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.HasSuffix(gotPath, "/models/gemini-2.5-pro:generateContent") {
		t.Fatalf("upstream path = %q", gotPath)
	}
	if strings.Contains(gotBody, `"model"`) {
		t.Fatalf("gemini body must not contain model: %s", gotBody)
	}
}

func TestRelayComboRequiresModelAfterPrefix(t *testing.T) {
	svc := newComboRelayService(t, "openai", "http://127.0.0.1:1", nil, nil)
	req := chatRequest(t, "/v1/chat/completions", map[string]any{"model": "byok"})
	w := httptest.NewRecorder()
	svc.Handle(w, req, "openai", false)

	if w.Code != 400 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "requires a model") {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}
