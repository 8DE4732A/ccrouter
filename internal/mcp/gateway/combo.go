package gateway

import (
	"context"
	"fmt"
	"log"
	"strings"

	"ccrouter/internal/config"
	"ccrouter/internal/mcp/auth"
	"ccrouter/internal/mcp/client"
	"ccrouter/internal/mcp/protocol"
)

// Combo represents a virtual MCP server aggregating tools, resources, and prompts from member providers.
type Combo struct {
	cfg         config.McpComboConfig
	authManager *auth.Manager
	clients     map[string]client.Client
}

// NewCombo creates a new Combo instance.
func NewCombo(cfg config.McpComboConfig, am *auth.Manager, clients map[string]client.Client) *Combo {
	if cfg.UserIDHeader == "" {
		cfg.UserIDHeader = "X-User-Id"
	}
	return &Combo{
		cfg:         cfg,
		authManager: am,
		clients:     clients,
	}
}

func (c *Combo) Name() string         { return c.cfg.Name }
func (c *Combo) UserIDHeader() string { return c.cfg.UserIDHeader }
func (c *Combo) Description() string  { return c.cfg.Description }

func isItemAllowed(item string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		if p == "*" || p == item {
			return true
		}
	}
	return false
}

// ListTools aggregates and filters tools across all members, applying prefixes.
func (c *Combo) ListTools(ctx context.Context) ([]protocol.Tool, error) {
	var allTools []protocol.Tool
	for _, m := range c.cfg.Members {
		cli, ok := c.clients[m.Provider]
		if !ok {
			continue
		}

		tools, err := cli.ListTools(ctx)
		if err != nil {
			log.Printf("[mcp-combo:%s] provider %q failed to list tools: %v", c.cfg.Name, m.Provider, err)
			continue
		}

		for _, t := range tools {
			if !isItemAllowed(t.Name, m.Tools) {
				continue
			}

			exposedName := t.Name
			desc := t.Description
			if m.Prefix != "" {
				exposedName = m.Prefix + "__" + t.Name
				if desc != "" {
					desc = fmt.Sprintf("[%s] %s", m.Prefix, desc)
				} else {
					desc = fmt.Sprintf("[%s]", m.Prefix)
				}
			}

			allTools = append(allTools, protocol.Tool{
				Name:        exposedName,
				Description: desc,
				InputSchema: t.InputSchema,
			})
		}
	}
	return allTools, nil
}

// ResolveTool routes an exposed tool name back to its provider and original tool name.
func (c *Combo) ResolveTool(virtualName string) (provider string, originalName string, err error) {
	return c.resolveTool(virtualName)
}

// resolveTool routes an exposed tool name back to its provider and original tool name.
func (c *Combo) resolveTool(virtualName string) (provider string, originalName string, err error) {
	for _, m := range c.cfg.Members {
		if m.Prefix != "" {
			prefix := m.Prefix + "__"
			if strings.HasPrefix(virtualName, prefix) {
				orig := strings.TrimPrefix(virtualName, prefix)
				if isItemAllowed(orig, m.Tools) {
					return m.Provider, orig, nil
				}
			}
		} else {
			// No prefix
			if isItemAllowed(virtualName, m.Tools) {
				return m.Provider, virtualName, nil
			}
		}
	}
	return "", "", fmt.Errorf("tool %q not found in combo %q", virtualName, c.cfg.Name)
}

// CallTool routes a tool call, resolving credentials and forwarding to the upstream client.
func (c *Combo) CallTool(ctx context.Context, virtualName string, args map[string]any, userID string) (*protocol.CallToolResult, *protocol.Error) {
	providerName, originalName, err := c.resolveTool(virtualName)
	if err != nil {
		return nil, protocol.NewError(protocol.CodeMethodNotFound, err.Error(), nil)
	}

	cli, ok := c.clients[providerName]
	if !ok {
		return nil, protocol.NewError(protocol.CodeInternalError, fmt.Sprintf("provider %q client unavailable", providerName), nil)
	}

	// Resolve user credential
	var cred *auth.Credential
	if c.authManager != nil {
		resolved, err := c.authManager.ResolveCredential(ctx, providerName, userID)
		if err != nil {
			if authErr, ok := err.(*auth.AuthRequiredError); ok {
				return nil, protocol.NewError(protocol.CodeAuthRequired, fmt.Sprintf("Authentication required for provider: %s", authErr.Provider), map[string]any{
					"provider": authErr.Provider,
					"user_id":  authErr.UserID,
					"auth_url": authErr.AuthURL,
				})
			}
			return nil, protocol.NewError(protocol.CodeInternalError, fmt.Sprintf("credential resolution failed: %v", err), nil)
		}
		cred = resolved
	}

	result, err := cli.CallTool(ctx, originalName, args, cred)
	if err != nil {
		if rpcErr, ok := err.(*protocol.Error); ok {
			return nil, rpcErr
		}
		return nil, protocol.NewError(protocol.CodeInternalError, err.Error(), nil)
	}
	return result, nil
}

// ListResources aggregates and filters resources across all members.
func (c *Combo) ListResources(ctx context.Context) ([]protocol.Resource, error) {
	var all []protocol.Resource
	for _, m := range c.cfg.Members {
		cli, ok := c.clients[m.Provider]
		if !ok {
			continue
		}
		res, err := cli.ListResources(ctx)
		if err != nil {
			log.Printf("[mcp-combo:%s] provider %q failed to list resources: %v", c.cfg.Name, m.Provider, err)
			continue
		}
		for _, r := range res {
			if isItemAllowed(r.URI, m.Resources) || isItemAllowed(r.Name, m.Resources) {
				all = append(all, r)
			}
		}
	}
	return all, nil
}

// ReadResource routes a resource read call.
func (c *Combo) ReadResource(ctx context.Context, uri string, userID string) (*protocol.ReadResourceResult, *protocol.Error) {
	for _, m := range c.cfg.Members {
		cli, ok := c.clients[m.Provider]
		if !ok {
			continue
		}

		// Check if uri is directly allowed or matches any resource Name in member (Review point #7)
		allowed := isItemAllowed(uri, m.Resources)
		if !allowed && len(m.Resources) > 0 {
			resList, err := cli.ListResources(ctx)
			if err == nil {
				for _, r := range resList {
					if r.URI == uri && isItemAllowed(r.Name, m.Resources) {
						allowed = true
						break
					}
				}
			}
		}
		if !allowed {
			continue
		}

		var cred *auth.Credential
		if c.authManager != nil {
			resolved, err := c.authManager.ResolveCredential(ctx, m.Provider, userID)
			if err != nil {
				if authErr, ok := err.(*auth.AuthRequiredError); ok {
					return nil, protocol.NewError(protocol.CodeAuthRequired, fmt.Sprintf("Authentication required for provider: %s", authErr.Provider), map[string]any{
						"provider": authErr.Provider,
						"user_id":  authErr.UserID,
						"auth_url": authErr.AuthURL,
					})
				}
				return nil, protocol.NewError(protocol.CodeInternalError, err.Error(), nil)
			}
			cred = resolved
		}

		res, err := cli.ReadResource(ctx, uri, cred)
		if err != nil {
			if rpcErr, ok := err.(*protocol.Error); ok {
				return nil, rpcErr
			}
			return nil, protocol.NewError(protocol.CodeInternalError, err.Error(), nil)
		}
		return res, nil
	}
	return nil, protocol.NewError(protocol.CodeMethodNotFound, fmt.Sprintf("resource %q not found", uri), nil)
}

// ListPrompts aggregates and filters prompts across all members.
func (c *Combo) ListPrompts(ctx context.Context) ([]protocol.Prompt, error) {
	var all []protocol.Prompt
	for _, m := range c.cfg.Members {
		cli, ok := c.clients[m.Provider]
		if !ok {
			continue
		}
		prompts, err := cli.ListPrompts(ctx)
		if err != nil {
			log.Printf("[mcp-combo:%s] provider %q failed to list prompts: %v", c.cfg.Name, m.Provider, err)
			continue
		}
		for _, p := range prompts {
			if !isItemAllowed(p.Name, m.Prompts) {
				continue
			}
			name := p.Name
			if m.Prefix != "" {
				name = m.Prefix + "__" + p.Name
			}
			all = append(all, protocol.Prompt{
				Name:        name,
				Description: p.Description,
				Arguments:   p.Arguments,
			})
		}
	}
	return all, nil
}

// GetPrompt routes a prompt get call.
func (c *Combo) GetPrompt(ctx context.Context, virtualName string, args map[string]string, userID string) (*protocol.GetPromptResult, *protocol.Error) {
	for _, m := range c.cfg.Members {
		cli, ok := c.clients[m.Provider]
		if !ok {
			continue
		}
		orig := virtualName
		if m.Prefix != "" {
			prefix := m.Prefix + "__"
			if !strings.HasPrefix(virtualName, prefix) {
				continue
			}
			orig = strings.TrimPrefix(virtualName, prefix)
		}
		if !isItemAllowed(orig, m.Prompts) {
			continue
		}

		var cred *auth.Credential
		if c.authManager != nil {
			resolved, err := c.authManager.ResolveCredential(ctx, m.Provider, userID)
			if err != nil {
				if authErr, ok := err.(*auth.AuthRequiredError); ok {
					return nil, protocol.NewError(protocol.CodeAuthRequired, fmt.Sprintf("Authentication required for provider: %s", authErr.Provider), map[string]any{
						"provider": authErr.Provider,
						"user_id":  authErr.UserID,
						"auth_url": authErr.AuthURL,
					})
				}
				return nil, protocol.NewError(protocol.CodeInternalError, err.Error(), nil)
			}
			cred = resolved
		}

		res, err := cli.GetPrompt(ctx, orig, args, cred)
		if err != nil {
			if rpcErr, ok := err.(*protocol.Error); ok {
				return nil, rpcErr
			}
			return nil, protocol.NewError(protocol.CodeInternalError, err.Error(), nil)
		}
		return res, nil
	}
	return nil, protocol.NewError(protocol.CodeMethodNotFound, fmt.Sprintf("prompt %q not found", virtualName), nil)
}
