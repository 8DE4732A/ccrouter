package transport

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ccrouter/internal/config"
	"ccrouter/internal/mcp/gateway"
	"ccrouter/internal/mcp/protocol"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func setupTestRouter() (*gin.Engine, *gateway.Gateway, error) {
	cfg := &config.AppConfig{
		McpProviders: []config.McpProviderConfig{
			{
				Name:      "test-p",
				Transport: "streamablehttp",
				URL:       "http://127.0.0.1",
			},
		},
		McpCombos: []config.McpComboConfig{
			{
				Name:         "tools-combo",
				Description:  "Combo test tools",
				UserIDHeader: "X-User-Id",
				Members: []config.McpComboMemberConfig{
					{
						Provider: "test-p",
						Prefix:   "tp",
						Tools:    []string{"*"},
					},
				},
			},
		},
	}

	gw, err := gateway.New(cfg, nil, nil, "http://localhost:8080")
	if err != nil {
		return nil, nil, err
	}

	handler := NewHandler(gw)
	r := gin.New()
	r.POST("/mcp/:combo", handler.HandleHTTP)
	r.GET("/mcp/:combo", handler.HandleMethodNotAllowed)
	r.DELETE("/mcp/:combo", handler.HandleMethodNotAllowed)
	r.GET("/mcp/:combo/sse", handler.HandleSSE)
	r.POST("/mcp/:combo/message", handler.HandleMessage)

	return r, gw, nil
}

func TestTransportMethodNotAllowed(t *testing.T) {
	router, _, err := setupTestRouter()
	if err != nil {
		t.Fatalf("setup router error: %v", err)
	}

	// GET /mcp/tools-combo should return 405 Method Not Allowed
	req, _ := http.NewRequest(http.MethodGet, "/mcp/tools-combo", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
	if w.Header().Get("Allow") != "POST" {
		t.Fatalf("expected Allow: POST, got %q", w.Header().Get("Allow"))
	}
}

func TestTransportInitializeHTTP(t *testing.T) {
	router, _, err := setupTestRouter()
	if err != nil {
		t.Fatalf("setup router error: %v", err)
	}

	initReq := protocol.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params:  nil,
	}
	body, _ := json.Marshal(initReq)
	req, _ := http.NewRequest(http.MethodPost, "/mcp/tools-combo", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp protocol.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	resMap, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %#v", resp.Result)
	}
	serverInfo, ok := resMap["serverInfo"].(map[string]any)
	if !ok || serverInfo["name"] != "ccrouter-mcp/tools-combo" {
		t.Fatalf("unexpected serverInfo: %#v", serverInfo)
	}
}

func TestTransportSSEStream(t *testing.T) {
	router, _, err := setupTestRouter()
	if err != nil {
		t.Fatalf("setup router error: %v", err)
	}

	// Start SSE server
	ts := httptest.NewServer(router)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/mcp/tools-combo/sse", nil)
	if err != nil {
		t.Fatalf("create req: %v", err)
	}

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("sse get error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	var endpointURL string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") && strings.Contains(line, "/mcp/tools-combo/message?sessionId=") {
			endpointURL = strings.TrimPrefix(line, "data: ")
			break
		}
	}

	if endpointURL == "" {
		t.Fatal("failed to receive SSE endpoint event")
	}

	// Send an initialize message via the returned endpoint URL
	initReq := protocol.Request{
		JSONRPC: "2.0",
		ID:      "sse-1",
		Method:  "initialize",
	}
	body, _ := json.Marshal(initReq)
	postReq, _ := http.NewRequest(http.MethodPost, ts.URL+endpointURL, bytes.NewReader(body))
	postReq.Header.Set("Content-Type", "application/json")

	postResp, err := ts.Client().Do(postReq)
	if err != nil {
		t.Fatalf("post message error: %v", err)
	}
	defer postResp.Body.Close()

	if postResp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d", postResp.StatusCode)
	}

	// Verify response received in SSE stream
	foundResponse := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") && strings.Contains(line, "ccrouter-mcp/tools-combo") {
			foundResponse = true
			break
		}
	}

	if !foundResponse {
		t.Fatal("expected to receive initialize response on SSE stream")
	}
}
