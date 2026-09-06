package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"ccrouter/internal/config"
	"ccrouter/internal/mcp/auth"
	"ccrouter/internal/mcp/protocol"
)

// StdioClient manages communication with a local MCP server subprocess via stdin/stdout.
type StdioClient struct {
	cfg     config.McpProviderConfig
	mu      sync.Mutex
	initMu  sync.Mutex
	stdinMu sync.Mutex // separate lock for writing to stdin to avoid holding mu during I/O
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Scanner
	seq     int64
	pending map[int64]chan *protocol.Response
	closed  bool
	initRes *protocol.InitializeResult
}

// NewStdioClient creates a new StdioClient.
func NewStdioClient(cfg config.McpProviderConfig) *StdioClient {
	return &StdioClient{
		cfg:     cfg,
		pending: make(map[int64]chan *protocol.Response),
	}
}

func (c *StdioClient) Name() string      { return c.cfg.Name }
func (c *StdioClient) Transport() string { return "stdio" }

func (c *StdioClient) ensureProcessLocked() error {
	if c.cmd != nil && c.cmd.Process != nil {
		// Process is already started
		return nil
	}
	if c.closed {
		return errors.New("client is closed")
	}

	cmd := exec.Command(c.cfg.Command, c.cfg.Args...)
	if c.cfg.WorkingDir != "" {
		cmd.Dir = c.cfg.WorkingDir
	}

	// Environment variables
	env := os.Environ()
	for k, v := range c.cfg.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = env

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe error: %w", err)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdinPipe.Close()
		return fmt.Errorf("stdout pipe error: %w", err)
	}

	stderrPipe, err := cmd.StderrPipe()
	if err == nil {
		go func(pName string, r io.Reader) {
			s := bufio.NewScanner(r)
			for s.Scan() {
				log.Printf("[mcp-stdio:%s:stderr] %s", pName, s.Text())
			}
		}(c.cfg.Name, stderrPipe)
	}

	if err := cmd.Start(); err != nil {
		_ = stdinPipe.Close()
		_ = stdoutPipe.Close()
		return fmt.Errorf("start process %q failed: %w", c.cfg.Command, err)
	}

	c.cmd = cmd
	c.stdin = stdinPipe
	c.stdout = bufio.NewScanner(stdoutPipe)
	c.initRes = nil // reset initRes when starting a new process

	// Start response reader goroutine
	go c.readResponses(cmd, stdoutPipe)
	return nil
}

func (c *StdioClient) readResponses(cmd *exec.Cmd, stdout io.ReadCloser) {
	scanner := bufio.NewScanner(stdout)
	// Buffer size up to 10MB for large tool responses
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var resp protocol.Response
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}

		idNum, ok := toInt64(resp.ID)
		if !ok {
			continue
		}

		c.mu.Lock()
		ch, exists := c.pending[idNum]
		delete(c.pending, idNum)
		c.mu.Unlock()

		if exists {
			ch <- &resp
		}
	}

	_ = cmd.Wait()

	// Clean up pending on process exit and invalidate initRes
	c.mu.Lock()
	c.cmd = nil
	c.stdin = nil
	c.initRes = nil
	for id, ch := range c.pending {
		delete(c.pending, id)
		ch <- protocol.NewErrorResponse(id, protocol.CodeInternalError, "mcp server process exited unexpectedly", nil)
	}
	c.mu.Unlock()
}

func toInt64(v any) (int64, bool) {
	switch t := v.(type) {
	case int64:
		return t, true
	case int:
		return int64(t), true
	case float64:
		return int64(t), true
	}
	return 0, false
}

func (c *StdioClient) writeStdinWithTimeout(data []byte, timeout time.Duration) error {
	c.stdinMu.Lock()
	defer c.stdinMu.Unlock()

	c.mu.Lock()
	stdin := c.stdin
	cmd := c.cmd
	c.mu.Unlock()

	if stdin == nil || cmd == nil {
		return errors.New("stdin is not available")
	}

	done := make(chan error, 1)
	go func() {
		_, err := stdin.Write(data)
		done <- err
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		if cmd.Process != nil {
			log.Printf("[mcp-stdio:%s] stdin write timeout (%v), killing hung process (pid=%d)", c.cfg.Name, timeout, cmd.Process.Pid)
			_ = cmd.Process.Kill()
		}
		_ = stdin.Close()
		return fmt.Errorf("stdin write timed out after %v", timeout)
	}
}

func (c *StdioClient) ensureInitialized(ctx context.Context) error {
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

func (c *StdioClient) Initialize(ctx context.Context) (*protocol.InitializeResult, error) {
	c.initMu.Lock()
	defer c.initMu.Unlock()
	return c.initializeLocked(ctx)
}

func (c *StdioClient) initializeLocked(ctx context.Context) (*protocol.InitializeResult, error) {
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
			Version: "0.8.0",
		},
		Capabilities: protocol.ClientCapabilities{},
	}

	resp, err := c.doSendRequest(ctx, "initialize", params)
	if err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}

	resBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}
	var initRes protocol.InitializeResult
	if err := json.Unmarshal(resBytes, &initRes); err != nil {
		return nil, fmt.Errorf("unmarshal initialize result: %w", err)
	}

	// Send notifications/initialized
	_ = c.sendNotification("notifications/initialized", nil)

	c.mu.Lock()
	c.initRes = &initRes
	c.mu.Unlock()
	return &initRes, nil
}

// sendRequest sends a JSON-RPC request and awaits response or context cancellation.
// It transparently handles lazy initialization and retries once if the server reports not initialized.
func (c *StdioClient) sendRequest(ctx context.Context, method string, params any) (*protocol.Response, error) {
	resp, err := c.doSendRequest(ctx, method, params)
	if err != nil {
		var rpcErr *protocol.Error
		if errors.As(err, &rpcErr) && rpcErr.Code == protocol.CodeServerNotInitialized && method != "initialize" {
			c.mu.Lock()
			c.initRes = nil
			c.mu.Unlock()
			return c.doSendRequest(ctx, method, params)
		}
		return nil, err
	}
	return resp, nil
}

func (c *StdioClient) doSendRequest(ctx context.Context, method string, params any) (*protocol.Response, error) {
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

	c.mu.Lock()
	if err := c.ensureProcessLocked(); err != nil {
		c.mu.Unlock()
		return nil, err
	}

	id := atomic.AddInt64(&c.seq, 1)
	var rawParams json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			c.mu.Unlock()
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		rawParams = data
	}

	req := protocol.Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  rawParams,
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		c.mu.Unlock()
		return nil, err
	}
	reqBytes = append(reqBytes, '\n')

	ch := make(chan *protocol.Response, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	if writeErr := c.writeStdinWithTimeout(reqBytes, 5*time.Second); writeErr != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("write to stdin: %w", writeErr)
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp, nil
	}
}

// sendNotification sends a JSON-RPC notification (no ID, no response).
func (c *StdioClient) sendNotification(method string, params any) error {
	c.mu.Lock()
	if err := c.ensureProcessLocked(); err != nil {
		c.mu.Unlock()
		return err
	}

	var rawParams json.RawMessage
	if params != nil {
		data, _ := json.Marshal(params)
		rawParams = data
	}

	req := protocol.Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  rawParams,
	}
	reqBytes, err := json.Marshal(req)
	if err != nil {
		c.mu.Unlock()
		return err
	}
	reqBytes = append(reqBytes, '\n')
	c.mu.Unlock()

	return c.writeStdinWithTimeout(reqBytes, 5*time.Second)
}

func (c *StdioClient) ListTools(ctx context.Context) ([]protocol.Tool, error) {
	resp, err := c.sendRequest(ctx, "tools/list", map[string]any{})
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

func (c *StdioClient) CallTool(ctx context.Context, name string, args map[string]any, cred *auth.Credential) (*protocol.CallToolResult, error) {
	params := protocol.CallToolParams{
		Name:      name,
		Arguments: args,
	}
	resp, err := c.sendRequest(ctx, "tools/call", params)
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

func (c *StdioClient) ListResources(ctx context.Context) ([]protocol.Resource, error) {
	resp, err := c.sendRequest(ctx, "resources/list", map[string]any{})
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

func (c *StdioClient) ReadResource(ctx context.Context, uri string, cred *auth.Credential) (*protocol.ReadResourceResult, error) {
	params := protocol.ReadResourceParams{URI: uri}
	resp, err := c.sendRequest(ctx, "resources/read", params)
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

func (c *StdioClient) ListPrompts(ctx context.Context) ([]protocol.Prompt, error) {
	resp, err := c.sendRequest(ctx, "prompts/list", map[string]any{})
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

func (c *StdioClient) GetPrompt(ctx context.Context, name string, args map[string]string, cred *auth.Credential) (*protocol.GetPromptResult, error) {
	params := protocol.GetPromptParams{
		Name:      name,
		Arguments: args,
	}
	resp, err := c.sendRequest(ctx, "prompts/get", params)
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

func (c *StdioClient) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := c.sendRequest(ctx, "ping", nil)
	return err
}

func (c *StdioClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	return nil
}
