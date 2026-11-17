package printmode

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"antigravity.dev/agy-sdk/pkg/aicode"
	"antigravity.dev/agy-sdk/pkg/cortex"
)

func TestStreamJSONEmitter(t *testing.T) {
	var out, errOut bytes.Buffer
	em, err := NewEmitter(FormatStreamJSON, &out, &errOut)
	if err != nil {
		t.Fatalf("NewEmitter: %v", err)
	}

	if err := em.EmitInit(InitPayload{
		SessionID: "sess-123",
		Model:     "gemini-4-hydrogen-medium",
		WorkDir:   "/tmp/ws",
	}); err != nil {
		t.Fatalf("EmitInit: %v", err)
	}

	events := []cortex.Event{
		{Type: cortex.EventThought, Text: "Checking file."},
		{Type: cortex.EventToolStart, ToolCall: &aicode.FunctionCall{
			ID:   "call-1",
			Name: "read_file",
			Args: map[string]any{"Path": "main.go"},
		}},
		{Type: cortex.EventToolDone, ToolCall: &aicode.FunctionCall{
			ID:   "call-1",
			Name: "read_file",
		}, ToolResult: &cortex.ToolResult{Text: "package main"}},
		{Type: cortex.EventText, Text: "Done reading."},
	}
	for _, ev := range events {
		if err := em.EmitEvent(ev); err != nil {
			t.Fatalf("EmitEvent(%s): %v", ev.Type, err)
		}
	}

	summary := &cortex.TurnSummary{
		Steps:        2,
		FinalText:    "Done reading.",
		ThoughtText:  "Checking file.",
		ToolCalls:    1,
		FinishReason: "STOP",
		Usage:        &aicode.UsageMetadata{TotalTokenCount: 42},
	}
	if err := em.EmitResult(summary, nil); err != nil {
		t.Fatalf("EmitResult: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	wantTypes := []string{"init", "thought", "tool_use", "tool_result", "assistant", "result"}
	if len(lines) != len(wantTypes) {
		t.Fatalf("got %d NDJSON lines, want %d:\n%s", len(lines), len(wantTypes), out.String())
	}

	for i, line := range lines {
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Fatalf("line %d invalid JSON: %v", i, err)
		}
		if obj["type"] != wantTypes[i] {
			t.Fatalf("line %d type = %v, want %v", i, obj["type"], wantTypes[i])
		}
	}
}

func TestJSONAndTextEmitters(t *testing.T) {
	t.Run("json success", func(t *testing.T) {
		var out, errOut bytes.Buffer
		em, err := NewEmitter(FormatJSON, &out, &errOut)
		if err != nil {
			t.Fatalf("NewEmitter: %v", err)
		}
		_ = em.EmitInit(InitPayload{SessionID: "s1", Model: "m1"})
		_ = em.EmitEvent(cortex.Event{Type: cortex.EventText, Text: "ignored until result"})
		err = em.EmitResult(&cortex.TurnSummary{
			Steps:        1,
			FinalText:    "hello json",
			ToolCalls:    0,
			FinishReason: "STOP",
		}, nil)
		if err != nil {
			t.Fatalf("EmitResult: %v", err)
		}

		var res ResultPayload
		if err := json.Unmarshal(out.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal json result: %v", err)
		}
		if res.Type != "result" || res.Status != "success" || res.Response != "hello json" || res.SessionID != "s1" {
			t.Fatalf("unexpected ResultPayload: %+v", res)
		}
	})

	t.Run("json error", func(t *testing.T) {
		var out, errOut bytes.Buffer
		em, _ := NewEmitter(FormatJSON, &out, &errOut)
		_ = em.EmitResult(nil, errors.New("boom"))
		var res ResultPayload
		if err := json.Unmarshal(out.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if res.Status != "error" || res.Error != "boom" {
			t.Fatalf("unexpected error payload: %+v", res)
		}
	})

	t.Run("text", func(t *testing.T) {
		var out, errOut bytes.Buffer
		em, _ := NewEmitter(FormatText, &out, &errOut)
		_ = em.EmitEvent(cortex.Event{Type: cortex.EventText, Text: "hello"})
		_ = em.EmitResult(&cortex.TurnSummary{FinalText: "hello"}, nil)
		if out.String() != "hello\n" {
			t.Fatalf("text output = %q, want %q", out.String(), "hello\n")
		}
	})
}

func TestReadStreamInput(t *testing.T) {
	input := strings.Join([]string{
		`{"event":"user_message","message":{"content":"first prompt"}}`,
		``,
		`{"event":"user_message","message":{"content":[{"type":"text","text":"second "},{"type":"text","text":"prompt"}]}}`,
	}, "\n")

	msgCh, errCh := ReadStreamInput(strings.NewReader(input))
	var prompts []string
	for msg := range msgCh {
		p, err := msg.Message.PromptText()
		if err != nil {
			t.Fatalf("PromptText: %v", err)
		}
		prompts = append(prompts, p)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("unexpected ReadStreamInput error: %v", err)
	}

	if len(prompts) != 2 || prompts[0] != "first prompt" || prompts[1] != "second prompt" {
		t.Fatalf("unexpected prompts: %#v", prompts)
	}

	t.Run("invalid json", func(t *testing.T) {
		mCh, eCh := ReadStreamInput(strings.NewReader("{bad json\n"))
		for range mCh {
		}
		if err := <-eCh; err == nil {
			t.Fatalf("expected parse error, got nil")
		}
	})
}

func TestValidateJSONSchema(t *testing.T) {
	schema := []byte(`{
		"type": "object",
		"required": ["name", "count", "active", "tags"],
		"properties": {
			"name": {"type": "string", "enum": ["alpha", "beta"]},
			"count": {"type": "integer"},
			"score": {"type": "number"},
			"active": {"type": "boolean"},
			"tags": {
				"type": "array",
				"items": {"type": "string"}
			}
		}
	}`)

	valid := []byte(`{
		"name": "alpha",
		"count": 3,
		"score": 9.5,
		"active": true,
		"tags": ["go", "sdk"]
	}`)
	if err := ValidateJSONSchema(schema, valid); err != nil {
		t.Fatalf("expected valid instance to pass, got: %v", err)
	}

	failCases := []struct {
		name     string
		instance string
	}{
		{"missing required", `{"name":"alpha","count":1,"active":true}`},
		{"wrong enum", `{"name":"gamma","count":1,"active":true,"tags":[]}`},
		{"float for integer", `{"name":"alpha","count":1.5,"active":true,"tags":[]}`},
		{"string for boolean", `{"name":"alpha","count":1,"active":"yes","tags":[]}`},
		{"bad array item", `{"name":"alpha","count":1,"active":true,"tags":["ok", 42]}`},
		{"wrong root type", `["not", "an", "object"]`},
	}

	for _, tc := range failCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateJSONSchema(schema, []byte(tc.instance)); err == nil {
				t.Fatalf("expected validation error for %s, got nil", tc.name)
			}
		})
	}
}
