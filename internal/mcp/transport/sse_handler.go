package transport

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"ccrouter/internal/mcp/auth"
	"ccrouter/internal/mcp/protocol"

	"github.com/gin-gonic/gin"
)

type sseSession struct {
	id        string
	comboName string
	userID    string
	msgChan   chan []byte
	done      chan struct{}
}

// SSEManager manages active SSE client sessions.
type SSEManager struct {
	mu       sync.RWMutex
	sessions map[string]*sseSession
}

var sseMgr = &SSEManager{
	sessions: make(map[string]*sseSession),
}

func generateSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// HandleSSE establishes a Server-Sent Events stream for downstream clients.
func (h *Handler) HandleSSE(c *gin.Context) {
	comboName := c.Param("combo")
	combo, ok := h.gw.GetCombo(comboName)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "combo not found: " + comboName})
		return
	}

	sessionID := generateSessionID()
	userID := strings.TrimSpace(c.GetHeader(combo.UserIDHeader()))

	session := &sseSession{
		id:        sessionID,
		comboName: comboName,
		userID:    userID,
		msgChan:   make(chan []byte, 32),
		done:      make(chan struct{}),
	}

	sseMgr.mu.Lock()
	sseMgr.sessions[sessionID] = session
	sseMgr.mu.Unlock()

	defer func() {
		sseMgr.mu.Lock()
		delete(sseMgr.sessions, sessionID)
		sseMgr.mu.Unlock()
		close(session.done)
	}()

	w := c.Writer
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	w.Flush()

	// Send endpoint event
	endpointURL := fmt.Sprintf("/mcp/%s/message?sessionId=%s", comboName, sessionID)
	fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", endpointURL)
	w.Flush()

	ctx := c.Request.Context()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	c.Stream(func(w io.Writer) bool {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			fmt.Fprintf(w, ": ping\n\n")
			return true
		case msg, ok := <-session.msgChan:
			if !ok {
				return false
			}
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", string(msg))
			return true
		}
	})
}

// HandleMessage handles incoming JSON-RPC POST messages for an active SSE session.
func (h *Handler) HandleMessage(c *gin.Context) {
	comboName := c.Param("combo")
	combo, ok := h.gw.GetCombo(comboName)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "combo not found: " + comboName})
		return
	}

	sessionID := c.Query("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sessionId query parameter required"})
		return
	}

	sseMgr.mu.RLock()
	session, exists := sseMgr.sessions[sessionID]
	sseMgr.mu.RUnlock()

	if !exists || session.comboName != comboName {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found or expired"})
		return
	}

	var req protocol.Request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, protocol.NewErrorResponse(nil, protocol.CodeParseError, "parse error: "+err.Error(), nil))
		return
	}

	// Use user_id from session or allow override from request header if present
	userID := session.userID
	if hID := strings.TrimSpace(c.GetHeader(combo.UserIDHeader())); hID != "" {
		userID = hID
	}

	scheme := "http"
	if c.Request.TLS != nil || c.Request.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := c.Request.Host
	if fHost := c.Request.Header.Get("X-Forwarded-Host"); fHost != "" {
		host = fHost
	}
	reqBaseURL := fmt.Sprintf("%s://%s", scheme, host)
	ctx := context.WithValue(c.Request.Context(), auth.BaseURLContextKey, reqBaseURL)

	resp := h.dispatchMethod(ctx, combo, &req, userID, nil, "sse")
	if resp != nil {
		data, err := json.Marshal(resp)
		if err == nil {
			select {
			case session.msgChan <- data:
			case <-session.done:
			}
		}
	}

	c.Status(http.StatusAccepted)
}
