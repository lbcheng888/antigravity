package printmode

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"antigravity.dev/agy-sdk/pkg/aicode"
	"antigravity.dev/agy-sdk/pkg/cortex"
)

// OutputFormat selects the output encoding for headless/CI printmode runs.
type OutputFormat string

const (
	FormatText       OutputFormat = "text"
	FormatJSON       OutputFormat = "json"
	FormatStreamJSON OutputFormat = "stream-json"
)

// InitPayload carries session metadata emitted at the start of a printmode run.
type InitPayload struct {
	SessionID string `json:"session_id,omitempty"`
	Model     string `json:"model,omitempty"`
	WorkDir   string `json:"work_dir,omitempty"`
}

// Emitter formats Cortex lifecycle events and turn summaries into text, JSON, or NDJSON.
type Emitter interface {
	EmitInit(payload InitPayload) error
	EmitEvent(ev cortex.Event) error
	EmitResult(summary *cortex.TurnSummary, err error) error
}

// NewEmitter constructs an Emitter for the requested OutputFormat.
func NewEmitter(format OutputFormat, w, errW io.Writer) (Emitter, error) {
	if w == nil {
		w = io.Discard
	}
	if errW == nil {
		errW = io.Discard
	}
	switch format {
	case "", FormatText:
		return &textEmitter{w: w, errW: errW}, nil
	case FormatJSON:
		return &jsonEmitter{w: w, errW: errW, start: time.Now()}, nil
	case FormatStreamJSON:
		return &streamJSONEmitter{w: w, errW: errW, start: time.Now()}, nil
	default:
		return nil, fmt.Errorf("unsupported output format %q", format)
	}
}

type textEmitter struct {
	mu          sync.Mutex
	w           io.Writer
	errW        io.Writer
	wroteText   bool
	endsNewline bool
}

func (e *textEmitter) EmitInit(InitPayload) error { return nil }

func (e *textEmitter) EmitEvent(ev cortex.Event) error {
	if ev.Type != cortex.EventText || ev.Text == "" {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.wroteText = true
	e.endsNewline = strings.HasSuffix(ev.Text, "\n")
	_, err := io.WriteString(e.w, ev.Text)
	return err
}

func (e *textEmitter) EmitResult(summary *cortex.TurnSummary, runErr error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.wroteText && summary != nil && summary.FinalText != "" {
		if _, err := io.WriteString(e.w, summary.FinalText); err != nil {
			return err
		}
		e.wroteText = true
		e.endsNewline = strings.HasSuffix(summary.FinalText, "\n")
	}
	if e.wroteText && !e.endsNewline {
		if _, err := io.WriteString(e.w, "\n"); err != nil {
			return err
		}
		e.endsNewline = true
	}
	if runErr != nil {
		_, err := fmt.Fprintf(e.errW, "error: %v\n", runErr)
		return err
	}
	return nil
}

// ResultPayload is the structured summary emitted for FormatJSON and the final FormatStreamJSON line.
type ResultPayload struct {
	Type         string                `json:"type"`
	SessionID    string                `json:"session_id,omitempty"`
	Model        string                `json:"model,omitempty"`
	Status       string                `json:"status"`
	Response     string                `json:"response,omitempty"`
	Thought      string                `json:"thought,omitempty"`
	Error        string                `json:"error,omitempty"`
	Steps        int                   `json:"steps,omitempty"`
	ToolCalls    int                   `json:"tool_calls,omitempty"`
	FinishReason string                `json:"finish_reason,omitempty"`
	Usage        *aicode.UsageMetadata `json:"usage,omitempty"`
	DurationMs   int64                 `json:"duration_ms,omitempty"`
	Timestamp    string                `json:"timestamp,omitempty"`
}

type jsonEmitter struct {
	mu    sync.Mutex
	w     io.Writer
	errW  io.Writer
	init  InitPayload
	start time.Time
}

func (e *jsonEmitter) EmitInit(payload InitPayload) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.init = payload
	return nil
}

func (e *jsonEmitter) EmitEvent(cortex.Event) error { return nil }

func (e *jsonEmitter) EmitResult(summary *cortex.TurnSummary, runErr error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	payload := buildResultPayload(e.init, summary, runErr, e.start)
	return writeJSONLine(e.w, payload)
}

type streamLine struct {
	Type      string         `json:"type"`
	SessionID string         `json:"session_id,omitempty"`
	Model     string         `json:"model,omitempty"`
	WorkDir   string         `json:"work_dir,omitempty"`
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	Output    any            `json:"output,omitempty"`
	Error     string         `json:"error,omitempty"`
	IsError   bool           `json:"is_error,omitempty"`
	Timestamp string         `json:"timestamp,omitempty"`
}

type streamJSONEmitter struct {
	mu    sync.Mutex
	w     io.Writer
	errW  io.Writer
	init  InitPayload
	start time.Time
}

func (e *streamJSONEmitter) EmitInit(payload InitPayload) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.init = payload
	return writeJSONLine(e.w, streamLine{
		Type:      "init",
		SessionID: payload.SessionID,
		Model:     payload.Model,
		WorkDir:   payload.WorkDir,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func (e *streamJSONEmitter) EmitEvent(ev cortex.Event) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339Nano)

	switch ev.Type {
	case cortex.EventThought:
		return writeJSONLine(e.w, streamLine{
			Type:      "thought",
			SessionID: e.init.SessionID,
			Text:      ev.Text,
			Timestamp: now,
		})
	case cortex.EventText:
		return writeJSONLine(e.w, streamLine{
			Type:      "assistant",
			SessionID: e.init.SessionID,
			Text:      ev.Text,
			Timestamp: now,
		})
	case cortex.EventToolStart:
		var id, name string
		var input map[string]any
		if ev.ToolCall != nil {
			id = ev.ToolCall.ID
			name = ev.ToolCall.Name
			input = ev.ToolCall.Args
		}
		return writeJSONLine(e.w, streamLine{
			Type:      "tool_use",
			SessionID: e.init.SessionID,
			ID:        id,
			Name:      name,
			Input:     input,
			Timestamp: now,
		})
	case cortex.EventToolDone:
		var id, name, errStr string
		if ev.ToolCall != nil {
			id = ev.ToolCall.ID
			name = ev.ToolCall.Name
		}
		var out any
		if ev.ToolResult != nil {
			if len(ev.ToolResult.Output) > 0 {
				out = ev.ToolResult.Output
			} else if ev.ToolResult.Text != "" {
				out = ev.ToolResult.Text
			}
		}
		if ev.Err != nil {
			errStr = ev.Err.Error()
		}
		return writeJSONLine(e.w, streamLine{
			Type:      "tool_result",
			SessionID: e.init.SessionID,
			ID:        id,
			Name:      name,
			Output:    out,
			Error:     errStr,
			IsError:   ev.Err != nil,
			Timestamp: now,
		})
	default:
		return nil
	}
}

func (e *streamJSONEmitter) EmitResult(summary *cortex.TurnSummary, runErr error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	payload := buildResultPayload(e.init, summary, runErr, e.start)
	payload.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	return writeJSONLine(e.w, payload)
}

func buildResultPayload(init InitPayload, summary *cortex.TurnSummary, runErr error, start time.Time) ResultPayload {
	status := "success"
	errStr := ""
	if runErr != nil {
		status = "error"
		errStr = runErr.Error()
	}
	res := ResultPayload{
		Type:       "result",
		SessionID:  init.SessionID,
		Model:      init.Model,
		Status:     status,
		Error:      errStr,
		DurationMs: time.Since(start).Milliseconds(),
	}
	if summary != nil {
		res.Response = summary.FinalText
		res.Thought = summary.ThoughtText
		res.Steps = summary.Steps
		res.ToolCalls = summary.ToolCalls
		res.FinishReason = summary.FinishReason
		res.Usage = summary.Usage
	}
	return res
}

func writeJSONLine(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

// StreamInputMessage represents one NDJSON line read from stream-json input.
type StreamInputMessage struct {
	Event   string                  `json:"event"`
	Message *StreamInputUserMessage `json:"message,omitempty"`
}

// StreamInputUserMessage carries a user prompt as either a JSON string or content blocks.
type StreamInputUserMessage struct {
	Content json.RawMessage `json:"content,omitempty"`
}

type streamInputContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// PromptText extracts the plain text prompt from Message.Content.
func (m *StreamInputUserMessage) PromptText() (string, error) {
	if m == nil || len(bytes.TrimSpace(m.Content)) == 0 {
		return "", nil
	}
	raw := bytes.TrimSpace(m.Content)
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", fmt.Errorf("parse string content: %w", err)
		}
		return s, nil
	}
	if raw[0] == '[' {
		var blocks []streamInputContentBlock
		if err := json.Unmarshal(raw, &blocks); err != nil {
			return "", fmt.Errorf("parse content blocks: %w", err)
		}
		var sb strings.Builder
		for _, b := range blocks {
			if b.Type == "" || b.Type == "text" {
				sb.WriteString(b.Text)
			}
		}
		return sb.String(), nil
	}
	return "", fmt.Errorf("unsupported content JSON %q", string(raw))
}

// ReadStreamInput parses NDJSON stream-json input prompts from r asynchronously.
func ReadStreamInput(r io.Reader) (<-chan StreamInputMessage, <-chan error) {
	msgCh := make(chan StreamInputMessage, 16)
	errCh := make(chan error, 1)

	go func() {
		defer close(msgCh)
		defer close(errCh)

		if r == nil {
			return
		}
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := bytes.TrimSpace(scanner.Bytes())
			if len(line) == 0 {
				continue
			}
			var msg StreamInputMessage
			if err := json.Unmarshal(line, &msg); err != nil {
				errCh <- fmt.Errorf("stream-json line %d: %w", lineNum, err)
				return
			}
			msgCh <- msg
		}
		if err := scanner.Err(); err != nil {
			errCh <- err
		}
	}()

	return msgCh, errCh
}
