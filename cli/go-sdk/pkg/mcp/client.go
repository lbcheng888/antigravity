package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
)

const mcpProtocolVersion = "2024-11-05"

// MCPToolInfo describes a tool advertised by an MCP server.
type MCPToolInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("mcp rpc error %d: %s", e.Code, e.Message)
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type listToolsResult struct {
	Tools      []MCPToolInfo `json:"tools"`
	NextCursor string        `json:"nextCursor,omitempty"`
}

type callToolResult struct {
	Content []contentBlock `json:"content"`
	IsError bool           `json:"isError"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// Client communicates with an MCP server over stdio NDJSON or Streamable HTTP JSON-RPC 2.0.
type Client struct {
	transport string // "stdio" | "http"
	nextID    atomic.Int64

	// stdio transport state
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	writeMu   sync.Mutex
	pendingMu sync.Mutex
	pending   map[int64]chan rpcResponse
	readDone  chan struct{}
	readErr   error

	// http transport state
	url        string
	headers    map[string]string
	httpClient *http.Client
	sessionMu  sync.RWMutex
	sessionID  string

	closeOnce sync.Once
	closed    atomic.Bool
}

// NewStdioClient spawns an MCP server subprocess and connects over NDJSON stdin/stdout.
func NewStdioClient(command string, args []string, env map[string]string) (*Client, error) {
	if command == "" {
		return nil, errors.New("mcp: stdio command is required")
	}

	cmd := exec.Command(command, args...)
	if len(env) > 0 {
		merged := os.Environ()
		for k, v := range env {
			merged = append(merged, k+"="+v)
		}
		cmd.Env = merged
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("mcp: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("mcp: start %q: %w", command, err)
	}

	c := &Client{
		transport: "stdio",
		cmd:       cmd,
		stdin:     stdin,
		stdout:    stdout,
		pending:   make(map[int64]chan rpcResponse),
		readDone:  make(chan struct{}),
	}
	go c.readStdioLoop()
	return c, nil
}

// NewHTTPClient creates a Streamable HTTP MCP client for the given endpoint URL and headers.
func NewHTTPClient(url string, headers map[string]string) (*Client, error) {
	if url == "" {
		return nil, errors.New("mcp: http url is required")
	}
	hdrCopy := make(map[string]string, len(headers))
	for k, v := range headers {
		hdrCopy[k] = v
	}
	return &Client{
		transport:  "http",
		url:        url,
		headers:    hdrCopy,
		httpClient: &http.Client{},
	}, nil
}

// Initialize performs the MCP initialize handshake and sends notifications/initialized.
func (c *Client) Initialize(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "antigravity-cli",
			"version": "1.0.0",
		},
	}
	if _, err := c.call(ctx, "initialize", params); err != nil {
		return fmt.Errorf("mcp: initialize: %w", err)
	}
	if err := c.notify(ctx, "notifications/initialized", map[string]any{}); err != nil {
		return fmt.Errorf("mcp: notifications/initialized: %w", err)
	}
	return nil
}

// ListTools fetches all tools exposed by the MCP server, following nextCursor pagination if present.
func (c *Client) ListTools(ctx context.Context) ([]MCPToolInfo, error) {
	var all []MCPToolInfo
	var cursor string

	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := c.call(ctx, "tools/list", params)
		if err != nil {
			return nil, fmt.Errorf("mcp: tools/list: %w", err)
		}
		var res listToolsResult
		if err := json.Unmarshal(raw, &res); err != nil {
			return nil, fmt.Errorf("mcp: decode tools/list: %w", err)
		}
		all = append(all, res.Tools...)
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	return all, nil
}

// CallTool invokes a named tool on the MCP server and returns its text output, isError flag, and any transport/RPC error.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (string, bool, error) {
	if name == "" {
		return "", false, errors.New("mcp: tool name is required")
	}
	if args == nil {
		args = map[string]any{}
	}
	params := map[string]any{
		"name":      name,
		"arguments": args,
	}
	raw, err := c.call(ctx, "tools/call", params)
	if err != nil {
		return "", false, fmt.Errorf("mcp: tools/call %q: %w", name, err)
	}

	var res callToolResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", false, fmt.Errorf("mcp: decode tools/call %q: %w", name, err)
	}

	var parts []string
	for _, block := range res.Content {
		if block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n"), res.IsError, nil
}

// Close shuts down the client transport and releases any subprocess or HTTP resources.
func (c *Client) Close() error {
	var closeErr error
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		switch c.transport {
		case "stdio":
			if c.stdin != nil {
				_ = c.stdin.Close()
			}
			if c.cmd != nil && c.cmd.Process != nil {
				done := make(chan error, 1)
				go func() {
					done <- c.cmd.Wait()
				}()
				select {
				case <-c.readDone:
					closeErr = <-done
				default:
					_ = c.cmd.Process.Kill()
					closeErr = <-done
				}
			}
			if c.readDone != nil {
				<-c.readDone
			}
		case "http":
			if c.httpClient != nil {
				c.httpClient.CloseIdleConnections()
			}
		}
	})
	return closeErr
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if c.closed.Load() {
		return nil, errors.New("mcp: client is closed")
	}
	id := c.nextID.Add(1)
	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	switch c.transport {
	case "stdio":
		return c.callStdio(ctx, req)
	case "http":
		return c.callHTTP(ctx, req)
	default:
		return nil, fmt.Errorf("mcp: unknown transport %q", c.transport)
	}
}

func (c *Client) notify(ctx context.Context, method string, params any) error {
	if c.closed.Load() {
		return errors.New("mcp: client is closed")
	}
	req := rpcRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	switch c.transport {
	case "stdio":
		return c.writeStdio(req)
	case "http":
		return c.notifyHTTP(ctx, req)
	default:
		return fmt.Errorf("mcp: unknown transport %q", c.transport)
	}
}

func (c *Client) writeStdio(req rpcRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closed.Load() {
		return errors.New("mcp: client is closed")
	}
	_, err = c.stdin.Write(data)
	return err
}

func (c *Client) callStdio(ctx context.Context, req rpcRequest) (json.RawMessage, error) {
	ch := make(chan rpcResponse, 1)
	c.pendingMu.Lock()
	if c.readErr != nil {
		err := c.readErr
		c.pendingMu.Unlock()
		return nil, err
	}
	c.pending[req.ID] = ch
	c.pendingMu.Unlock()

	cleanup := func() {
		c.pendingMu.Lock()
		delete(c.pending, req.ID)
		c.pendingMu.Unlock()
	}

	if err := c.writeStdio(req); err != nil {
		cleanup()
		return nil, err
	}

	select {
	case <-ctx.Done():
		cleanup()
		return nil, ctx.Err()
	case <-c.readDone:
		cleanup()
		if c.readErr != nil {
			return nil, c.readErr
		}
		return nil, io.EOF
	case resp, ok := <-ch:
		if !ok {
			if c.readErr != nil {
				return nil, c.readErr
			}
			return nil, io.EOF
		}
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

func (c *Client) readStdioLoop() {
	defer close(c.readDone)
	scanner := bufio.NewScanner(c.stdout)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var resp rpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}
		if len(resp.ID) == 0 || bytes.Equal(resp.ID, []byte("null")) {
			continue
		}
		var id int64
		if err := json.Unmarshal(resp.ID, &id); err != nil {
			continue
		}

		c.pendingMu.Lock()
		ch, ok := c.pending[id]
		if ok {
			delete(c.pending, id)
		}
		c.pendingMu.Unlock()

		if ok {
			ch <- resp
			close(ch)
		}
	}

	err := scanner.Err()
	if err == nil {
		err = io.EOF
	}
	c.pendingMu.Lock()
	c.readErr = err
	for id, ch := range c.pending {
		delete(c.pending, id)
		close(ch)
	}
	c.pendingMu.Unlock()
}

func (c *Client) doHTTPRequest(ctx context.Context, req rpcRequest) (*http.Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range c.headers {
		httpReq.Header.Set(k, v)
	}

	c.sessionMu.RLock()
	sid := c.sessionID
	c.sessionMu.RUnlock()
	if sid != "" {
		httpReq.Header.Set("Mcp-Session-Id", sid)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if newSID := resp.Header.Get("Mcp-Session-Id"); newSID != "" {
		c.sessionMu.Lock()
		c.sessionID = newSID
		c.sessionMu.Unlock()
	}
	return resp, nil
}

func (c *Client) notifyHTTP(ctx context.Context, req rpcRequest) error {
	resp, err := c.doHTTPRequest(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("mcp: http notify status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) callHTTP(ctx context.Context, req rpcRequest) (json.RawMessage, error) {
	resp, err := c.doHTTPRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("mcp: read http response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("mcp: http status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	ct := resp.Header.Get("Content-Type")
	var rpcResp rpcResponse
	if strings.HasPrefix(strings.ToLower(ct), "text/event-stream") {
		parsed, err := parseSSEResponse(data, req.ID)
		if err != nil {
			return nil, err
		}
		rpcResp = parsed
	} else {
		if err := json.Unmarshal(data, &rpcResp); err != nil {
			return nil, fmt.Errorf("mcp: decode http json: %w", err)
		}
	}

	if rpcResp.Error != nil {
		return nil, rpcResp.Error
	}
	return rpcResp.Result, nil
}

func parseSSEResponse(data []byte, targetID int64) (rpcResponse, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	var dataLines []string
	var lastResp *rpcResponse

	flush := func() (*rpcResponse, bool) {
		if len(dataLines) == 0 {
			return nil, false
		}
		payload := strings.Join(dataLines, "\n")
		dataLines = nil
		var r rpcResponse
		if err := json.Unmarshal([]byte(payload), &r); err != nil {
			return nil, false
		}
		lastResp = &r
		var id int64
		if len(r.ID) > 0 && json.Unmarshal(r.ID, &id) == nil && id == targetID {
			return &r, true
		}
		return nil, false
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if matched, ok := flush(); ok {
				return *matched, nil
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			val := strings.TrimPrefix(line, "data:")
			val = strings.TrimPrefix(val, " ")
			dataLines = append(dataLines, val)
		}
	}
	if matched, ok := flush(); ok {
		return *matched, nil
	}
	if lastResp != nil {
		return *lastResp, nil
	}
	return rpcResponse{}, errors.New("mcp: no json-rpc response found in SSE stream")
}
