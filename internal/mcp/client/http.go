package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ccrouter/internal/config"
	"ccrouter/internal/mcp/auth"
	"ccrouter/internal/mcp/protocol"
)

// HTTPClient communicates with an upstream MCP server via modern Streamable HTTP (single endpoint POST).
type HTTPClient struct {
	cfg        config.McpProviderConfig
	httpClient *http.Client
	seq        int64
	mu         sync.Mutex
	initMu     sync.Mutex
	initRes    *protocol.InitializeResult
}

// NewHTTPClient creates a new HTTPClient.
func NewHTTPClient(cfg config.McpProviderConfig, client *http.Client) *HTTPClient {
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTPClient{
		cfg:        cfg,
		httpClient: client,
	}
}

func (c *HTTPClient) Name() string      { return c.cfg.Name }
func (c *HTTPClient) Transport() string { return "streamablehttp" }

func (c *HTTPClient) sendRequest(ctx context.Context, method string, params any, cred *auth.Credential) (*protocol.Response, error) {
	resp, err := c.doSendRequest(ctx, method, params, cred)
	if err != nil {
		var rpcErr *protocol.Error
		if errors.As(err, &rpcErr) && rpcErr.Code == protocol.CodeServerNotInitialized && method != "initialize" {
			c.mu.Lock()
			c.initRes = nil
			c.mu.Unlock()
			return c.doSendRequest(ctx, method, params, cred)
		}
		return nil, err
	}
	return resp, nil
}

func (c *HTTPClient) doSendRequest(ctx context.Context, method string, params any, cred *auth.Credential) (*protocol.Response, error) {
	if method != "initialize" && method != "notifications/initialized" {
		if err := c.ensureInitialized(ctx); err != nil {
			return nil, fmt.Errorf("ensure initialized: %w", err)
		}
	}

	id := atomic.AddInt64(&c.seq, 1)
	var rawParams json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		rawParams = data
	}

	reqPayload := protocol.Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  rawParams,
	}

	bodyBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, err
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline && c.cfg.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(c.cfg.TimeoutSeconds)*time.Second)
		defer cancel()
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.URL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range c.cfg.Headers {
		httpReq.Header.Set(k, v)
	}
	if cred != nil && cred.HeaderName != "" && cred.HeaderValue != "" {
		httpReq.Header.Set(cred.HeaderName, cred.HeaderValue)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("post to mcp streamablehttp endpoint failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("upstream mcp error %d: %s", resp.StatusCode, string(body))
	}

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") {
		// Parse SSE stream response
		return c.parseStreamResponse(resp.Body, id)
	}

	// Normal JSON response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	var res protocol.Response
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("unmarshal json-rpc response: %w", err)
	}
	if res.Error != nil {
		return nil, res.Error
	}
	return &res, nil
}

func (c *HTTPClient) parseStreamResponse(rc io.Reader, targetID int64) (*protocol.Response, error) {
	scanner := bufio.NewScanner(rc)
	// Buffer size up to 10MB for large tool responses
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	var dataBuffer strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			data := dataBuffer.String()
			dataBuffer.Reset()
			if data != "" {
				var res protocol.Response
				if err := json.Unmarshal([]byte(data), &res); err == nil {
					if idNum, ok := toInt64(res.ID); ok && idNum == targetID {
						if res.Error != nil {
							return nil, res.Error
						}
						return &res, nil
					}
				}
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			d := strings.TrimPrefix(line, "data:")
			if strings.HasPrefix(d, " ") {
				d = d[1:]
			}
			dataBuffer.WriteString(d)
		}
	}
	return nil, errors.New("stream closed without matching response")
}

func (c *HTTPClient) sendNotification(ctx context.Context, method string, params any) error {
	var rawParams json.RawMessage
	if params != nil {
		data, _ := json.Marshal(params)
		rawParams = data
	}

	reqPayload := protocol.Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  rawParams,
	}

	bodyBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.URL, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range c.cfg.Headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

func (c *HTTPClient) ensureInitialized(ctx context.Context) error {
	c.mu.Lock()
	if c.initRes != nil {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	c.initMu.Lock()
	defer c.initMu.Unlock()

	_, err := c.initializeLocked(ctx)
	return err
}

func (c *HTTPClient) Initialize(ctx context.Context) (*protocol.InitializeResult, error) {
	c.initMu.Lock()
	defer c.initMu.Unlock()
	return c.initializeLocked(ctx)
}

func (c *HTTPClient) initializeLocked(ctx context.Context) (*protocol.InitializeResult, error) {
	c.mu.Lock()
	if c.initRes != nil {
		res := c.initRes
		c.mu.Unlock()
		return res, nil
	}
	c.mu.Unlock()

	params := protocol.InitializeParams{
		ProtocolVersion: protocol.LatestProtocolVersion,
		ClientInfo: protocol.Implementation{
			Name:    "ccrouter",
			Version: "0.7.0",
		},
		Capabilities: protocol.ClientCapabilities{},
	}

	resp, err := c.doSendRequest(ctx, "initialize", params, nil)
	if err != nil {
		return nil, err
	}

	resBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, err
	}
	var initRes protocol.InitializeResult
	if err := json.Unmarshal(resBytes, &initRes); err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.initRes = &initRes
	c.mu.Unlock()

	// Send initialized notification per MCP specification
	_ = c.sendNotification(ctx, "notifications/initialized", map[string]any{})

	return &initRes, nil
}

func (c *HTTPClient) ListTools(ctx context.Context) ([]protocol.Tool, error) {
	resp, err := c.sendRequest(ctx, "tools/list", map[string]any{}, nil)
	if err != nil {
		return nil, err
	}
	data, _ := json.Marshal(resp.Result)
	var res protocol.ListToolsResult
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}
	return res.Tools, nil
}

func (c *HTTPClient) CallTool(ctx context.Context, name string, args map[string]any, cred *auth.Credential) (*protocol.CallToolResult, error) {
	params := protocol.CallToolParams{
		Name:      name,
		Arguments: args,
	}
	resp, err := c.sendRequest(ctx, "tools/call", params, cred)
	if err != nil {
		return nil, err
	}
	data, _ := json.Marshal(resp.Result)
	var res protocol.CallToolResult
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *HTTPClient) ListResources(ctx context.Context) ([]protocol.Resource, error) {
	resp, err := c.sendRequest(ctx, "resources/list", map[string]any{}, nil)
	if err != nil {
		return nil, err
	}
	data, _ := json.Marshal(resp.Result)
	var res protocol.ListResourcesResult
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}
	return res.Resources, nil
}

func (c *HTTPClient) ReadResource(ctx context.Context, uri string, cred *auth.Credential) (*protocol.ReadResourceResult, error) {
	params := protocol.ReadResourceParams{URI: uri}
	resp, err := c.sendRequest(ctx, "resources/read", params, cred)
	if err != nil {
		return nil, err
	}
	data, _ := json.Marshal(resp.Result)
	var res protocol.ReadResourceResult
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *HTTPClient) ListPrompts(ctx context.Context) ([]protocol.Prompt, error) {
	resp, err := c.sendRequest(ctx, "prompts/list", map[string]any{}, nil)
	if err != nil {
		return nil, err
	}
	data, _ := json.Marshal(resp.Result)
	var res protocol.ListPromptsResult
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}
	return res.Prompts, nil
}

func (c *HTTPClient) GetPrompt(ctx context.Context, name string, args map[string]string, cred *auth.Credential) (*protocol.GetPromptResult, error) {
	params := protocol.GetPromptParams{
		Name:      name,
		Arguments: args,
	}
	resp, err := c.sendRequest(ctx, "prompts/get", params, cred)
	if err != nil {
		return nil, err
	}
	data, _ := json.Marshal(resp.Result)
	var res protocol.GetPromptResult
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *HTTPClient) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := c.sendRequest(ctx, "ping", nil, nil)
	return err
}

func (c *HTTPClient) Close() error {
	return nil
}
