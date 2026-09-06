package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ccrouter/internal/config"
)

func TestPKCE(t *testing.T) {
	pkce, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("generate pkce error: %v", err)
	}
	if len(pkce.Verifier) < 43 {
		t.Fatalf("verifier too short: %d", len(pkce.Verifier))
	}
	if pkce.Challenge == "" || pkce.Method != "S256" {
		t.Fatalf("invalid challenge or method: %#v", pkce)
	}
}

func TestStateSignAndVerify(t *testing.T) {
	secret := []byte("01234567890123456789012345678901")

	state := GenerateState("github", "alice", secret)
	if state == "" {
		t.Fatal("expected non-empty state")
	}

	// Successful verification
	prov, uid, err := VerifyState(state, secret, 5*time.Minute)
	if err != nil {
		t.Fatalf("verify state error: %v", err)
	}
	if prov != "github" || uid != "alice" {
		t.Fatalf("mismatched payload: prov=%s, uid=%s", prov, uid)
	}

	// Tampered secret
	badSecret := []byte("badsecretbadsecretbadsecret12345")
	_, _, err = VerifyState(state, badSecret, 5*time.Minute)
	if err == nil {
		t.Fatal("expected error with wrong secret, got nil")
	}

	// Expired state
	_, _, err = VerifyState(state, secret, -1*time.Second)
	if err == nil {
		t.Fatal("expected error for expired state, got nil")
	}
}

func TestExchangeCodeAndRefresh(t *testing.T) {
	// Mock OAuth Server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		_ = r.ParseForm()
		gt := r.Form.Get("grant_type")
		w.Header().Set("Content-Type", "application/json")

		if gt == "authorization_code" {
			if r.Form.Get("code") != "valid-code" || r.Form.Get("code_verifier") == "" {
				http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
				return
			}
			exp := int64(3600)
			_ = json.NewEncoder(w).Encode(TokenResult{
				AccessToken:  "mock-access-token",
				RefreshToken: "mock-refresh-token",
				TokenType:    "Bearer",
				ExpiresIn:    &exp,
				Scope:        "repo",
			})
		} else if gt == "refresh_token" {
			if r.Form.Get("refresh_token") != "mock-refresh-token" {
				http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(TokenResult{
				AccessToken: "mock-new-access-token",
				TokenType:   "Bearer",
			})
		}
	}))
	defer server.Close()

	cfg := &config.McpAuthConfig{
		ClientID: "test-client",
		TokenURL: server.URL,
	}

	ctx := context.Background()
	res, err := ExchangeCode(ctx, server.Client(), cfg, "valid-code", "test-verifier", "https://redirect")
	if err != nil {
		t.Fatalf("exchange code error: %v", err)
	}
	if res.AccessToken != "mock-access-token" || res.RefreshToken != "mock-refresh-token" {
		t.Fatalf("unexpected exchange result: %#v", res)
	}

	refreshed, err := RefreshToken(ctx, server.Client(), cfg, "mock-refresh-token")
	if err != nil {
		t.Fatalf("refresh token error: %v", err)
	}
	if refreshed.AccessToken != "mock-new-access-token" {
		t.Fatalf("unexpected refresh result: %#v", refreshed)
	}
}

func TestDeterministicSecretKey(t *testing.T) {
	m1 := NewManager(nil, nil, nil, "", "admin-secret-123")
	defer m1.Close()
	m2 := NewManager(nil, nil, nil, "", "admin-secret-123")
	defer m2.Close()

	if string(m1.secretKey) != string(m2.secretKey) {
		t.Fatal("expected identical secretKey for same admin password seed across instances")
	}

	state := GenerateState("provider", "user", m1.secretKey)
	prov, uid, err := VerifyState(state, m2.secretKey, 5*time.Minute)
	if err != nil {
		t.Fatalf("failed to verify state across restarted instances: %v", err)
	}
	if prov != "provider" || uid != "user" {
		t.Fatalf("unexpected state verify: prov=%s, uid=%s", prov, uid)
	}
}
