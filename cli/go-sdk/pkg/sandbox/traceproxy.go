package sandbox

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"antigravity.dev/agy-sdk/pkg/auth"
)

// TraceEntry is one JSONL record written by TraceProxy for each proxied HTTP request.
type TraceEntry struct {
	Timestamp       string            `json:"timestamp"`
	Method          string            `json:"method"`
	Path            string            `json:"path"`
	Status          int               `json:"status"`
	DurationMs      int64             `json:"duration_ms"`
	RedactedHeaders map[string]string `json:"redacted_headers,omitempty"`
	RequestBytes    int64             `json:"request_bytes"`
	ResponseBytes   int64             `json:"response_bytes"`
	Error           string            `json:"error,omitempty"`
}

// TraceProxy forwards HTTP/SSE requests to an upstream URL while recording JSONL metadata.
type TraceProxy struct {
	upstream *url.URL
	client   *http.Client
	mu       sync.Mutex
	out      io.Writer
	closer   io.Closer
}

// NewTraceProxy creates a TraceProxy forwarding to upstream and writing JSONL entries to traceOut.
func NewTraceProxy(upstream *url.URL, traceOut io.Writer) (*TraceProxy, error) {
	if upstream == nil || upstream.Scheme == "" || upstream.Host == "" {
		return nil, fmt.Errorf("sandbox: invalid upstream URL")
	}
	cp := *upstream
	return &TraceProxy{
		upstream: &cp,
		client:   &http.Client{},
		out:      traceOut,
	}, nil
}

// NewTraceProxyFile creates a TraceProxy that appends JSONL entries to tracePath.
func NewTraceProxyFile(upstream *url.URL, tracePath string) (*TraceProxy, error) {
	if strings.TrimSpace(tracePath) == "" {
		return nil, fmt.Errorf("sandbox: trace path must not be empty")
	}
	if err := os.MkdirAll(filepath.Dir(tracePath), 0o755); err != nil {
		return nil, fmt.Errorf("sandbox: create trace dir: %w", err)
	}
	f, err := os.OpenFile(tracePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("sandbox: open trace file: %w", err)
	}
	p, err := NewTraceProxy(upstream, f)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	p.closer = f
	return p, nil
}

// Close closes the underlying trace file if opened by NewTraceProxyFile.
func (p *TraceProxy) Close() error {
	if p == nil || p.closer == nil {
		return nil
	}
	return p.closer.Close()
}

// ServeHTTP forwards the request to upstream, streaming SSE responses without buffering,
// and logs a redacted TraceEntry.
func (p *TraceProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now().UTC()
	entry := TraceEntry{
		Timestamp:       start.Format(time.RFC3339Nano),
		Method:          r.Method,
		Path:            r.URL.RequestURI(),
		RedactedHeaders: redactHeaders(r.Header),
	}
	defer func() {
		entry.DurationMs = time.Since(start).Milliseconds()
		p.writeEntry(entry)
	}()

	targetURL := p.buildTargetURL(r.URL)
	reqCounter := &countingReadCloser{rc: r.Body}
	var body io.Reader
	if r.Body != nil {
		body = reqCounter
	}

	upReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL.String(), body)
	if err != nil {
		entry.Status = http.StatusBadRequest
		entry.Error = err.Error()
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	copyHeaders(upReq.Header, r.Header)
	upReq.Host = p.upstream.Host
	if r.ContentLength >= 0 {
		upReq.ContentLength = r.ContentLength
	}

	resp, err := p.client.Do(upReq)
	entry.RequestBytes = reqCounter.n
	if err != nil {
		entry.Status = http.StatusBadGateway
		entry.Error = err.Error()
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	entry.Status = resp.StatusCode
	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)

	flusher, canFlush := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			written, writeErr := w.Write(buf[:n])
			entry.ResponseBytes += int64(written)
			if canFlush {
				flusher.Flush()
			}
			if writeErr != nil {
				entry.Error = writeErr.Error()
				return
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				entry.Error = readErr.Error()
			}
			return
		}
	}
}

func (p *TraceProxy) buildTargetURL(reqURL *url.URL) *url.URL {
	out := *p.upstream
	basePath := strings.TrimSuffix(p.upstream.Path, "/")
	reqPath := reqURL.Path
	if !strings.HasPrefix(reqPath, "/") && reqPath != "" {
		reqPath = "/" + reqPath
	}
	out.Path = basePath + reqPath
	out.RawPath = ""
	out.RawQuery = reqURL.RawQuery
	return &out
}

func (p *TraceProxy) writeEntry(entry TraceEntry) {
	if p == nil || p.out == nil {
		return
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_, _ = p.out.Write(append(line, '\n'))
}

func redactHeaders(h http.Header) map[string]string {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string]string, len(h))
	for k, vals := range h {
		joined := strings.Join(vals, ", ")
		if isSensitiveHeader(k) {
			out[k] = auth.MaskSecret(joined)
		} else {
			out[k] = joined
		}
	}
	return out
}

func isSensitiveHeader(name string) bool {
	switch strings.ToLower(name) {
	case "authorization", "x-goog-api-key", "x-api-key", "proxy-authorization", "cookie", "set-cookie":
		return true
	default:
		return false
	}
}

func copyHeaders(dst, src http.Header) {
	for k, vals := range src {
		if isHopByHopHeader(k) {
			continue
		}
		for _, v := range vals {
			dst.Add(k, v)
		}
	}
}

func isHopByHopHeader(name string) bool {
	switch strings.ToLower(name) {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailers", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}

type countingReadCloser struct {
	rc io.ReadCloser
	n  int64
}

func (c *countingReadCloser) Read(p []byte) (int, error) {
	n, err := c.rc.Read(p)
	c.n += int64(n)
	return n, err
}

func (c *countingReadCloser) Close() error {
	return c.rc.Close()
}
