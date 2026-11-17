package cortex

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"antigravity.dev/agy-sdk/pkg/aicode"
)

// BuildSystemPrompt constructs the Antigravity system instruction for the given workspace and mode.
func BuildSystemPrompt(workspaceRoot string, mode Mode, extraPrompt string) *aicode.Content {
	if mode == "" {
		mode = ModeAgent
	}

	var b strings.Builder
	b.WriteString("You are Antigravity, a powerful agentic AI coding assistant designed by the Google Deepmind team.\n")
	b.WriteString("You are working with a USER to solve their coding task.\n\n")

	b.WriteString("<workspace>\n")
	if workspaceRoot != "" {
		fmt.Fprintf(&b, "Workspace Root: %s\n", workspaceRoot)
	}
	fmt.Fprintf(&b, "Mode: %s\n", mode)
	switch mode {
	case ModePlan:
		b.WriteString("Policy: Read-only planning mode. Inspect files and produce a clear plan without modifying files or running commands.\n")
	case ModeAsk:
		b.WriteString("Policy: Read-only Q&A mode. Answer questions using read-only inspection tools without modifying files or running commands.\n")
	default:
		b.WriteString("Policy: Full agent mode. Use tools to inspect, edit, and verify changes within the workspace.\n")
	}
	b.WriteString("</workspace>\n\n")

	b.WriteString("<guidelines>\n")
	b.WriteString("- Keep your responses concise and formatted in GitHub-style markdown.\n")
	b.WriteString("- Only state file paths, symbol names, and command outputs that you directly observed.\n")
	b.WriteString("- Reference files using clickable file:// markdown links.\n")
	b.WriteString("</guidelines>\n")

	if strings.TrimSpace(extraPrompt) != "" {
		b.WriteString("\n")
		b.WriteString(strings.TrimSpace(extraPrompt))
		b.WriteString("\n")
	}

	return &aicode.Content{
		Role:  "system",
		Parts: []*aicode.Part{{Text: b.String()}},
	}
}

// Trajectory step types matching Antigravity transcript.jsonl.
const (
	StepTypeUserInput       = "USER_INPUT"
	StepTypePlannerResponse = "PLANNER_RESPONSE"
	StepTypeToolResult      = "TOOL_RESULT"
)

// TrajectoryStep represents a single JSONL record in transcript.jsonl.
type TrajectoryStep struct {
	StepIndex  int                    `json:"step_index"`
	Source     string                 `json:"source"`
	Type       string                 `json:"type"`
	Status     string                 `json:"status"`
	CreatedAt  string                 `json:"created_at"`
	Content    string                 `json:"content,omitempty"`
	Thinking   string                 `json:"thinking,omitempty"`
	ToolCalls  []*aicode.FunctionCall `json:"tool_calls,omitempty"`
	ToolResult map[string]any         `json:"tool_result,omitempty"`
}

// TrajectoryRecorder appends execution steps as JSONL to a transcript file.
type TrajectoryRecorder struct {
	mu        sync.Mutex
	file      *os.File
	stepIndex int
}

// NewTrajectoryRecorder opens or creates a JSONL transcript file for appending.
func NewTrajectoryRecorder(path string) (*TrajectoryRecorder, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("transcript path must not be empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create transcript dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open transcript: %w", err)
	}
	return &TrajectoryRecorder{file: f}, nil
}

// Record appends a TrajectoryStep as one JSON line.
func (r *TrajectoryRecorder) Record(step TrajectoryStep) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file == nil {
		return fmt.Errorf("trajectory recorder is closed")
	}

	if step.StepIndex == 0 && r.stepIndex > 0 {
		step.StepIndex = r.stepIndex
	} else if step.StepIndex == 0 {
		step.StepIndex = r.stepIndex
	}
	r.stepIndex++

	if step.CreatedAt == "" {
		step.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if step.Status == "" {
		step.Status = "DONE"
	}

	line, err := json.Marshal(step)
	if err != nil {
		return fmt.Errorf("marshal trajectory step: %w", err)
	}
	if _, err := r.file.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write trajectory step: %w", err)
	}
	return nil
}

// RecordUserInput logs a USER_INPUT step.
func (r *TrajectoryRecorder) RecordUserInput(text string) error {
	return r.Record(TrajectoryStep{
		Source:  "USER_EXPLICIT",
		Type:    StepTypeUserInput,
		Status:  "DONE",
		Content: text,
	})
}

// RecordPlannerResponse logs a PLANNER_RESPONSE step.
func (r *TrajectoryRecorder) RecordPlannerResponse(thinking, text string, toolCalls []*aicode.FunctionCall) error {
	return r.Record(TrajectoryStep{
		Source:    "MODEL",
		Type:      StepTypePlannerResponse,
		Status:    "DONE",
		Content:   text,
		Thinking:  thinking,
		ToolCalls: toolCalls,
	})
}

// RecordToolResult logs a TOOL_RESULT step.
func (r *TrajectoryRecorder) RecordToolResult(name string, result ToolResult, execErr error) error {
	status := "DONE"
	if execErr != nil {
		status = "ERROR"
	}
	payload := result.toResponseMap(execErr)
	payload["tool_name"] = name
	return r.Record(TrajectoryStep{
		Source:     "SYSTEM",
		Type:       StepTypeToolResult,
		Status:     status,
		Content:    result.Text,
		ToolResult: payload,
	})
}

// Close closes the underlying transcript file.
func (r *TrajectoryRecorder) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file == nil {
		return nil
	}
	err := r.file.Close()
	r.file = nil
	return err
}
