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
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ccrouter/internal/config"
	"ccrouter/internal/mcp/auth"
	"ccrouter/internal/mcp/protocol"
)

// SSEClient communicates with an upstream MCP server via Server-Sent Events (SSE).
type SSEClient struct {
	cfg        config.McpProviderConfig
	httpClient *http.Client

	mu          sync.RWMutex
	initMu      sync.Mutex
	endpointURL string
	sessionID   string
	seq         int64
	pending     map[int64]chan *protocol.Response
	closed      bool
	initRes     *protocol.InitializeResult

	cancelStream context.CancelFunc
}

// NewSSEClient creates a new SSEClient.
func NewSSEClient(cfg config.McpProviderConfig, client *http.Client) *SSEClient {
	if client == nil {
		client = http.DefaultClient
	}
	return &SSEClient{
		cfg:        cfg,
		httpClient: client,
		pending:    make(map[int64]chan *protocol.Response),
	}
}

func (c *SSEClient) Name() string      { return c.cfg.Name }
func (c *SSEClient) Transport() string { return "sse" }

// ensureConnection connects to the SSE stream and extracts the message endpoint.
func (c *SSEClient) ensureConnection(ctx context.Context) error {
	c.mu.RLock()
	hasEndpoint := c.endpointURL != ""
	c.mu.RUnlock()
	if hasEndpoint {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.endpointURL != "" {
		return nil
	}
	if c.closed {
		return errors.New("sse client is closed")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range c.cfg.Headers {
		req.Header.Set(k, v)
	}

	streamCtx, cancel := context.WithCancel(context.Background())
	c.cancelStream = cancel
	req = req.WithContext(streamCtx)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		cancel()
		return fmt.Errorf("connect to sse stream %s failed: %w", c.cfg.URL, err)
	}
	if resp.StatusCode != http.StatusOK {
		cancel()
		resp.Body.Close()
		return fmt.Errorf("connect to sse stream returned status %d", resp.StatusCode)
	}

	endpointCh := make(chan string, 1)
	errCh := make(chan error, 1)

	// Reader goroutine for SSE events
	go c.readSSEStream(resp.Body, endpointCh, errCh)

	select {
	case <-ctx.Done():
		cancel()
		return ctx.Err()
	case err := <-errCh:
		cancel()
		return err
	case ep := <-endpointCh:
		// Resolve endpoint URL
		parsedBase, err := url.Parse(c.cfg.URL)
		if err != nil {
			cancel()
			return err
		}
		parsedEp, err := url.Parse(ep)
		if err != nil {
			cancel()
			return err
		}
		resolved := parsedBase.ResolveReference(parsedEp).String()
		c.endpointURL = resolved
		return nil
	case <-time.After(10 * time.Second):
		cancel()
		return errors.New("timeout waiting for sse endpoint event")
	}
}

func (c *SSEClient) readSSEStream(rc io.ReadCloser, endpointCh chan<- string, errCh chan<- error) {
	defer rc.Close()
	scanner := bufio.NewScanner(rc)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	var currentEvent string
	var dataBuffer strings.Builder
	firstEndpointReceived := false

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, ":") {
			// Comment, ignore
			continue
		}
		if line == "" {
			// Dispatch event
			eventData := dataBuffer.String()
			dataBuffer.Reset()
			if currentEvent == "endpoint" && !firstEndpointReceived {
				firstEndpointReceived = true
				endpointCh <- strings.TrimSpace(eventData)
			} else if currentEvent == "message" || currentEvent == "" {
				c.dispatchMessage([]byte(eventData))
			}
			currentEvent = ""
			continue
		}

		if strings.HasPrefix(line, "event:") {
			currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			d := strings.TrimPrefix(line, "data:")
			if strings.HasPrefix(d, " ") {
				d = d[1:]
			}
			dataBuffer.WriteString(d)
		}
	}

	if !firstEndpointReceived {
		if err := scanner.Err(); err != nil {
			errCh <- err
		} else {
			errCh <- errors.New("sse stream closed before receiving endpoint event")
		}
	}

	// Clean up pending on stream close
	c.mu.Lock()
	c.endpointURL = ""
	c.initRes = nil
	for id, ch := range c.pending {
		delete(c.pending, id)
		ch <- protocol.NewErrorResponse(id, protocol.CodeInternalError, "sse connection closed", nil)
	}
	c.mu.Unlock()
}

func (c *SSEClient) dispatchMessage(data []byte) {
	var resp protocol.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}
	idNum, ok := toInt64(resp.ID)
	if !ok {
		return
	}

	c.mu.Lock()
	ch, exists := c.pending[idNum]
	delete(c.pending, idNum)
	c.mu.Unlock()

	if exists {
		ch <- &resp
	}
}

func (c *SSEClient) ensureInitialized(ctx context.Context) error {
	c.mu.RLock()
	if c.initRes != nil {
		c.mu.RUnlock()
		return nil
	}
	c.mu.RUnlock()

	c.initMu.Lock()
	defer c.initMu.Unlock()

	_, err := c.initializeLocked(ctx)
	return err
}

func (c *SSEClient) Initialize(ctx context.Context) (*protocol.InitializeResult, error) {
	c.initMu.Lock()
	defer c.initMu.Unlock()
	return c.initializeLocked(ctx)
}

func (c *SSEClient) initializeLocked(ctx context.Context) (*protocol.InitializeResult, error) {
	c.mu.RLock()
	if c.initRes != nil {
		res := c.initRes
		c.mu.RUnlock()
		return res, nil
	}
	c.mu.RUnlock()

	params := protocol.InitializeParams{
		ProtocolVersion: protocol.LatestProtocolVersion,
		ClientInfo: protocol.Implementation{
			Name:    "ccrouter",
			Version: "0.8.0",
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

func (c *SSEClient) sendRequest(ctx context.Context, method string, params any, cred *auth.Credential) (*protocol.Response, error) {
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

func (c *SSEClient) doSendRequest(ctx context.Context, method string, params any, cred *auth.Credential) (*protocol.Response, error) {
	if method != "initialize" && method != "notifications/initialized" {
		if err := c.ensureInitialized(ctx); err != nil {
			return nil, fmt.Errorf("ensure initialized: %w", err)
		}
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline && c.cfg.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(c.cfg.TimeoutSeconds)*time.Second)
		defer cancel()
	}

	if err := c.ensureConnection(ctx); err != nil {
		return nil, err
	}

	c.mu.RLock()
	epURL := c.endpointURL
	c.mu.RUnlock()

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

	ch := make(chan *protocol.Response, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, epURL, bytes.NewReader(bodyBytes))
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range c.cfg.Headers {
		httpReq.Header.Set(k, v)
	}
	if cred != nil && cred.HeaderName != "" && cred.HeaderValue != "" {
		httpReq.Header.Set(cred.HeaderName, cred.HeaderValue)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("post to mcp sse message endpoint failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("post mcp message returned status %d: %s", resp.StatusCode, string(body))
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	case res := <-ch:
		if res.Error != nil {
			return nil, res.Error
		}
		return res, nil
	}
}

func (c *SSEClient) sendNotification(ctx context.Context, method string, params any) error {
	if err := c.ensureConnection(ctx); err != nil {
		return err
	}

	c.mu.RLock()
	epURL := c.endpointURL
	c.mu.RUnlock()

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

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, epURL, bytes.NewReader(bodyBytes))
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

func (c *SSEClient) ListTools(ctx context.Context) ([]protocol.Tool, error) {
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

func (c *SSEClient) CallTool(ctx context.Context, name string, args map[string]any, cred *auth.Credential) (*protocol.CallToolResult, error) {
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

func (c *SSEClient) ListResources(ctx context.Context) ([]protocol.Resource, error) {
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

func (c *SSEClient) ReadResource(ctx context.Context, uri string, cred *auth.Credential) (*protocol.ReadResourceResult, error) {
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

func (c *SSEClient) ListPrompts(ctx context.Context) ([]protocol.Prompt, error) {
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

func (c *SSEClient) GetPrompt(ctx context.Context, name string, args map[string]string, cred *auth.Credential) (*protocol.GetPromptResult, error) {
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

func (c *SSEClient) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := c.sendRequest(ctx, "ping", nil, nil)
	return err
}

func (c *SSEClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.cancelStream != nil {
		c.cancelStream()
	}
	return nil
}
