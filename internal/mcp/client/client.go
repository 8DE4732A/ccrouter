package client

import (
	"context"

	"ccrouter/internal/mcp/auth"
	"ccrouter/internal/mcp/protocol"
)

// Client is the interface implemented by upstream MCP drivers (stdio, sse, streamablehttp).
type Client interface {
	Name() string
	Transport() string
	Initialize(ctx context.Context) (*protocol.InitializeResult, error)
	ListTools(ctx context.Context) ([]protocol.Tool, error)
	CallTool(ctx context.Context, name string, args map[string]any, cred *auth.Credential) (*protocol.CallToolResult, error)
	ListResources(ctx context.Context) ([]protocol.Resource, error)
	ReadResource(ctx context.Context, uri string, cred *auth.Credential) (*protocol.ReadResourceResult, error)
	ListPrompts(ctx context.Context) ([]protocol.Prompt, error)
	GetPrompt(ctx context.Context, name string, args map[string]string, cred *auth.Credential) (*protocol.GetPromptResult, error)
	Ping(ctx context.Context) error
	Close() error
}
