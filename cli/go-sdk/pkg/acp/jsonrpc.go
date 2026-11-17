package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Request represents an incoming or outgoing JSON-RPC 2.0 request or notification.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
}

// Response represents a JSON-RPC 2.0 response frame.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  any             `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
}

// Error is a JSON-RPC 2.0 error object.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("jsonrpc error (%d): %s", e.Code, e.Message)
}

const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// Envelope is a raw decoded JSON-RPC frame capable of holding either a Request or Response.
type Envelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

func (e *Envelope) HasID() bool {
	trimmed := bytes.TrimSpace(e.ID)
	return len(trimmed) > 0 && string(trimmed) != "null"
}

func (e *Envelope) IsResponse() bool {
	return e.Method == "" && e.HasID()
}

// Stream provides auto-detecting NDJSON / Content-Length JSON-RPC 2.0 framing.
type Stream struct {
	br         *bufio.Reader
	w          io.Writer
	mu         sync.Mutex
	hasHeaders atomic.Bool
}

// NewStream wraps r and w for bidirectional JSON-RPC message framing.
func NewStream(r io.Reader, w io.Writer) *Stream {
	return &Stream{
		br: bufio.NewReaderSize(r, 64*1024),
		w:  w,
	}
}

// ReadMessage reads one raw JSON payload, auto-detecting NDJSON vs Content-Length headers.
func (s *Stream) ReadMessage() ([]byte, error) {
	for {
		line, err := s.br.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if errors.Is(err, io.EOF) {
				return nil, io.EOF
			}
			continue
		}

		// Bare JSON line (NDJSON mode)
		if strings.HasPrefix(trimmed, "{") {
			return []byte(trimmed), nil
		}

		// Header-framed LSP / Content-Length mode
		contentLength := -1
		if k, v, ok := strings.Cut(trimmed, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "content-length") {
			n, convErr := strconv.Atoi(strings.TrimSpace(v))
			if convErr != nil || n < 0 {
				return nil, fmt.Errorf("invalid Content-Length %q", v)
			}
			contentLength = n
			s.hasHeaders.Store(true)
		}

		// Drain remaining headers up to blank line (\r\n\r\n or \n\n)
		for {
			hdrLine, hdrErr := s.br.ReadString('\n')
			if hdrErr != nil {
				return nil, hdrErr
			}
			hdrTrim := strings.TrimSpace(hdrLine)
			if hdrTrim == "" {
				break
			}
			if k, v, ok := strings.Cut(hdrTrim, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "content-length") {
				n, convErr := strconv.Atoi(strings.TrimSpace(v))
				if convErr != nil || n < 0 {
					return nil, fmt.Errorf("invalid Content-Length %q", v)
				}
				contentLength = n
				s.hasHeaders.Store(true)
			}
		}

		if contentLength < 0 {
			return nil, errors.New("missing Content-Length header")
		}
		buf := make([]byte, contentLength)
		if _, readErr := io.ReadFull(s.br, buf); readErr != nil {
			return nil, readErr
		}
		return buf, nil
	}
}

// WriteMessage writes one JSON-RPC message using Content-Length framing if peer used headers, else NDJSON.
func (s *Stream) WriteMessage(msg any) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hasHeaders.Load() {
		frame := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(raw), raw)
		_, err = io.WriteString(s.w, frame)
		return err
	}
	raw = append(raw, '\n')
	_, err = s.w.Write(raw)
	return err
}

// SetHeaderFraming explicitly enables or disables Content-Length header output.
func (s *Stream) SetHeaderFraming(enabled bool) {
	s.hasHeaders.Store(enabled)
}

type pendingCall struct {
	done chan *Envelope
}

// Conn manages a bidirectional JSON-RPC 2.0 stream with concurrent outbound calls and inbound requests.
type Conn struct {
	stream    *Stream
	seq       atomic.Uint64
	pendingMu sync.Mutex
	pending   map[string]*pendingCall
	handler   func(context.Context, *Request)
}

// NewConn creates a Conn over stream dispatching inbound requests/notifications to handler.
func NewConn(stream *Stream, handler func(context.Context, *Request)) *Conn {
	return &Conn{
		stream:  stream,
		pending: make(map[string]*pendingCall),
		handler: handler,
	}
}

// Serve runs the inbound read loop until EOF or context cancellation.
func (c *Conn) Serve(ctx context.Context) error {
	var wg sync.WaitGroup
	defer func() {
		wg.Wait()
		c.failAllPending()
	}()

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw, err := c.stream.ReadMessage()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		var env Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			continue
		}
		if env.IsResponse() {
			key := normalizeID(env.ID)
			c.pendingMu.Lock()
			pc := c.pending[key]
			delete(c.pending, key)
			c.pendingMu.Unlock()
			if pc != nil {
				pc.done <- &env
			}
			continue
		}

		req := &Request{
			JSONRPC: env.JSONRPC,
			Method:  env.Method,
			Params:  env.Params,
			ID:      env.ID,
		}
		if c.handler != nil {
			wg.Add(1)
			go func(r *Request) {
				defer wg.Done()
				c.handler(ctx, r)
			}(req)
		}
	}
}

func (c *Conn) failAllPending() {
	c.pendingMu.Lock()
	for k, pc := range c.pending {
		delete(c.pending, k)
		close(pc.done)
	}
	c.pendingMu.Unlock()
}

// Reply sends a JSON-RPC Response for a received Request ID.
func (c *Conn) Reply(id json.RawMessage, result any, rpcErr *Error) error {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
	}
	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		resp.Result = result
	}
	return c.stream.WriteMessage(resp)
}

// Notify sends a one-way JSON-RPC notification without an ID.
func (c *Conn) Notify(method string, params any) error {
	var rawParams json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return err
		}
		rawParams = b
	}
	return c.stream.WriteMessage(Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  rawParams,
	})
}

// Call sends an outbound request and awaits its response, unmarshaling Result into out.
func (c *Conn) Call(ctx context.Context, method string, params any, out any) error {
	idNum := c.seq.Add(1)
	idRaw := json.RawMessage(strconv.FormatUint(idNum, 10))
	key := string(idRaw)

	pc := &pendingCall{done: make(chan *Envelope, 1)}
	c.pendingMu.Lock()
	c.pending[key] = pc
	c.pendingMu.Unlock()

	var rawParams json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			c.pendingMu.Lock()
			delete(c.pending, key)
			c.pendingMu.Unlock()
			return err
		}
		rawParams = b
	}

	if err := c.stream.WriteMessage(Request{
		JSONRPC: "2.0",
		ID:      idRaw,
		Method:  method,
		Params:  rawParams,
	}); err != nil {
		c.pendingMu.Lock()
		delete(c.pending, key)
		c.pendingMu.Unlock()
		return err
	}

	select {
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, key)
		c.pendingMu.Unlock()
		return ctx.Err()
	case env, ok := <-pc.done:
		if !ok || env == nil {
			return errors.New("jsonrpc connection closed before response")
		}
		if env.Error != nil {
			return env.Error
		}
		if out != nil && len(env.Result) > 0 {
			return json.Unmarshal(env.Result, out)
		}
		return nil
	}
}

func normalizeID(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(bytes.TrimSpace(raw))
}
