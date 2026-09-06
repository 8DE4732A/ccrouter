package gateway

import (
	"context"
	"path/filepath"
	"testing"

	"ccrouter/internal/config"
	"ccrouter/internal/db"
	"ccrouter/internal/mcp/auth"
	"ccrouter/internal/mcp/client"
	"ccrouter/internal/mcp/protocol"
)

// MockClient implements client.Client for testing
type MockClient struct {
	name      string
	transport string
	tools     []protocol.Tool
	lastCred  *auth.Credential
	lastArgs  map[string]any
}

func (m *MockClient) Name() string      { return m.name }
func (m *MockClient) Transport() string { return m.transport }
func (m *MockClient) Initialize(ctx context.Context) (*protocol.InitializeResult, error) {
	return &protocol.InitializeResult{
		ProtocolVersion: protocol.LatestProtocolVersion,
		ServerInfo:      protocol.Implementation{Name: m.name, Version: "1.0"},
	}, nil
}
func (m *MockClient) ListTools(ctx context.Context) ([]protocol.Tool, error) {
	return m.tools, nil
}
func (m *MockClient) CallTool(ctx context.Context, name string, args map[string]any, cred *auth.Credential) (*protocol.CallToolResult, error) {
	m.lastCred = cred
	m.lastArgs = args
	return &protocol.CallToolResult{
		Content: []protocol.Content{
			{Type: "text", Text: "executed " + name},
		},
	}, nil
}
func (m *MockClient) ListResources(ctx context.Context) ([]protocol.Resource, error) { return nil, nil }
func (m *MockClient) ReadResource(ctx context.Context, uri string, cred *auth.Credential) (*protocol.ReadResourceResult, error) {
	return nil, nil
}
func (m *MockClient) ListPrompts(ctx context.Context) ([]protocol.Prompt, error) { return nil, nil }
func (m *MockClient) GetPrompt(ctx context.Context, name string, args map[string]string, cred *auth.Credential) (*protocol.GetPromptResult, error) {
	return nil, nil
}
func (m *MockClient) Ping(ctx context.Context) error { return nil }
func (m *MockClient) Close() error                   { return nil }

func TestComboAggregationAndRouting(t *testing.T) {
	mockP1 := &MockClient{
		name: "fs",
		tools: []protocol.Tool{
			{Name: "read_file", Description: "Read a file"},
			{Name: "delete_file", Description: "Delete a file"},
		},
	}
	mockP2 := &MockClient{
		name: "github",
		tools: []protocol.Tool{
			{Name: "create_issue", Description: "Create issue"},
		},
	}

	clients := map[string]client.Client{
		"fs":     mockP1,
		"github": mockP2,
	}

	comboCfg := config.McpComboConfig{
		Name:         "all-tools",
		UserIDHeader: "X-User-Id",
		Members: []config.McpComboMemberConfig{
			{
				Provider: "fs",
				Prefix:   "fs",
				Tools:    []string{"read_file"}, // only read_file allowed
			},
			{
				Provider: "github",
				Prefix:   "gh",
				Tools:    []string{"*"}, // all tools
			},
		},
	}

	c := NewCombo(comboCfg, nil, clients)
	ctx := context.Background()

	// 1. Test ListTools
	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	if tools[0].Name != "fs__read_file" || tools[1].Name != "gh__create_issue" {
		t.Fatalf("unexpected tool names: %#v", tools)
	}

	// 2. Call allowed tool
	res, rpcErr := c.CallTool(ctx, "fs__read_file", map[string]any{"path": "/tmp"}, "")
	if rpcErr != nil {
		t.Fatalf("unexpected call error: %v", rpcErr)
	}
	if len(res.Content) != 1 || res.Content[0].Text != "executed read_file" {
		t.Fatalf("unexpected call result: %#v", res)
	}

	// 3. Call filtered-out tool
	_, rpcErr = c.CallTool(ctx, "fs__delete_file", nil, "")
	if rpcErr == nil || rpcErr.Code != protocol.CodeMethodNotFound {
		t.Fatalf("expected method not found error for filtered tool, got: %#v", rpcErr)
	}
}

func TestComboIsolatedAuthRequirement(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "auth-test.db")
	rec, err := db.NewRecorder(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()

	providers := []config.McpProviderConfig{
		{
			Name:      "github",
			Transport: "streamablehttp",
			URL:       "https://api.github.com",
			AuthMode:  "isolated",
			Auth: &config.McpAuthConfig{
				Type:             "oauth2",
				ClientID:         "cid",
				AuthorizationURL: "https://github.com/auth",
				TokenURL:         "https://github.com/token",
			},
		},
	}

	am := auth.NewManager(rec, providers, nil, "https://gateway.example.com")
	mockGh := &MockClient{
		name:  "github",
		tools: []protocol.Tool{{Name: "list_repos"}},
	}
	clients := map[string]client.Client{"github": mockGh}

	comboCfg := config.McpComboConfig{
		Name: "dev",
		Members: []config.McpComboMemberConfig{
			{Provider: "github", Prefix: "gh", Tools: []string{"*"}},
		},
	}
	combo := NewCombo(comboCfg, am, clients)
	ctx := context.Background()

	// 1. Call without user authentication -> must return standard CodeAuthRequired (-32001) error
	_, rpcErr := combo.CallTool(ctx, "gh__list_repos", nil, "alice")
	if rpcErr == nil {
		t.Fatal("expected auth required error, got nil")
	}
	if rpcErr.Code != protocol.CodeAuthRequired {
		t.Fatalf("expected error code %d, got %d", protocol.CodeAuthRequired, rpcErr.Code)
	}
	dataMap, ok := rpcErr.Data.(map[string]any)
	if !ok || dataMap["auth_url"] == "" || dataMap["user_id"] != "alice" {
		t.Fatalf("unexpected error data: %#v", rpcErr.Data)
	}

	// 2. Save token for alice -> next call succeeds and passes token
	exp := int64(9999999999)
	_ = rec.SaveMcpToken(&db.McpUserToken{
		Provider:    "github",
		UserID:      "alice",
		AccessToken: "alice-valid-token",
		ExpiresAt:   &exp,
	})

	callRes, rpcErr := combo.CallTool(ctx, "gh__list_repos", nil, "alice")
	if rpcErr != nil {
		t.Fatalf("unexpected call error with valid token: %v", rpcErr)
	}
	if callRes == nil {
		t.Fatal("expected call result, got nil")
	}
	if mockGh.lastCred == nil || mockGh.lastCred.Token != "alice-valid-token" {
		t.Fatalf("credential not injected properly: %#v", mockGh.lastCred)
	}
}
