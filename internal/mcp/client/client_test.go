package client

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"ccrouter/internal/config"
	"ccrouter/internal/mcp/auth"
	"ccrouter/internal/mcp/protocol"
)

func TestHTTPClient(t *testing.T) {
	// Mock Streamable HTTP MCP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req protocol.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			res := protocol.NewResponse(req.ID, protocol.InitializeResult{
				ProtocolVersion: protocol.LatestProtocolVersion,
				ServerInfo: protocol.Implementation{
					Name:    "mock-server",
					Version: "1.0",
				},
			})
			_ = json.NewEncoder(w).Encode(res)
		case "tools/list":
			res := protocol.NewResponse(req.ID, protocol.ListToolsResult{
				Tools: []protocol.Tool{
					{Name: "echo", Description: "Echoes input"},
				},
			})
			_ = json.NewEncoder(w).Encode(res)
		case "tools/call":
			// Check auth header
			authHeader := r.Header.Get("Authorization")
			if authHeader != "Bearer secret-token" {
				res := protocol.NewErrorResponse(req.ID, protocol.CodeAuthRequired, "Unauthorized", nil)
				_ = json.NewEncoder(w).Encode(res)
				return
			}
			res := protocol.NewResponse(req.ID, protocol.CallToolResult{
				Content: []protocol.Content{
					{Type: "text", Text: "echo output"},
				},
			})
			_ = json.NewEncoder(w).Encode(res)
		default:
			res := protocol.NewErrorResponse(req.ID, protocol.CodeMethodNotFound, "method not found", nil)
			_ = json.NewEncoder(w).Encode(res)
		}
	}))
	defer server.Close()

	cfg := config.McpProviderConfig{
		Name:      "test-http",
		Transport: "streamablehttp",
		URL:       server.URL,
	}
	c := NewHTTPClient(cfg, server.Client())
	ctx := context.Background()

	// 1. Initialize
	initRes, err := c.Initialize(ctx)
	if err != nil {
		t.Fatalf("initialize error: %v", err)
	}
	if initRes.ServerInfo.Name != "mock-server" {
		t.Fatalf("unexpected server info: %#v", initRes)
	}

	// 2. List tools
	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("list tools error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("unexpected tools: %#v", tools)
	}

	// 3. Call tool with cred
	cred := &auth.Credential{
		HeaderName:  "Authorization",
		HeaderValue: "Bearer secret-token",
	}
	callRes, err := c.CallTool(ctx, "echo", map[string]any{"msg": "hi"}, cred)
	if err != nil {
		t.Fatalf("call tool error: %v", err)
	}
	if len(callRes.Content) != 1 || callRes.Content[0].Text != "echo output" {
		t.Fatalf("unexpected call result: %#v", callRes)
	}

	// 4. Call tool without cred -> expect error
	_, err = c.CallTool(ctx, "echo", map[string]any{"msg": "hi"}, nil)
	if err == nil {
		t.Fatal("expected auth error, got nil")
	}
}

func TestSSEClient(t *testing.T) {
	var messageURL string
	var msgChan = make(chan []byte, 10)

	// Mock SSE server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			// SSE Stream
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)

			flusher, ok := w.(http.Flusher)
			if !ok {
				return
			}

			// Send endpoint event
			fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", messageURL)
			flusher.Flush()

			// Listen for outgoing messages to write to SSE stream
			for msg := range msgChan {
				fmt.Fprintf(w, "event: message\ndata: %s\n\n", string(msg))
				flusher.Flush()
			}
			return
		}

		if r.Method == http.MethodPost {
			var req protocol.Request
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusAccepted)

			// Reply asynchronously via SSE
			if req.Method == "initialize" {
				res := protocol.NewResponse(req.ID, protocol.InitializeResult{
					ProtocolVersion: protocol.LatestProtocolVersion,
					ServerInfo: protocol.Implementation{
						Name:    "sse-server",
						Version: "1.0",
					},
				})
				bytes, _ := json.Marshal(res)
				msgChan <- bytes
			}
			return
		}
	}))
	defer server.Close()
	defer close(msgChan)

	messageURL = server.URL + "/message"

	cfg := config.McpProviderConfig{
		Name:      "test-sse",
		Transport: "sse",
		URL:       server.URL,
	}
	c := NewSSEClient(cfg, server.Client())
	defer c.Close()

	ctx := context.Background()
	initRes, err := c.Initialize(ctx)
	if err != nil {
		t.Fatalf("sse initialize error: %v", err)
	}
	if initRes.ServerInfo.Name != "sse-server" {
		t.Fatalf("unexpected server info: %#v", initRes)
	}
}

func TestStrictHTTPClientLazyInitialize(t *testing.T) {
	var mu sync.Mutex
	initialized := false
	initNotificationReceived := false

	// Strict mock server that fails any request with -32002 if not initialized
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req protocol.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		mu.Lock()
		defer mu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		if req.Method == "initialize" {
			res := protocol.NewResponse(req.ID, protocol.InitializeResult{
				ProtocolVersion: protocol.LatestProtocolVersion,
				ServerInfo: protocol.Implementation{
					Name:    "strict-server",
					Version: "1.0",
				},
			})
			_ = json.NewEncoder(w).Encode(res)
			return
		}

		if req.Method == "notifications/initialized" {
			initialized = true
			initNotificationReceived = true
			return
		}

		if !initialized {
			res := protocol.NewErrorResponse(req.ID, protocol.CodeServerNotInitialized, "Server not initialized", nil)
			_ = json.NewEncoder(w).Encode(res)
			return
		}

		if req.Method == "tools/list" {
			res := protocol.NewResponse(req.ID, protocol.ListToolsResult{
				Tools: []protocol.Tool{
					{Name: "strict_tool", Description: "A tool from strict server"},
				},
			})
			_ = json.NewEncoder(w).Encode(res)
			return
		}

		res := protocol.NewErrorResponse(req.ID, protocol.CodeMethodNotFound, "method not found", nil)
		_ = json.NewEncoder(w).Encode(res)
	}))
	defer server.Close()

	cfg := config.McpProviderConfig{
		Name:      "strict-prov",
		Transport: "streamablehttp",
		URL:       server.URL,
	}

	c := NewHTTPClient(cfg, server.Client())
	ctx := context.Background()

	// 1. Call ListTools directly WITHOUT calling c.Initialize() first.
	// The client MUST lazily execute initialize + notifications/initialized before tools/list.
	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("lazy initialize ListTools failed on strict server: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "strict_tool" {
		t.Fatalf("unexpected tools: %#v", tools)
	}

	mu.Lock()
	if !initNotificationReceived || !initialized {
		t.Fatalf("strict server was not properly initialized: initNotificationReceived=%v, initialized=%v", initNotificationReceived, initialized)
	}
	mu.Unlock()

	// 2. Simulate upstream server restart/state wipe (reset initialized to false)
	mu.Lock()
	initialized = false
	initNotificationReceived = false
	mu.Unlock()

	// The client will get -32002 on the next call, clear its initRes, re-handshake, and succeed!
	tools2, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("recovery on -32002 failed: %v", err)
	}
	if len(tools2) != 1 || tools2[0].Name != "strict_tool" {
		t.Fatalf("unexpected tools after recovery: %#v", tools2)
	}

	mu.Lock()
	if !initNotificationReceived || !initialized {
		t.Fatalf("strict server was not re-initialized after recovery: initNotificationReceived=%v, initialized=%v", initNotificationReceived, initialized)
	}
	mu.Unlock()
}

// Helper process for strict stdio mock testing
func TestHelperStrictProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)

	scanner := bufio.NewScanner(os.Stdin)
	initialized := false

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req protocol.Request
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}

		if req.Method == "initialize" {
			res := protocol.NewResponse(req.ID, protocol.InitializeResult{
				ProtocolVersion: protocol.LatestProtocolVersion,
				ServerInfo: protocol.Implementation{
					Name:    "strict-stdio-server",
					Version: "1.0",
				},
			})
			bytes, _ := json.Marshal(res)
			fmt.Println(string(bytes))
			continue
		}

		if req.Method == "notifications/initialized" {
			initialized = true
			continue
		}

		if !initialized {
			res := protocol.NewErrorResponse(req.ID, protocol.CodeServerNotInitialized, "Server not initialized", nil)
			bytes, _ := json.Marshal(res)
			fmt.Println(string(bytes))
			continue
		}

		if req.Method == "tools/list" {
			res := protocol.NewResponse(req.ID, protocol.ListToolsResult{
				Tools: []protocol.Tool{
					{Name: "stdio_tool", Description: "Tool from strict stdio server"},
				},
			})
			bytes, _ := json.Marshal(res)
			fmt.Println(string(bytes))
			continue
		}
	}
}

func TestStrictStdioClientLazyInitialize(t *testing.T) {
	cfg := config.McpProviderConfig{
		Name:      "strict-stdio",
		Transport: "stdio",
		Command:   os.Args[0],
		Args:      []string{"-test.run=TestHelperStrictProcess"},
		Env: map[string]string{
			"GO_WANT_HELPER_PROCESS": "1",
		},
	}

	c := NewStdioClient(cfg)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Call ListTools directly WITHOUT calling c.Initialize() first.
	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("lazy initialize ListTools failed on strict stdio server: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "stdio_tool" {
		t.Fatalf("unexpected tools from strict stdio: %#v", tools)
	}
}
