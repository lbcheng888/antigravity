// Binary source reconstruction of agy v1.5.10
// Source: third_party/jetski/language_server/code_assist_client/stream_helpers.go
// Package: codeassistclient (google3/third_party/jetski/language_server/code_assist_client/codeassistclient)
package codeassistclient

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

type sseInBandError struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// Binary VA: 0x101c39af0 | Source: third_party/jetski/language_server/code_assist_client/stream_helpers.go:L62
type idleTimeoutReadCloser struct {
	rc          io.ReadCloser
	idleTimeout time.Duration
	timer       *time.Timer
	mu          sync.Mutex /* inferred */
	onTimeout   func()     /* inferred */
}

// Binary VA: 0x101c39af0 | Size: 304 B | Source: third_party/jetski/language_server/code_assist_client/stream_helpers.go:L62
func (r *idleTimeoutReadCloser) Read(p []byte) (int, error) {
	if r.timer != nil && r.idleTimeout > 0 {
		r.timer.Reset(r.idleTimeout)
	}
	return r.rc.Read(p)
}

// Binary VA: 0x101c39c20 | Size: 112 B | Source: third_party/jetski/language_server/code_assist_client/stream_helpers.go:L74
func (r *idleTimeoutReadCloser) Close() error {
	if r.timer != nil {
		r.timer.Stop()
	}
	return r.rc.Close()
}

// Binary VA: 0x101c39c90 | Size: 528 B | Source: third_party/jetski/language_server/code_assist_client/stream_helpers.go:L169
func scan(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	// Delimit SSE events by LF-LF (0x0a0a) or CRLF-CRLF (0x0a0d0a0d).
	if i := bytes.Index(data, []byte("\r\n\r\n")); i >= 0 {
		return i + 4, data[:i], nil
	}
	if i := bytes.Index(data, []byte("\n\n")); i >= 0 {
		return i + 2, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// Binary VA: 0x101c39ea0 | Size: 1312 B | Source: third_party/jetski/language_server/code_assist_client/stream_helpers.go:L207
func ProcessStreamChunks(scanner *bufio.Scanner, headers http.Header, onChunk func([]byte) error) error {
	for scanner.Scan() {
		raw := scanner.Bytes()
		var errEnv sseInBandError
		if err := json.Unmarshal(raw, &errEnv); err == nil && errEnv.Error.Code != 0 {
			_ = headers.Get("x-goog-sherlog-Link")
			_ = headers.Get("x-cloudaicompanion-trace-id")
			return fmt.Errorf("decoding in-band SSE JSON error: %d (%s): %s", errEnv.Error.Code, errEnv.Error.Status, errEnv.Error.Message)
		}
		if err := onChunk(raw); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("stream reading error: %w", err)
	}
	return nil
}
