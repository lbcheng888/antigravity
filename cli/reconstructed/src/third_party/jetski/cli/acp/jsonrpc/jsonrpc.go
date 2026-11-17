// Reconstructed from agy v1.5.10 binary evidence (disassembly + Go type table).
// Binary VA: 0x102341710 | Size: 22 funcs (first func NewStream, 400 B) | Source: third_party/jetski/cli/acp/jsonrpc/jsonrpc.go:L55
//
// Package jsonrpc implements the JSON-RPC 2.0 transport used by `agy acp`.
// Framing auto-detects between newline-delimited JSON (ACP default) and
// LSP-style "Content-Length:" headers; once a header frame is seen, replies
// are written with headers too (Stream.hasHeaders).
package jsonrpc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json" // json.Compact (v1) is used in WriteMessage
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

var _ = errors.New // inferred: keep import (errors.errorString seen via fmt.Errorf fast path)

// Request is an incoming or outgoing JSON-RPC request/notification.
// go_type 0x10a55ad28, size 48.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`          // +0x00
	Method  string          `json:"method"`           // +0x10
	Params  *jsontext.Value `json:"params,omitempty"` // +0x20
	ID      *jsontext.Value `json:"id,omitempty"`     // +0x28
}

// Response is a JSON-RPC response. go_type 0x10a55ade8, size 48.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`          // +0x00
	Result  any             `json:"result,omitempty"` // +0x10
	Error   *Error          `json:"error,omitempty"`  // +0x20
	ID      *jsontext.Value `json:"id,omitempty"`     // +0x28
}

// Error is a JSON-RPC error object. go_type 0x10a45c5d8, size 40.
type Error struct {
	Code    int    `json:"code"`           // +0x00
	Message string `json:"message"`        // +0x08
	Data    any    `json:"data,omitempty"` // +0x18
}

// Envelope is a generic decoded frame. go_type 0x10a805bc8, size 112.
// Methods (value receiver): HasID, IsResponse.
type Envelope struct {
	JSONRPC string         `json:"jsonrpc"`          // +0x00
	ID      jsontext.Value `json:"id,omitempty"`     // +0x10
	Method  string         `json:"method,omitempty"` // +0x28
	Params  jsontext.Value `json:"params,omitempty"` // +0x38
	Result  jsontext.Value `json:"result,omitempty"` // +0x50
	Error   *Error         `json:"error,omitempty"`  // +0x68
}

// Stream does message framing over a reader/writer pair.
// go_type 0x10a55aea8, size 40.
type Stream struct {
	br         *bufio.Reader // +0x00
	w          io.Writer     // +0x08
	mu         sync.Mutex    // +0x18 (guards writes)
	hasHeaders atomic.Bool   // +0x20 (peer used Content-Length framing)
}

// rpcCall is an outstanding outbound call. go_type 0x10a1b1f48, size 8.
type rpcCall struct {
	done chan *Response // +0x00 (buffered, cap 1)
}

// Conn is a bidirectional JSON-RPC connection. go_type 0x10a8c0648, size 88.
type Conn struct {
	stream      *Stream             // +0x00
	mu          sync.Mutex          // +0x08 (guards seq)
	seq         uint64              // +0x10
	pending     map[string]*rpcCall // +0x18
	pendingMu   sync.Mutex          // +0x20 (guards pending/closed/closeReason)
	closed      bool                // +0x28
	closeReason string              // +0x30
	done        chan struct{}       // +0x40
	handler     func(*Request)      // +0x48
	onClose     func()              // +0x50
}

// NewStream wraps r/w for JSON-RPC framing.
// Binary VA: 0x102341710 | Size: 400 B | Source: third_party/jetski/cli/acp/jsonrpc/jsonrpc.go:L55
func NewStream(r io.Reader, w io.Writer) *Stream {
	return &Stream{ // L60
		br: bufio.NewReader(r), // inlined bufio.go:52-88 (default 4096 buffer)
		w:  w,
	}
}

// ReadMessage reads one frame. A line starting with '{' is a bare NDJSON
// message; otherwise header lines are parsed until a blank line and exactly
// Content-Length bytes are read.
// Binary VA: 0x1023418a0 | Size: 784 B | Source: third_party/jetski/cli/acp/jsonrpc/jsonrpc.go:L64
func (s *Stream) ReadMessage() ([]byte, error) {
	if s.br == nil { // L65
		return nil, io.EOF // L66 (global error var @0x10d3c9700; io.EOF inferred — also compared in Conn.Run)
	}
	for {
		line, err := s.br.ReadString('\n') // L73
		if err != nil {                    // L74
			return nil, err // L75
		}
		trimmed := strings.TrimSpace(line) // L77
		if trimmed == "" {                 // L78
			continue
		}
		if trimmed[0] == '{' { // L84 (cmp #0x7b)
			s.hasHeaders.Store(false) // L85
			return []byte(trimmed), nil // L86
		}

		// Header-framed message.
		s.hasHeaders.Store(true) // L89
		contentLength := 0       /* inferred */
		if v, ok := strings.CutPrefix(trimmed, "Content-Length:"); ok { // L91 (15-byte prefix compare inlined)
			contentLength, _ = strconv.Atoi(strings.TrimSpace(v)) // L92
		}
		for { // L95
			hline, err := s.br.ReadString('\n') // L96
			if err != nil {                     // L97
				return nil, err // L98
			}
			h := strings.TrimSpace(hline) // L100
			if h == "" {                  // L101
				break
			}
			if v, ok := strings.CutPrefix(h, "Content-Length:"); ok { // L104
				contentLength, _ = strconv.Atoi(strings.TrimSpace(v)) // L105
			}
		}
		if contentLength <= 0 { // L109
			return nil, fmt.Errorf("no Content-Length header found in HTTP-like header stream") // L110
		}
		buf := make([]byte, contentLength) // L113
		_, err = io.ReadFull(s.br, buf)    // L114 (io.ReadAtLeast inlined)
		return buf, err                    // L115
	}
}

// WriteMessage writes one frame, mirroring the framing the peer used.
// Binary VA: 0x102341bb0 | Size: 704 B | Source: third_party/jetski/cli/acp/jsonrpc/jsonrpc.go:L121
func (s *Stream) WriteMessage(data []byte) error {
	data = bytes.TrimSpace(data) // L122
	s.mu.Lock()                  // L123
	defer s.mu.Unlock()          // L124 (deferwrap1 @0x102343950)

	if s.hasHeaders.Load() { // L127
		_, err := fmt.Fprintf(s.w, "Content-Length: %d\r\n\r\n%s", len(data), data) // L128
		return err
	}
	if bytes.ContainsAny(data, "\r\n") { // L130 (bytes.IndexAny inlined)
		var buf bytes.Buffer                             // L131
		if err := json.Compact(&buf, data); err != nil { // L132
			return fmt.Errorf("compacting a multi-line message: %w", err) // L133
		}
		data = buf.Bytes() /* inferred */
	}
	_, err := fmt.Fprintf(s.w, "%s\n", data) // L137
	return err                                // L139
}

// NewConn creates a connection whose incoming requests/notifications are
// dispatched to handler (each on its own goroutine).
// Binary VA: 0x102341e70 | Size: 512 B | Source: third_party/jetski/cli/acp/jsonrpc/jsonrpc.go:L158
func NewConn(r io.Reader, w io.Writer, handler func(*Request)) *Conn {
	return &Conn{ // L159
		stream:  NewStream(r, w),             // inlined L55-60
		pending: make(map[string]*rpcCall),   // L161
		done:    make(chan struct{}),         // L162
		handler: handler,
	}
}

// SetOnClose registers a callback run once when the connection closes.
// Binary VA: 0x102342070 | Size: 96 B | Source: third_party/jetski/cli/acp/jsonrpc/jsonrpc.go:L169
func (c *Conn) SetOnClose(fn func()) {
	c.onClose = fn
}

// Done returns a channel closed when the connection is closed.
// Binary VA: 0x1023420d0 | Size: 112 B | Source: third_party/jetski/cli/acp/jsonrpc/jsonrpc.go:L174
func (c *Conn) Done() <-chan struct{} {
	if c == nil || c.done == nil { // L175
		ch := make(chan struct{}) // L176
		close(ch)                 // L177
		return ch                 // L178
	}
	return c.done // L180
}

// IsClosed reports whether the connection has been closed.
// Binary VA: 0x102342140 | Size: 272 B | Source: third_party/jetski/cli/acp/jsonrpc/jsonrpc.go:L184
func (c *Conn) IsClosed() bool {
	if c == nil { // L185
		return true
	}
	c.pendingMu.Lock()         // L188
	defer c.pendingMu.Unlock() // L189 (deferwrap1 @0x1023439c0)
	return c.closed            // L190
}

// Run reads frames until EOF/error. Responses are matched to pending calls by
// normalized ID; requests/notifications go to the handler in a goroutine.
// Binary VA: 0x102342250 | Size: 1232 B | Source: third_party/jetski/cli/acp/jsonrpc/jsonrpc.go:L194
func (c *Conn) Run() error {
	var wg sync.WaitGroup                     // L195
	defer wg.Wait()                           // L196 (deferwrap1 @0x102343b50)
	defer c.failPendingCalls("connection closed") // L197 (deferwrap2 @0x102343b00)

	for {
		data, err := c.stream.ReadMessage() // L199
		if err != nil {                     // L200
			if err == io.EOF { // L201 (runtime.ifaceeq against global @0x10d3c9700)
				return nil // L202
			}
			return err // L204
		}

		var env struct { // L207 (anonymous struct go_type 0x109d14010)
			JSONRPC string          `json:"jsonrpc"`
			Method  string          `json:"method"`
			Params  *jsontext.Value `json:"params,omitempty"`
			ID      *jsontext.Value `json:"id,omitempty"`
			Result  jsontext.Value  `json:"result,omitempty"`
			Error   *Error          `json:"error,omitempty"`
		}
		if err := jsonv2.Unmarshal(data, &env); err != nil { // L207-215
			continue // L216
		}

		if env.ID != nil && env.Method == "" { // L219: response to one of our calls
			id := NormalizeID(*env.ID) // L220
			c.pendingMu.Lock()         // L221
			if call, ok := c.pending[id]; ok { // L222
				delete(c.pending, id) // L223
				call.done <- &Response{ // L224
					ID:     env.ID,     // L225
					Result: env.Result, // L226 (convTslice -> jsontext.Value in interface)
					Error:  env.Error,  // L227
					JSONRPC: env.JSONRPC, /* inferred: L228 copies a further string field */
				}
			}
			c.pendingMu.Unlock() // L231
		}

		if env.Method == "" || c.handler == nil { // L232
			continue
		}
		req := &Request{ // L233
			JSONRPC: env.JSONRPC, // L234
			Method:  env.Method,  // L235
			Params:  env.Params,  // L236
			ID:      env.ID,      // L237
		}
		wg.Add(1) // L239
		// Binary VA: 0x102343a30 | Size: 144 B | Source: third_party/jetski/cli/acp/jsonrpc/jsonrpc.go:L240 (Run.func1)
		go func() {
			defer wg.Done() // L241 (Run.func1.deferwrap1 @0x102343ac0)
			c.handler(req)  // L242
		}() // L243
	}
}

// NormalizeID turns a raw JSON id (number or string) into a map key.
// Binary VA: 0x102342720 | Size: 112 B | Source: third_party/jetski/cli/acp/jsonrpc/jsonrpc.go:L249
func NormalizeID(raw jsontext.Value) string {
	return strings.Trim(strings.TrimSpace(string(raw)), `"`) // L250
}

// failPendingCalls marks the conn closed (first time only: records reason,
// runs onClose, closes done) and fails every outstanding call with -32603.
// Binary VA: 0x102342790 | Size: 1040 B | Source: third_party/jetski/cli/acp/jsonrpc/jsonrpc.go:L253
func (c *Conn) failPendingCalls(reason string) {
	if reason == "" { // L254
		reason = "connection closed" // L257
	}
	c.pendingMu.Lock()
	if !c.closed { // L258
		c.closed = true         // L259
		c.closeReason = reason  // L260
		if c.onClose != nil {   // L261
			c.onClose() // L262
		}
		if c.done != nil { // L264
			close(c.done) // L265
		}
	} else {
		reason = c.closeReason // L267
	}
	calls := make([]*rpcCall, 0, len(c.pending)) // L270
	for id, call := range c.pending {            // L271
		delete(c.pending, id)       // L272
		calls = append(calls, call) // L273
	}
	c.pendingMu.Unlock() // L275

	for _, call := range calls { // L277
		select {
		case call.done <- &Response{ // L279 (selectnbsend)
			JSONRPC: "2.0",                                  // L280
			Error:   &Error{Code: -32603, Message: reason}, // L281 (#-0x7f5b)
		}:
		default:
		}
	}
}

// writeMessage marshals msg with encoding/json/v2 and writes it.
// Binary VA: 0x102342ba0 | Size: 176 B | Source: third_party/jetski/cli/acp/jsonrpc/jsonrpc.go:L288
func (c *Conn) writeMessage(msg any) error {
	data, err := jsonv2.Marshal(msg) // L289
	if err != nil {
		return err
	}
	return c.stream.WriteMessage(data) // L293
}

// SendResponse sends a result or error for request id.
// Binary VA: 0x102342c50 | Size: 144 B | Source: third_party/jetski/cli/acp/jsonrpc/jsonrpc.go:L297
func (c *Conn) SendResponse(id *jsontext.Value, result any, rpcErr *Error) error {
	return c.writeMessage(Response{ // L298
		JSONRPC: "2.0", // L299
		ID:      id,
		Result:  result,
		Error:   rpcErr,
	})
}

// SendNotification sends a method call without an id.
// Binary VA: 0x102342ce0 | Size: 320 B | Source: third_party/jetski/cli/acp/jsonrpc/jsonrpc.go:L307
func (c *Conn) SendNotification(method string, params any) error {
	data, err := jsonv2.Marshal(params) // L308
	if err != nil {
		return err
	}
	raw := jsontext.Value(data) // L312
	return c.writeMessage(Request{ // L314
		JSONRPC: "2.0", // L315
		Method:  method,
		Params:  &raw,
	})
}

// Call performs an outbound request (agent -> client, e.g. session/request_permission,
// fs/read_text_file) and decodes the result into result (may be nil).
// IDs are decimal strings from an incrementing counter.
// Binary VA: 0x102342e20 | Size: 2720 B | Source: third_party/jetski/cli/acp/jsonrpc/jsonrpc.go:L322
func (c *Conn) Call(ctx context.Context, method string, params any, result any) error {
	if ctx == nil { // L323
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil { // L326
		return err // L327
	}
	c.mu.Lock()                         // L330
	c.seq++                             // L331
	id := strconv.FormatUint(c.seq, 10) // L332
	c.mu.Unlock()                       // L333

	idJSON, _ := jsonv2.Marshal(id) // L335
	idRaw := jsontext.Value(idJSON) // L336

	paramsJSON, err := jsonv2.Marshal(params) // L338
	if err != nil {                           // L339
		return err // L340
	}
	paramsRaw := jsontext.Value(paramsJSON) // L342

	call := &rpcCall{done: make(chan *Response, 1)} // L344
	c.pendingMu.Lock()                              // L345
	if c.closed {                                   // L346
		reason := c.closeReason // L347
		if reason == "" {       // L348
			reason = "connection closed" // L351
		}
		c.pendingMu.Unlock()
		return fmt.Errorf("JSON-RPC error: %s (code -32603)", reason) // L352
	}
	c.pending[id] = call // L354
	done := c.done       // L355
	c.pendingMu.Unlock() // L356

	if err := c.writeMessage(Request{ // L358
		JSONRPC: "2.0",       // L359
		Method:  method,      // L360
		Params:  &paramsRaw,  // L361
		ID:      &idRaw,      // L362
	}); err != nil { // L364
		c.pendingMu.Lock()    // L365
		delete(c.pending, id) // L366
		c.pendingMu.Unlock()  // L367
		return err            // L368
	}

	select { // L371
	case <-ctx.Done(): // L372-373
		c.pendingMu.Lock()    // L373
		delete(c.pending, id) // L374
		c.pendingMu.Unlock()  // L375
		return ctx.Err()      // L376
	case resp := <-call.done: // L377
		if resp.Error != nil { // L378
			return fmt.Errorf("JSON-RPC error: %s (code %d)", resp.Error.Message, resp.Error.Code) // L379
		}
		if result == nil || resp.Result == nil { // L381
			return nil // L394
		}
		if raw, ok := resp.Result.(jsontext.Value); ok { // L382
			if len(raw) > 0 { // L383
				return jsonv2.Unmarshal(raw, result) // L384
			}
			return nil // L386
		}
		b, err := jsonv2.Marshal(resp.Result) // L388
		if err != nil {                        // L389
			return err // L390
		}
		return jsonv2.Unmarshal(b, result) // L392
	case <-done: // L395
		c.pendingMu.Lock()      // L396
		delete(c.pending, id)   // L397
		reason := c.closeReason // L398
		c.pendingMu.Unlock()    // L399
		if reason == "" {       // L400
			reason = "connection closed"
		}
		return fmt.Errorf("JSON-RPC error: %s (code -32603)", reason) // L403
	}
}

// IsResponse reports whether the envelope has no method.
// Binary VA: 0x1023438c0 | Size: 48 B | Source: third_party/jetski/cli/acp/jsonrpc/jsonrpc.go:L417
func (e Envelope) IsResponse() bool { return e.Method == "" }

// HasID reports whether a non-null id is present.
// Binary VA: 0x1023438f0 | Size: 96 B | Source: third_party/jetski/cli/acp/jsonrpc/jsonrpc.go:L419
func (e Envelope) HasID() bool { return len(e.ID) > 0 && string(e.ID) != "null" }
