package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"ccrouter/internal/config"
	"ccrouter/internal/db"
)

// AuthRequiredError is returned when a user has not authenticated with an isolated provider.
type AuthRequiredError struct {
	Provider string `json:"provider"`
	UserID   string `json:"user_id"`
	AuthURL  string `json:"auth_url"`
}

func (e *AuthRequiredError) Error() string {
	return fmt.Sprintf("authentication required for provider %q (user: %q)", e.Provider, e.UserID)
}

type contextKey string

const BaseURLContextKey contextKey = "mcp_base_url"

// Manager manages credentials and OAuth flows for MCP providers.
type Manager struct {
	mu        sync.RWMutex
	db        *db.Recorder
	providers map[string]*config.McpProviderConfig
	clients   map[string]*http.Client
	secretKey []byte
	baseURL   string
	stopCh    chan struct{}
	closeOnce sync.Once

	// pkceCache maps state -> pkceVerifier with expiration
	pkceCache sync.Map
}

type pkceEntry struct {
	verifier  string
	provider  string
	userID    string
	expiresAt time.Time
}

// NewManager creates an MCP authentication manager.
// If secretSeed (e.g. admin_password) is provided, the secret key is derived deterministically via SHA256
// so that OAuth state signatures survive service restarts.
func NewManager(recorder *db.Recorder, providers []config.McpProviderConfig, clients map[string]*http.Client, baseURL string, secretSeed ...string) *Manager {
	var secret []byte
	if len(secretSeed) > 0 && secretSeed[0] != "" {
		h := sha256.Sum256([]byte("ccrouter-mcp-oauth-secret:" + secretSeed[0]))
		secret = h[:]
	} else {
		secret = make([]byte, 32)
		_, _ = rand.Read(secret)
	}

	provMap := make(map[string]*config.McpProviderConfig, len(providers))
	for i := range providers {
		p := &providers[i]
		provMap[p.Name] = p
	}

	m := &Manager{
		db:        recorder,
		providers: provMap,
		clients:   clients,
		secretKey: secret,
		baseURL:   strings.TrimRight(baseURL, "/"),
		stopCh:    make(chan struct{}),
	}

	// Periodically clean up expired PKCE cache entries
	go m.cleanupPKCECache()
	return m
}

// Close stops background cleanup goroutines.
func (m *Manager) Close() {
	m.closeOnce.Do(func() {
		close(m.stopCh)
	})
}

func (m *Manager) cleanupPKCECache() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			now := time.Now()
			m.pkceCache.Range(func(k, v any) bool {
				if entry, ok := v.(pkceEntry); ok && now.After(entry.expiresAt) {
					m.pkceCache.Delete(k)
				}
				return true
			})
		}
	}
}

// SetBaseURL updates the base URL for generating auth callback/start links.
func (m *Manager) SetBaseURL(u string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.baseURL = strings.TrimRight(u, "/")
}

// ResolveCredential finds or refreshes the credential for a provider and user.
func (m *Manager) ResolveCredential(ctx context.Context, providerName, userID string) (*Credential, error) {
	m.mu.RLock()
	p, ok := m.providers[providerName]
	client := m.clients[providerName]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown mcp provider %q", providerName)
	}

	// 1. Shared auth mode
	if p.AuthMode != "isolated" {
		if p.Auth == nil || p.Auth.Type == "none" || p.Auth.Type == "" {
			return nil, nil
		}
		if p.Auth.Type == "api_key" {
			hName := p.Auth.Header
			if hName == "" {
				hName = "Authorization"
			}
			hVal := p.Auth.APIKey
			if strings.EqualFold(hName, "Authorization") && !strings.HasPrefix(strings.ToLower(hVal), "bearer ") {
				hVal = "Bearer " + hVal
			}
			return &Credential{
				HeaderName:  hName,
				HeaderValue: hVal,
				Token:       p.Auth.APIKey,
				UserID:      userID,
			}, nil
		}
	}

	// 2. Isolated auth mode: user_id is required
	if userID == "" {
		return nil, &AuthRequiredError{
			Provider: providerName,
			UserID:   "",
			AuthURL:  m.BuildStartAuthURL(ctx, providerName, ""),
		}
	}

	if m.db == nil {
		return nil, errors.New("database not available for isolated user token storage")
	}

	tok, err := m.db.GetMcpToken(providerName, userID)
	if err != nil {
		return nil, fmt.Errorf("retrieve user token: %w", err)
	}

	if tok == nil || tok.AccessToken == "" {
		return nil, &AuthRequiredError{
			Provider: providerName,
			UserID:   userID,
			AuthURL:  m.BuildStartAuthURL(ctx, providerName, userID),
		}
	}

	// Check expiration (buffer 60 seconds)
	now := time.Now().Unix()
	if tok.ExpiresAt != nil && *tok.ExpiresAt <= now+60 {
		// Attempt silent refresh if refresh_token exists and auth is oauth2
		if tok.RefreshToken != "" && p.Auth != nil && p.Auth.Type == "oauth2" {
			refreshed, err := RefreshToken(ctx, client, p.Auth, tok.RefreshToken)
			if err == nil && refreshed.AccessToken != "" {
				tok.AccessToken = refreshed.AccessToken
				if refreshed.RefreshToken != "" {
					tok.RefreshToken = refreshed.RefreshToken
				}
				if refreshed.ExpiresIn != nil {
					exp := now + *refreshed.ExpiresIn
					tok.ExpiresAt = &exp
				}
				_ = m.db.SaveMcpToken(tok)
			} else {
				// Refresh failed: require re-authentication
				return nil, &AuthRequiredError{
					Provider: providerName,
					UserID:   userID,
					AuthURL:  m.BuildStartAuthURL(ctx, providerName, userID),
				}
			}
		} else {
			// Expired without refresh token
			return nil, &AuthRequiredError{
				Provider: providerName,
				UserID:   userID,
				AuthURL:  m.BuildStartAuthURL(ctx, providerName, userID),
			}
		}
	}

	headerVal := tok.AccessToken
	if !strings.HasPrefix(strings.ToLower(headerVal), "bearer ") {
		headerVal = "Bearer " + headerVal
	}

	return &Credential{
		HeaderName:  "Authorization",
		HeaderValue: headerVal,
		Token:       tok.AccessToken,
		UserID:      userID,
	}, nil
}

// BuildStartAuthURL returns the link downstream users/bots should visit to initiate auth.
// If ctx contains BaseURLContextKey or Manager has a configured baseURL, an absolute URL is returned.
func (m *Manager) BuildStartAuthURL(ctx context.Context, providerName, userID string) string {
	m.mu.RLock()
	base := m.baseURL
	m.mu.RUnlock()

	if ctx != nil {
		if reqBase, ok := ctx.Value(BaseURLContextKey).(string); ok && reqBase != "" {
			base = strings.TrimRight(reqBase, "/")
		}
	}

	if base == "" {
		base = "/mcp"
	} else {
		base = base + "/mcp"
	}
	return fmt.Sprintf("%s/auth/start?provider=%s&user_id=%s", base, url.QueryEscape(providerName), url.QueryEscape(userID))
}

// StartOAuthFlow generates state & PKCE and produces the upstream OAuth redirect URL.
func (m *Manager) StartOAuthFlow(providerName, userID, redirectURI string) (upstreamAuthURL string, err error) {
	m.mu.RLock()
	p, ok := m.providers[providerName]
	m.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("unknown mcp provider %q", providerName)
	}
	if p.Auth == nil || p.Auth.Type != "oauth2" {
		return "", fmt.Errorf("provider %q does not support oauth2", providerName)
	}

	pkce, err := GeneratePKCE()
	if err != nil {
		return "", fmt.Errorf("generate pkce: %w", err)
	}

	state := GenerateState(providerName, userID, m.secretKey)

	// Save PKCE verifier keyed by state (expires in 15 minutes)
	m.pkceCache.Store(state, pkceEntry{
		verifier:  pkce.Verifier,
		provider:  providerName,
		userID:    userID,
		expiresAt: time.Now().Add(15 * time.Minute),
	})

	authCfg := *p.Auth
	if redirectURI != "" {
		authCfg.RedirectURL = redirectURI
	}

	return BuildAuthorizationURL(&authCfg, state, pkce.Challenge)
}

// HandleCallback handles the OAuth redirect callback from upstream.
func (m *Manager) HandleCallback(ctx context.Context, code, state, redirectURI string) (*db.McpUserToken, error) {
	providerName, userID, err := VerifyState(state, m.secretKey, 15*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("verify state failed: %w", err)
	}

	entryRaw, ok := m.pkceCache.LoadAndDelete(state)
	if !ok {
		return nil, errors.New("state not found or already consumed")
	}
	entry := entryRaw.(pkceEntry)
	if time.Now().After(entry.expiresAt) {
		return nil, errors.New("state has expired")
	}

	m.mu.RLock()
	p, ok := m.providers[providerName]
	client := m.clients[providerName]
	m.mu.RUnlock()

	if !ok || p.Auth == nil || p.Auth.Type != "oauth2" {
		return nil, fmt.Errorf("provider %q not configured for oauth2", providerName)
	}

	tokenRes, err := ExchangeCode(ctx, client, p.Auth, code, entry.verifier, redirectURI)
	if err != nil {
		return nil, fmt.Errorf("exchange code: %w", err)
	}

	now := time.Now().Unix()
	var exp *int64
	if tokenRes.ExpiresIn != nil {
		e := now + *tokenRes.ExpiresIn
		exp = &e
	}

	tok := &db.McpUserToken{
		Provider:     providerName,
		UserID:       userID,
		AccessToken:  tokenRes.AccessToken,
		RefreshToken: tokenRes.RefreshToken,
		TokenType:    tokenRes.TokenType,
		Scopes:       tokenRes.Scope,
		ExpiresAt:    exp,
		UpdatedAt:    now,
	}

	if m.db != nil {
		if err := m.db.SaveMcpToken(tok); err != nil {
			return nil, fmt.Errorf("save user token: %w", err)
		}
	}

	return tok, nil
}
