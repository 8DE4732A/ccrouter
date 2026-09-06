package gateway

import (
	"fmt"
	"net/http"
	"sync"

	"ccrouter/internal/config"
	"ccrouter/internal/db"
	"ccrouter/internal/mcp/auth"
	"ccrouter/internal/mcp/client"
)

// Gateway owns the full set of MCP providers, combos, and authentication services.
type Gateway struct {
	mu          sync.RWMutex
	recorder    *db.Recorder
	authManager *auth.Manager
	clients     map[string]client.Client
	combos      map[string]*Combo
	providers   []config.McpProviderConfig
	comboCfgs   []config.McpComboConfig
}

// New creates and initializes an MCP Gateway from configuration.
func New(cfg *config.AppConfig, recorder *db.Recorder, providerClients map[string]*http.Client, baseURL string) (*Gateway, error) {
	clients := make(map[string]client.Client, len(cfg.McpProviders))
	for _, p := range cfg.McpProviders {
		var cli client.Client
		httpClient := providerClients[p.Name]
		switch p.Transport {
		case "stdio":
			cli = client.NewStdioClient(p)
		case "sse":
			cli = client.NewSSEClient(p, httpClient)
		case "streamablehttp":
			cli = client.NewHTTPClient(p, httpClient)
		default:
			return nil, fmt.Errorf("unsupported mcp transport %q for provider %q", p.Transport, p.Name)
		}
		clients[p.Name] = cli
	}

	authMgr := auth.NewManager(recorder, cfg.McpProviders, providerClients, baseURL, cfg.General.AdminPassword)

	combos := make(map[string]*Combo, len(cfg.McpCombos))
	for _, c := range cfg.McpCombos {
		combos[c.Name] = NewCombo(c, authMgr, clients)
	}

	return &Gateway{
		recorder:    recorder,
		authManager: authMgr,
		clients:     clients,
		combos:      combos,
		providers:   cfg.McpProviders,
		comboCfgs:   cfg.McpCombos,
	}, nil
}

// Recorder returns the underlying database recorder.
func (g *Gateway) Recorder() *db.Recorder {
	return g.recorder
}

// RecordRequest enqueues an MCP request record.
func (g *Gateway) RecordRequest(row *db.McpRow) {
	if g != nil && g.recorder != nil && row != nil {
		g.recorder.RecordMcp(row)
	}
}

// GetCombo returns the Combo for a given name.
func (g *Gateway) GetCombo(name string) (*Combo, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	c, ok := g.combos[name]
	return c, ok
}

// Combos returns all loaded Combos.
func (g *Gateway) Combos() []*Combo {
	g.mu.RLock()
	defer g.mu.RUnlock()
	list := make([]*Combo, 0, len(g.combos))
	for _, c := range g.combos {
		list = append(list, c)
	}
	return list
}

// Providers returns all MCP provider configs.
func (g *Gateway) Providers() []config.McpProviderConfig {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.providers
}

// Client returns the client for an upstream provider.
func (g *Gateway) Client(name string) (client.Client, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	cli, ok := g.clients[name]
	return cli, ok
}

// AuthManager returns the authentication manager.
func (g *Gateway) AuthManager() *auth.Manager {
	return g.authManager
}

// Close gracefully closes all upstream provider clients and auth manager.
func (g *Gateway) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, cli := range g.clients {
		_ = cli.Close()
	}
	if g.authManager != nil {
		g.authManager.Close()
	}
}
