package aicode

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
	"time"
)

const (
	// DefaultStreamBufferSize configures the maximum single SSE event payload (10 MiB).
	DefaultStreamBufferSize = 10 * 1024 * 1024
	// DefaultStreamIdleTimeout bounds waiting between consecutive bytes on an open stream.
	DefaultStreamIdleTimeout = 2 * time.Minute
)

// APIError represents an HTTP, CloudCode, or in-band SSE error with diagnostic headers.
type APIError struct {
	StatusCode  int
	Code        int
	Status      string
	Message     string
	SherlogLink string
	TraceID     string
	Body        string
}

func (e *APIError) Error() string {
	var parts []string
	if e.StatusCode > 0 {
		parts = append(parts, fmt.Sprintf("http %d", e.StatusCode))
	}
	if e.Code > 0 && e.Code != e.StatusCode {
		parts = append(parts, fmt.Sprintf("code %d", e.Code))
	}
	if e.Status != "" {
		parts = append(parts, e.Status)
	}
	msg := e.Message
	if msg == "" {
		msg = e.Body
	}
	prefix := "aicode error"
	if len(parts) > 0 {
		prefix = fmt.Sprintf("aicode error (%s)", strings.Join(parts, ", "))
	}
	if e.TraceID != "" {
		return fmt.Sprintf("%s [trace=%s]: %s", prefix, e.TraceID, msg)
	}
	return fmt.Sprintf("%s: %s", prefix, msg)
}

// IdleTimeoutReadCloser aborts blocking reads if no byte arrives within IdleTimeout.
type IdleTimeoutReadCloser struct {
	rc          io.ReadCloser
	idleTimeout time.Duration
	cancelOnce  sync.Once
	timer       *time.Timer
}

// NewIdleTimeoutReadCloser wraps rc with an inactivity timer that closes rc on timeout.
func NewIdleTimeoutReadCloser(rc io.ReadCloser, timeout time.Duration) *IdleTimeoutReadCloser {
	if timeout <= 0 {
		timeout = DefaultStreamIdleTimeout
	}
	w := &IdleTimeoutReadCloser{
		rc:          rc,
		idleTimeout: timeout,
	}
	w.timer = time.AfterFunc(timeout, func() {
		w.Close()
	})
	return w
}

func (w *IdleTimeoutReadCloser) Read(p []byte) (int, error) {
	if w.timer != nil {
		w.timer.Reset(w.idleTimeout)
	}
	return w.rc.Read(p)
}

func (w *IdleTimeoutReadCloser) Close() error {
	var err error
	w.cancelOnce.Do(func() {
		if w.timer != nil {
			w.timer.Stop()
		}
		err = w.rc.Close()
	})
	return err
}

// scanSSEEvent splits an SSE stream on "\r\n\r\n" or "\n\n", matching codeassistclient.scan.
func scanSSEEvent(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	idxLF := bytes.Index(data, []byte("\n\n"))
	idxCRLF := bytes.Index(data, []byte("\r\n\r\n"))

	switch {
	case idxCRLF >= 0 && (idxLF < 0 || idxCRLF < idxLF):
		return idxCRLF + 4, data[:idxCRLF], nil
	case idxLF >= 0:
		return idxLF + 2, data[:idxLF], nil
	case atEOF:
		return len(data), data, nil
	default:
		return 0, nil, nil
	}
}

// extractSSEData gathers concatenated "data:" payloads from one raw SSE event block.
func extractSSEData(event []byte) string {
	var lines []string
	for _, rawLine := range strings.Split(string(event), "\n") {
		line := strings.TrimSuffix(rawLine, "\r")
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			val := strings.TrimPrefix(line, "data:")
			val = strings.TrimPrefix(val, " ")
			lines = append(lines, val)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// StreamFormat selects how SSE JSON payloads are decoded.
type StreamFormat int

const (
	StreamFormatCloudCode StreamFormat = iota
	StreamFormatGeminiV1Beta
	StreamFormatOpenAIGateway
)

// ProcessSSEStream reads SSE frames from resp.Body, enforces 10 MiB max token size and idle timeout,
// checks for in-band JSON errors, and invokes onChunk for each non-empty payload.
func ProcessSSEStream(ctx context.Context, resp *http.Response, format StreamFormat, maxBuf int, idleTimeout time.Duration, onChunk func(*SSEChunk) error) error {
	if resp == nil || resp.Body == nil {
		return errors.New("nil HTTP response body")
	}
	if maxBuf <= 0 {
		maxBuf = DefaultStreamBufferSize
	}

	wrapped := NewIdleTimeoutReadCloser(resp.Body, idleTimeout)
	defer wrapped.Close()

	// Ensure context cancellation unblocks any in-flight Read.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = wrapped.Close()
		case <-done:
		}
	}()

	sherlogLink := resp.Header.Get("x-goog-sherlog-Link")
	backendTrace := resp.Header.Get("x-cloudaicompanion-trace-id")

	scanner := bufio.NewScanner(wrapped)
	scanner.Buffer(make([]byte, 64*1024), maxBuf)
	scanner.Split(scanSSEEvent)

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		payload := extractSSEData(scanner.Bytes())
		if payload == "" || payload == "[DONE]" {
			continue
		}

		chunk, err := decodeSSEPayload([]byte(payload), format, sherlogLink, backendTrace)
		if err != nil {
			return err
		}
		if chunk != nil && onChunk != nil {
			if err := onChunk(chunk); err != nil {
				return err
			}
		}
	}

	if err := scanner.Err(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("read sse stream: %w", err)
	}
	return ctx.Err()
}

func decodeSSEPayload(raw []byte, format StreamFormat, sherlogLink, backendTrace string) (*SSEChunk, error) {
	// Check for in-band error object regardless of endpoint variant.
	var errProbe struct {
		Error *APIErrorBody `json:"error"`
	}
	if err := json.Unmarshal(raw, &errProbe); err == nil && errProbe.Error != nil && (errProbe.Error.Code != 0 || errProbe.Error.Message != "") {
		return nil, &APIError{
			StatusCode:  errProbe.Error.Code,
			Code:        errProbe.Error.Code,
			Status:      errProbe.Error.Status,
			Message:     errProbe.Error.Message,
			SherlogLink: sherlogLink,
			TraceID:     backendTrace,
			Body:        string(raw),
		}
	}

	chunk := &SSEChunk{
		RawData:      raw,
		SherlogLink:  sherlogLink,
		BackendTrace: backendTrace,
	}

	switch format {
	case StreamFormatCloudCode:
		var env GenerateContentResponse
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil, fmt.Errorf("decode cloudcode chunk: %w", err)
		}
		chunk.Response = env.Response
		chunk.TraceID = env.TraceID
		if chunk.TraceID == "" {
			chunk.TraceID = backendTrace
		}
		return chunk, nil

	case StreamFormatGeminiV1Beta:
		var inner InnerGenerateContentResponse
		if err := json.Unmarshal(raw, &inner); err != nil {
			return nil, fmt.Errorf("decode gemini chunk: %w", err)
		}
		chunk.Response = &inner
		chunk.TraceID = backendTrace
		return chunk, nil

	case StreamFormatOpenAIGateway:
		inner, err := decodeOpenAIChatChunk(raw)
		if err != nil {
			return nil, err
		}
		chunk.Response = inner
		chunk.TraceID = backendTrace
		return chunk, nil

	default:
		return nil, fmt.Errorf("unsupported stream format %d", format)
	}
}

// openAIChatChunk represents the subset of OpenAI /chat/completions SSE fields normalized into Gemini types.
type openAIChatChunk struct {
	Choices []struct {
		Index        int    `json:"index"`
		FinishReason string `json:"finish_reason"`
		Delta        struct {
			Role             string `json:"role"`
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func decodeOpenAIChatChunk(raw []byte) (*InnerGenerateContentResponse, error) {
	var msg openAIChatChunk
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, fmt.Errorf("decode gateway chunk: %w", err)
	}
	out := &InnerGenerateContentResponse{}
	if msg.Usage != nil {
		out.UsageMetadata = &UsageMetadata{
			PromptTokenCount:     msg.Usage.PromptTokens,
			CandidatesTokenCount: msg.Usage.CompletionTokens,
			TotalTokenCount:      msg.Usage.TotalTokens,
		}
	}
	for _, ch := range msg.Choices {
		var parts []*Part
		if ch.Delta.ReasoningContent != "" {
			parts = append(parts, &Part{Text: ch.Delta.ReasoningContent, Thought: true})
		}
		if ch.Delta.Content != "" {
			parts = append(parts, &Part{Text: ch.Delta.Content})
		}
		for _, tc := range ch.Delta.ToolCalls {
			args := map[string]any{}
			if strings.TrimSpace(tc.Function.Arguments) != "" {
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			}
			parts = append(parts, &Part{
				FunctionCall: &FunctionCall{
					ID:   tc.ID,
					Name: tc.Function.Name,
					Args: args,
				},
			})
		}
		out.Candidates = append(out.Candidates, &Candidate{
			Index: ch.Index,
			Content: &Content{
				Role:  "model",
				Parts: parts,
			},
			FinishReason: strings.ToUpper(ch.FinishReason),
		})
	}
	return out, nil
}
