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
	return newTestStateWithYAML(t, testConfigYAML)
}

func newTestStateWithYAML(t *testing.T, yaml string) (*gateway.State, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
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

const testConfigYAMLWithAPIKeys = `
general:
  api_keys:
    - key: correct-key
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

func TestAPIKeyAuthBothHeaders(t *testing.T) {
	st, _ := newTestStateWithYAML(t, testConfigYAMLWithAPIKeys)
	r := Router(st)

	doReq := func(xAPIKey, bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/v1/models", nil)
		if xAPIKey != "" {
			req.Header.Set("X-Api-Key", xAPIKey)
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	t.Run("only x-api-key, correct -> 200", func(t *testing.T) {
		rec := doReq("correct-key", "")
		if rec.Code != 200 {
			t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("only bearer, correct -> 200", func(t *testing.T) {
		rec := doReq("", "correct-key")
		if rec.Code != 200 {
			t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("neither -> 401", func(t *testing.T) {
		rec := doReq("", "")
		if rec.Code != 401 {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("both correct -> 200", func(t *testing.T) {
		rec := doReq("correct-key", "correct-key")
		if rec.Code != 200 {
			t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("x-api-key correct, bearer garbage -> 401 (both must match now)", func(t *testing.T) {
		rec := doReq("correct-key", "garbage-token")
		if rec.Code != 401 {
			t.Fatalf("expected 401, got %d, body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("bearer correct, x-api-key garbage -> 401 (both must match now)", func(t *testing.T) {
		rec := doReq("garbage-key", "correct-key")
		if rec.Code != 401 {
			t.Fatalf("expected 401, got %d, body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("x-api-key wrong, no bearer -> 401", func(t *testing.T) {
		rec := doReq("wrong-key", "")
		if rec.Code != 401 {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})
}

func TestListModelsOwnedBy(t *testing.T) {
	configYAML := `
providers:
  - name: sn
    api:
      - api_format: openai
        base_url: "https://upstream.test/v1"
    keys:
      - key: sk-1
combos:
  - owned_by: default
    default: true
    combos:
      - name: fast
        api_format: openai
        members: [{provider: sn, model: deepseek-flash}]
        aliases: ["fast-alias"]
  - owned_by: team-a
    combos:
      - name: smart
        api_format: openai
        members: [{provider: sn, model: deepseek-chat}]
        aliases: ["smart-alias"]
`
	st, _ := newTestStateWithYAML(t, configYAML)
	r := Router(st)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/models", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("models: expected 200, got %d", rec.Code)
	}

	var resp struct {
		Object string `json:"object"`
		Data   []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal /v1/models response: %v", err)
	}

	modelMap := map[string]string{}
	for _, m := range resp.Data {
		modelMap[m.ID] = m.OwnedBy
	}

	// Default group models: no prefix, owned_by is "default"
	if modelMap["fast"] != "default" {
		t.Fatalf("expected 'fast' to have owned_by 'default', got %q", modelMap["fast"])
	}
	if modelMap["fast-alias"] != "default" {
		t.Fatalf("expected 'fast-alias' to have owned_by 'default', got %q", modelMap["fast-alias"])
	}

	// team-a models: "team-a/smart" and "team-a/smart-alias", owned_by is "team-a"
	if modelMap["team-a/smart"] != "team-a" {
		t.Fatalf("expected 'team-a/smart' to have owned_by 'team-a', got %q", modelMap["team-a/smart"])
	}
	if modelMap["team-a/smart-alias"] != "team-a" {
		t.Fatalf("expected 'team-a/smart-alias' to have owned_by 'team-a', got %q", modelMap["team-a/smart-alias"])
	}
}

func TestEmbeddingsRoute(t *testing.T) {
	st, _ := newTestStateWithYAML(t, testConfigYAMLWithAPIKeys)
	r := Router(st)

	// Unauthorized request -> 401
	rec := doJSON(t, r, "POST", "/v1/embeddings", map[string]any{
		"model": "text-embedding-3-small",
		"input": "test input",
	})
	if rec.Code != 401 {
		t.Fatalf("expected 401 unauthorized for /v1/embeddings without key, got %d", rec.Code)
	}

	// Authorized request with key -> reaches proxy (returns 400 since combo not in this test YAML)
	req := httptest.NewRequest("POST", "/v1/embeddings", strings.NewReader(`{"model":"unknown-model","input":"test"}`))
	req.Header.Set("Authorization", "Bearer correct-key")
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("expected 400 unknown combo for authorized request, got %d", rec.Code)
	}
}

func TestAdminAuthentication(t *testing.T) {
	// 1. Unauthenticated mode (admin_password not set)
	stNoAuth, _ := newTestState(t)
	rNoAuth := Router(stNoAuth)

	// Status endpoint
	rec := doGET(t, rNoAuth, "/admin/api/auth/status")
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var statusResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &statusResp); err != nil {
		t.Fatal(err)
	}
	if statusResp["auth_required"] != false || statusResp["logged_in"] != true {
		t.Fatalf("expected auth_required=false and logged_in=true, got %#v", statusResp)
	}

	// Direct access to /admin/api/config should succeed
	rec = doGET(t, rNoAuth, "/admin/api/config")
	if rec.Code != 200 {
		t.Fatalf("expected 200 without auth, got %d", rec.Code)
	}

	// 2. Authenticated mode (admin_password set)
	const testConfigYAMLWithAdminPassword = `
general:
  admin_password: "super-admin-pass"
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
	stAuth, _ := newTestStateWithYAML(t, testConfigYAMLWithAdminPassword)
	rAuth := Router(stAuth)

	// Status endpoint without credentials
	rec = doGET(t, rAuth, "/admin/api/auth/status")
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &statusResp); err != nil {
		t.Fatal(err)
	}
	if statusResp["auth_required"] != true || statusResp["logged_in"] != false {
		t.Fatalf("expected auth_required=true and logged_in=false, got %#v", statusResp)
	}

	// Protected endpoint without credentials should fail with 401
	rec = doGET(t, rAuth, "/admin/api/config")
	if rec.Code != 401 {
		t.Fatalf("expected 401 for unauthenticated request, got %d", rec.Code)
	}

	// Login with wrong password
	rec = doJSON(t, rAuth, "POST", "/admin/api/auth/login", map[string]any{"password": "wrong-password"})
	if rec.Code != 401 {
		t.Fatalf("expected 401 for wrong password, got %d", rec.Code)
	}

	// Login with correct password
	rec = doJSON(t, rAuth, "POST", "/admin/api/auth/login", map[string]any{"password": "super-admin-pass"})
	if rec.Code != 200 {
		t.Fatalf("expected 200 for correct login, got %d", rec.Code)
	}
	var loginResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &loginResp); err != nil {
		t.Fatal(err)
	}
	token, ok := loginResp["token"].(string)
	if !ok || token == "" {
		t.Fatalf("expected non-empty token string, got %#v", loginResp["token"])
	}

	// Status endpoint with valid session token
	req := httptest.NewRequest("GET", "/admin/api/auth/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	rAuth.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &statusResp); err != nil {
		t.Fatal(err)
	}
	if statusResp["auth_required"] != true || statusResp["logged_in"] != true {
		t.Fatalf("expected auth_required=true and logged_in=true, got %#v", statusResp)
	}

	// Protected endpoint with valid session token via Authorization: Bearer <token>
	req = httptest.NewRequest("GET", "/admin/api/config", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	rAuth.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("expected 200 with session token, got %d", rec.Code)
	}

	// Protected endpoint with valid session token via X-Admin-Token
	req = httptest.NewRequest("GET", "/admin/api/config", nil)
	req.Header.Set("X-Admin-Token", token)
	rec = httptest.NewRecorder()
	rAuth.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("expected 200 with X-Admin-Token, got %d", rec.Code)
	}

	// Protected endpoint with direct password in Authorization: Bearer <admin_password>
	req = httptest.NewRequest("GET", "/admin/api/config", nil)
	req.Header.Set("Authorization", "Bearer super-admin-pass")
	rec = httptest.NewRecorder()
	rAuth.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("expected 200 with direct password Bearer, got %d", rec.Code)
	}

	// Protected endpoint with tampered token
	req = httptest.NewRequest("GET", "/admin/api/config", nil)
	req.Header.Set("Authorization", "Bearer "+token+"tampered")
	rec = httptest.NewRecorder()
	rAuth.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("expected 401 for tampered token, got %d", rec.Code)
	}

	// Logout endpoint
	rec = doJSON(t, rAuth, "POST", "/admin/api/auth/logout", nil)
	if rec.Code != 200 {
		t.Fatalf("expected 200 for logout, got %d", rec.Code)
	}

	// 3. Test rate limiting and lockout
	resetLoginLimiter()
	// Fail 4 times -> returns 401
	for i := 1; i <= 4; i++ {
		req := httptest.NewRequest("POST", "/admin/api/auth/login", strings.NewReader(`{"password":"bad"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.0.2.10:54321"
		rec := httptest.NewRecorder()
		rAuth.ServeHTTP(rec, req)
		if rec.Code != 401 {
			t.Fatalf("expected 401 on attempt %d, got %d", i, rec.Code)
		}
	}
	// 5th failure -> triggers lockout and returns 401
	{
		req := httptest.NewRequest("POST", "/admin/api/auth/login", strings.NewReader(`{"password":"bad"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.0.2.10:54321"
		rec := httptest.NewRecorder()
		rAuth.ServeHTTP(rec, req)
		if rec.Code != 401 {
			t.Fatalf("expected 401 on 5th attempt, got %d", rec.Code)
		}
	}
	// 6th attempt (even with correct password) -> 429 Too Many Requests
	{
		req := httptest.NewRequest("POST", "/admin/api/auth/login", strings.NewReader(`{"password":"super-admin-pass"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.0.2.10:54321"
		rec := httptest.NewRecorder()
		rAuth.ServeHTTP(rec, req)
		if rec.Code != 429 {
			t.Fatalf("expected 429 on 6th attempt due to lockout, got %d", rec.Code)
		}
	}
	// Different IP should not be locked out and can log in successfully
	{
		req := httptest.NewRequest("POST", "/admin/api/auth/login", strings.NewReader(`{"password":"super-admin-pass"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.0.2.20:54321"
		rec := httptest.NewRecorder()
		rAuth.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("expected 200 from different IP, got %d", rec.Code)
		}
	}
}

