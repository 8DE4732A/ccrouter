package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ccrouter/internal/config"
	"ccrouter/internal/db"
	"ccrouter/internal/gateway"
	"ccrouter/internal/report"
)

const testConfigYAML = `
providers:
  - name: sn
    api:
      - api_format: openai
        base_url: "http://127.0.0.1:1/v1"
    keys:
      - key: sk-1
    health_check_rules: []
combos:
  - name: fast
    api_format: openai
    strategy: fill-first
    members:
      - provider: sn
        model: gpt
verbose_logging: false
payload_scripts: []
`

func newTestState(t *testing.T) (*gateway.State, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(testConfigYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := db.NewRecorder(filepath.Join(dir, "sense-roll.db"))
	if err != nil {
		t.Fatal(err)
	}
	rl, err := report.New(filepath.Join(dir, "logs"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := gateway.New(cfg, cfgPath, rec, rl)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return st, cfgPath
}

func doGET(t *testing.T, r http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func doJSON(t *testing.T, r http.Handler, method, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if payload != nil {
		b, _ := json.Marshal(payload)
		body = strings.NewReader(string(b))
	}
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestAdminEndpoints(t *testing.T) {
	st, _ := newTestState(t)
	r := Router(st)

	// GET /admin/api/config
	rec := doGET(t, r, "/admin/api/config")
	if rec.Code != 200 {
		t.Fatalf("config: expected 200, got %d", rec.Code)
	}
	var cfgDump map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &cfgDump); err != nil {
		t.Fatal(err)
	}
	if len(cfgDump["providers"].([]any)) != 1 {
		t.Fatal("expected 1 provider in config")
	}

	// GET /v1/models
	rec = doGET(t, r, "/v1/models")
	if rec.Code != 200 {
		t.Fatalf("models: expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"fast"`) {
		t.Fatalf("expected fast in models, body=%s", rec.Body.String())
	}

	// GET /keys/status
	rec = doGET(t, r, "/keys/status")
	if rec.Code != 200 {
		t.Fatalf("keys/status: expected 200, got %d", rec.Code)
	}

	// GET /admin/api/stats/keys
	rec = doGET(t, r, "/admin/api/stats/keys")
	if rec.Code != 200 {
		t.Fatalf("stats/keys: expected 200, got %d", rec.Code)
	}

	// GET /admin/api/stats/summary
	rec = doGET(t, r, "/admin/api/stats/summary")
	if rec.Code != 200 {
		t.Fatalf("summary: expected 200, got %d", rec.Code)
	}

	// GET /admin/api/stats/trend
	rec = doGET(t, r, "/admin/api/stats/trend")
	if rec.Code != 200 {
		t.Fatalf("trend: expected 200, got %d", rec.Code)
	}

	// GET /admin/api/requests
	rec = doGET(t, r, "/admin/api/requests")
	if rec.Code != 200 {
		t.Fatalf("requests: expected 200, got %d", rec.Code)
	}

	// GET /admin/api/info
	rec = doGET(t, r, "/admin/api/info")
	if rec.Code != 200 {
		t.Fatalf("info: expected 200, got %d", rec.Code)
	}
	var info map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &info)
	if info["version"] == "" {
		t.Fatal("expected version in info")
	}

	// GET /admin/api/health
	rec = doGET(t, r, "/admin/api/health")
	if rec.Code != 200 {
		t.Fatalf("health: expected 200, got %d", rec.Code)
	}

	// GET /admin/api/logs
	rec = doGET(t, r, "/admin/api/logs")
	if rec.Code != 200 {
		t.Fatalf("logs: expected 200, got %d", rec.Code)
	}

	// GET /admin/api/logs/settings
	rec = doGET(t, r, "/admin/api/logs/settings")
	if rec.Code != 200 {
		t.Fatalf("logs/settings: expected 200, got %d", rec.Code)
	}
	var settings map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &settings)
	if settings["verbose_logging"] != false {
		t.Fatal("expected verbose_logging false")
	}
}

func TestPutConfigHotReload(t *testing.T) {
	st, cfgPath := newTestState(t)
	r := Router(st)

	newCfg := map[string]any{
		"providers": []map[string]any{
			{
				"name":               "sn2",
				"api":                []map[string]any{{"api_format": "openai", "base_url": "http://127.0.0.1:1/v1"}},
				"max_retries":        1,
				"key_strategy":       "fill-first",
				"keys":               []map[string]any{{"key": "sk-new"}},
				"health_check_rules": []any{},
			},
		},
		"combos": []map[string]any{
			{"name": "new-combo", "api_format": "openai", "strategy": "fill-first",
				"members": []map[string]any{{"provider": "sn2", "model": "m"}}},
		},
		"verbose_logging": false,
	}
	rec := doJSON(t, r, "PUT", "/admin/api/config", newCfg)
	if rec.Code != 200 {
		t.Fatalf("put config: expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	// The config file should now be rewritten.
	filedata, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(filedata), "sn2") {
		t.Fatalf("expected config file rewritten with sn2, got:\n%s", filedata)
	}
}

func TestPutConfigRejectsInvalid(t *testing.T) {
	st, _ := newTestState(t)
	r := Router(st)
	rec := doJSON(t, r, "PUT", "/admin/api/config", map[string]any{"providers": []any{}})
	if rec.Code != 400 {
		t.Fatalf("expected 400 for invalid config, got %d", rec.Code)
	}
}

func TestSPAAdmin(t *testing.T) {
	st, _ := newTestState(t)
	r := Router(st)
	rec := doGET(t, r, "/admin/")
	if rec.Code != 200 {
		t.Fatalf("expected 200 for admin index, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<!doctype html>") && !strings.Contains(rec.Body.String(), "<html") {
		t.Fatalf("expected html, got: %s", rec.Body.String()[:min(len(rec.Body.String()), 200)])
	}
}
