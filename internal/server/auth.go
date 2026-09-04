package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"ccrouter/internal/gateway"

	"github.com/gin-gonic/gin"
)

type tokenPayload struct {
	Exp   int64  `json:"exp"`
	Nonce string `json:"nonce"`
}

var (
	signingKeyMu     sync.RWMutex
	cachedPassword   string
	cachedSigningKey []byte
)

// deriveAdminSigningKey derives a cryptographic HMAC key from the admin password
// using domain separation salt and 10,000 rounds of SHA-256. This significantly
// increases the computational cost of offline brute-force attacks if a session token
// is leaked, and invalidates precomputed rainbow tables.
func deriveAdminSigningKey(password string) []byte {
	salt := []byte("ccrouter-admin-token-v1:")
	h := sha256.New()
	h.Write(salt)
	h.Write([]byte(password))
	derived := h.Sum(nil)

	for i := 0; i < 10000; i++ {
		h.Reset()
		h.Write(derived)
		h.Write(salt)
		derived = h.Sum(nil)
	}
	return derived
}

// getAdminSigningKey returns the cached derived signing key for the given password,
// recomputing only when the password changes.
func getAdminSigningKey(password string) []byte {
	signingKeyMu.RLock()
	if cachedPassword == password && len(cachedSigningKey) > 0 {
		key := cachedSigningKey
		signingKeyMu.RUnlock()
		return key
	}
	signingKeyMu.RUnlock()

	signingKeyMu.Lock()
	defer signingKeyMu.Unlock()
	if cachedPassword == password && len(cachedSigningKey) > 0 {
		return cachedSigningKey
	}
	key := deriveAdminSigningKey(password)
	cachedPassword = password
	cachedSigningKey = key
	return key
}

func generateAdminToken(password string, ttl time.Duration) (string, int64, error) {
	exp := time.Now().Add(ttl).Unix()
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", 0, fmt.Errorf("failed to read random bytes for nonce: %w", err)
	}
	payload := tokenPayload{
		Exp:   exp,
		Nonce: hex.EncodeToString(nonceBytes),
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", 0, fmt.Errorf("failed to marshal token payload: %w", err)
	}
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadBytes)

	key := getAdminSigningKey(password)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payloadB64))
	sigB64 := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return payloadB64 + "." + sigB64, exp, nil
}

func verifyAdminToken(token string, password string) bool {
	if token == "" || password == "" {
		return false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return false
	}
	payloadB64, sigB64 := parts[0], parts[1]

	sigBytes, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return false
	}

	key := getAdminSigningKey(password)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payloadB64))
	expectedSig := mac.Sum(nil)

	if !hmac.Equal(sigBytes, expectedSig) {
		return false
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return false
	}

	var payload tokenPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return false
	}

	if time.Now().Unix() > payload.Exp {
		return false
	}

	return true
}

func extractAdminToken(c *gin.Context) string {
	auth := c.GetHeader("Authorization")
	if auth != "" {
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
			return strings.TrimSpace(parts[1])
		}
	}
	if tok := c.GetHeader("X-Admin-Token"); tok != "" {
		return strings.TrimSpace(tok)
	}
	return ""
}

func checkAdminAuth(token string, password string) bool {
	if password == "" {
		return true
	}
	if token == "" {
		return false
	}
	// 1. Direct password match (CLI / curl convenience)
	if subtle.ConstantTimeCompare([]byte(token), []byte(password)) == 1 {
		return true
	}
	// 2. HMAC session token verification
	return verifyAdminToken(token, password)
}

func adminAuth(state *gateway.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := state.Service()
		password := ""
		if svc != nil && svc.Config != nil {
			password = svc.Config.General.AdminPassword
		}
		if password == "" {
			c.Next()
			return
		}

		token := extractAdminToken(c)
		if !checkAdminAuth(token, password) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "unauthorized",
				"type":  "auth_error",
			})
			return
		}
		c.Next()
	}
}

// ---- Rate Limiter & Audit Logging for Login ----

type ipAttempt struct {
	failures    int
	lastFailure time.Time
	lockedUntil time.Time
}

type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]*ipAttempt
}

var globalLoginLimiter = &loginLimiter{
	attempts: make(map[string]*ipAttempt),
}

const (
	maxConsecutiveFailures = 5
	lockoutDuration        = 5 * time.Minute
	cleanupThreshold       = 30 * time.Minute
)

func (l *loginLimiter) check(ip string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	att, ok := l.attempts[ip]
	if !ok {
		return true, 0
	}

	now := time.Now()
	if now.Before(att.lockedUntil) {
		return false, att.lockedUntil.Sub(now)
	}

	// Reset if lockout window has expired
	if att.failures >= maxConsecutiveFailures && now.After(att.lockedUntil) {
		att.failures = 0
		att.lockedUntil = time.Time{}
	}

	return true, 0
}

func (l *loginLimiter) recordFailure(ip string) (int, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	att, ok := l.attempts[ip]
	if !ok {
		att = &ipAttempt{}
		l.attempts[ip] = att
	}

	att.failures++
	att.lastFailure = now

	locked := false
	if att.failures >= maxConsecutiveFailures {
		att.lockedUntil = now.Add(lockoutDuration)
		locked = true
	}

	// Opportunistic cleanup of stale entries
	if len(l.attempts) > 100 {
		for k, v := range l.attempts {
			if now.Sub(v.lastFailure) > cleanupThreshold {
				delete(l.attempts, k)
			}
		}
	}

	return att.failures, locked
}

func (l *loginLimiter) recordSuccess(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, ip)
}

func resetLoginLimiter() {
	globalLoginLimiter.mu.Lock()
	defer globalLoginLimiter.mu.Unlock()
	globalLoginLimiter.attempts = make(map[string]*ipAttempt)
}

// ---- Auth Handlers ----

type authStatusResp struct {
	AuthRequired bool `json:"auth_required"`
	LoggedIn     bool `json:"logged_in"`
}

func getAuthStatus(c *gin.Context) {
	st := stateOf(c)
	password := ""
	if st != nil && st.Service() != nil && st.Service().Config != nil {
		password = st.Service().Config.General.AdminPassword
	}
	if password == "" {
		c.JSON(http.StatusOK, authStatusResp{
			AuthRequired: false,
			LoggedIn:     true,
		})
		return
	}

	token := extractAdminToken(c)
	loggedIn := checkAdminAuth(token, password)
	c.JSON(http.StatusOK, authStatusResp{
		AuthRequired: true,
		LoggedIn:     loggedIn,
	})
}

type loginReq struct {
	Password string `json:"password"`
}

func adminLogin(c *gin.Context) {
	st := stateOf(c)
	password := ""
	if st != nil && st.Service() != nil && st.Service().Config != nil {
		password = st.Service().Config.General.AdminPassword
	}

	clientIP := c.ClientIP()

	// If no password configured, login is always successful
	if password == "" {
		c.JSON(http.StatusOK, gin.H{
			"token":      "",
			"expires_at": 0,
		})
		return
	}

	// 1. Rate limiting check
	allowed, remaining := globalLoginLimiter.check(clientIP)
	if !allowed {
		log.Printf("[auth] login blocked: rate limit exceeded for IP %s (locked for %v)", clientIP, remaining.Round(time.Second))
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": "too many failed login attempts, please try again later",
			"type":  "rate_limit_error",
		})
		return
	}

	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password is required"})
		return
	}

	// 2. Constant-time password verification
	if subtle.ConstantTimeCompare([]byte(req.Password), []byte(password)) != 1 {
		failures, locked := globalLoginLimiter.recordFailure(clientIP)
		if locked {
			log.Printf("[auth] admin login failed from IP %s (consecutive failures: %d, locked for %v)", clientIP, failures, lockoutDuration)
		} else {
			log.Printf("[auth] admin login failed from IP %s (failure %d/%d)", clientIP, failures, maxConsecutiveFailures)
		}

		// Progressive backoff delay to mitigate high-speed automated attacks
		time.Sleep(time.Duration(failures*100) * time.Millisecond)

		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "incorrect password",
			"type":  "auth_error",
		})
		return
	}

	// Login succeeded
	globalLoginLimiter.recordSuccess(clientIP)
	log.Printf("[auth] admin login successful from IP %s", clientIP)

	token, exp, err := generateAdminToken(password, 7*24*time.Hour)
	if err != nil {
		log.Printf("[auth] failed to generate admin token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token":      token,
		"expires_at": exp,
	})
}

func adminLogout(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
