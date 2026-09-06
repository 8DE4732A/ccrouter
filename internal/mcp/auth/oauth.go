package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ccrouter/internal/config"
)

// PKCE holds a code verifier and its S256 challenge.
type PKCE struct {
	Verifier  string
	Challenge string
	Method    string
}

// GeneratePKCE creates a secure random code_verifier and S256 code_challenge.
func GeneratePKCE() (*PKCE, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("read random bytes: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])
	return &PKCE{
		Verifier:  verifier,
		Challenge: challenge,
		Method:    "S256",
	}, nil
}

// GenerateState generates a signed state parameter containing provider, user_id, and timestamp.
func GenerateState(provider, userID string, secretKey []byte) string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	payload := fmt.Sprintf("%s:%s:%s", ts, provider, userID)
	mac := hmac.New(sha256.New, secretKey)
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	raw := fmt.Sprintf("%s:%s", payload, sig)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// VerifyState validates a signed state string, ensuring it hasn't expired and the signature matches.
func VerifyState(stateStr string, secretKey []byte, maxAge time.Duration) (provider, userID string, err error) {
	data, err := base64.RawURLEncoding.DecodeString(stateStr)
	if err != nil {
		return "", "", errors.New("invalid state encoding")
	}
	parts := strings.Split(string(data), ":")
	if len(parts) != 4 {
		return "", "", errors.New("invalid state format")
	}
	tsStr, provider, userID, sigHex := parts[0], parts[1], parts[2], parts[3]

	// Verify timestamp
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return "", "", errors.New("invalid state timestamp")
	}
	if time.Since(time.Unix(ts, 0)) > maxAge {
		return "", "", errors.New("state parameter has expired")
	}

	// Verify HMAC signature
	payload := fmt.Sprintf("%s:%s:%s", tsStr, provider, userID)
	mac := hmac.New(sha256.New, secretKey)
	mac.Write([]byte(payload))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(sigHex), []byte(expectedSig)) {
		return "", "", errors.New("invalid state signature")
	}
	return provider, userID, nil
}

// TokenResult represents the response from an OAuth token endpoint.
type TokenResult struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type"`
	ExpiresIn    *int64 `json:"expires_in,omitempty"`
	Scope        string `json:"scope,omitempty"`
	Error        string `json:"error,omitempty"`
	ErrorDesc    string `json:"error_description,omitempty"`
}

// BuildAuthorizationURL constructs the OAuth 2.1 authorization URL.
func BuildAuthorizationURL(cfg *config.McpAuthConfig, state, codeChallenge string) (string, error) {
	u, err := url.Parse(cfg.AuthorizationURL)
	if err != nil {
		return "", fmt.Errorf("invalid authorization_url: %w", err)
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", cfg.ClientID)
	if cfg.RedirectURL != "" {
		q.Set("redirect_uri", cfg.RedirectURL)
	}
	if len(cfg.Scopes) > 0 {
		q.Set("scope", strings.Join(cfg.Scopes, " "))
	}
	q.Set("state", state)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// ExchangeCode exchanges an authorization code and PKCE verifier for tokens.
func ExchangeCode(ctx context.Context, client *http.Client, cfg *config.McpAuthConfig, code, codeVerifier, redirectURI string) (*TokenResult, error) {
	if client == nil {
		client = http.DefaultClient
	}

	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("client_id", cfg.ClientID)
	if cfg.ClientSecret != "" {
		data.Set("client_secret", cfg.ClientSecret)
	}
	if redirectURI != "" {
		data.Set("redirect_uri", redirectURI)
	} else if cfg.RedirectURL != "" {
		data.Set("redirect_uri", cfg.RedirectURL)
	}
	data.Set("code_verifier", codeVerifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read token response failed: %w", err)
	}

	var res TokenResult
	if err := json.Unmarshal(body, &res); err != nil {
		// Some legacy servers return form-urlencoded response (like GitHub)
		vals, errParse := url.ParseQuery(string(body))
		if errParse == nil && vals.Get("access_token") != "" {
			res.AccessToken = vals.Get("access_token")
			res.RefreshToken = vals.Get("refresh_token")
			res.TokenType = vals.Get("token_type")
			res.Scope = vals.Get("scope")
			if exp, errExp := strconv.ParseInt(vals.Get("expires_in"), 10, 64); errExp == nil {
				res.ExpiresIn = &exp
			}
		} else {
			return nil, fmt.Errorf("unmarshal token response failed (%s): %w", string(body), err)
		}
	}

	if res.Error != "" {
		return nil, fmt.Errorf("oauth error: %s: %s", res.Error, res.ErrorDesc)
	}
	if res.AccessToken == "" {
		return nil, fmt.Errorf("no access_token in response: %s", string(body))
	}
	if res.TokenType == "" {
		res.TokenType = "Bearer"
	}
	return &res, nil
}

// RefreshToken uses a refresh token to get a new access token.
func RefreshToken(ctx context.Context, client *http.Client, cfg *config.McpAuthConfig, refreshToken string) (*TokenResult, error) {
	if client == nil {
		client = http.DefaultClient
	}

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", cfg.ClientID)
	if cfg.ClientSecret != "" {
		data.Set("client_secret", cfg.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read refresh token response: %w", err)
	}

	var res TokenResult
	if err := json.Unmarshal(body, &res); err != nil {
		vals, errParse := url.ParseQuery(string(body))
		if errParse == nil && vals.Get("access_token") != "" {
			res.AccessToken = vals.Get("access_token")
			res.RefreshToken = vals.Get("refresh_token")
			res.TokenType = vals.Get("token_type")
		} else {
			return nil, fmt.Errorf("unmarshal refresh response failed: %w", err)
		}
	}

	if res.Error != "" {
		return nil, fmt.Errorf("oauth refresh error: %s: %s", res.Error, res.ErrorDesc)
	}
	if res.AccessToken == "" {
		return nil, fmt.Errorf("no access_token in refresh response: %s", string(body))
	}
	return &res, nil
}
