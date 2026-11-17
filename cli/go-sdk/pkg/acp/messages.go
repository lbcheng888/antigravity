package acp

import (
	"encoding/json"
	"strings"
)

// ContentBlock represents an ACP content element (text, image, or resource_link).
type ContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	URI      string `json:"uri,omitempty"`
	Name     string `json:"name,omitempty"`
	Title    string `json:"title,omitempty"`
}

func NewTextContentBlock(text string) ContentBlock {
	return ContentBlock{Type: "text", Text: text}
}

// ExtractPromptText flattens prompt content blocks into plain text, preserving resource URIs as references.
func ExtractPromptText(blocks []ContentBlock) string {
	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case "", "text":
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		case "resource_link":
			if b.URI != "" {
				parts = append(parts, "@"+b.URI)
			} else if b.Name != "" {
				parts = append(parts, b.Name)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// ToolCallLocation links a tool call to a file path and optional line number.
type ToolCallLocation struct {
	Path string `json:"path"`
	Line *int   `json:"line,omitempty"`
}

// ToolCallContent represents either inline content or a unified diff inside a tool call update.
type ToolCallContent struct {
	Type    string        `json:"type"`
	Content *ContentBlock `json:"content,omitempty"`
	Path    string        `json:"path,omitempty"`
	OldText string        `json:"oldText,omitempty"`
	NewText string        `json:"newText,omitempty"`
}

// ContentToolCallContent builds an inline text content block for a tool call update.
func ContentToolCallContent(text string) ToolCallContent {
	cb := NewTextContentBlock(text)
	return ToolCallContent{
		Type:    "content",
		Content: &cb,
	}
}

// DiffToolCallContent builds a unified diff content block for a tool call update.
func DiffToolCallContent(path, oldText, newText string) ToolCallContent {
	return ToolCallContent{
		Type:    "diff",
		Path:    path,
		OldText: oldText,
		NewText: newText,
	}
}

// Usage mirrors token accounting in PromptResult.
type Usage struct {
	InputTokens       int64 `json:"inputTokens"`
	OutputTokens      int64 `json:"outputTokens"`
	CachedReadTokens  int64 `json:"cachedReadTokens"`
	CachedWriteTokens int64 `json:"cachedWriteTokens"`
	ThoughtTokens     int64 `json:"thoughtTokens"`
	TotalTokens       int64 `json:"totalTokens"`
}

// AvailableCommandInput describes optional hint text for a slash command.
type AvailableCommandInput struct {
	Hint string `json:"hint"`
}

// AvailableCommand declares a slash command supported by the session.
type AvailableCommand struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Input       *AvailableCommandInput `json:"input,omitempty"`
}

// SessionUpdate is implemented by all 10 ACP session/update variants.
type SessionUpdate interface {
	SessionUpdateKind() string
}

// SessionUpdateNotification wraps an update for JSON-RPC notification method "session/update".
type SessionUpdateNotification struct {
	SessionID string `json:"sessionId"`
	Update    any    `json:"update"`
}

func marshalSessionUpdate(kind string, payload any) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	m["sessionUpdate"] = kind
	return json.Marshal(m)
}

type AgentThoughtChunkUpdate struct {
	Content ContentBlock `json:"content"`
}

func (AgentThoughtChunkUpdate) SessionUpdateKind() string { return "agent_thought_chunk" }
func (u AgentThoughtChunkUpdate) MarshalJSON() ([]byte, error) {
	type alias AgentThoughtChunkUpdate
	return marshalSessionUpdate(u.SessionUpdateKind(), alias(u))
}

type AgentMessageChunkUpdate struct {
	Content ContentBlock `json:"content"`
}

func (AgentMessageChunkUpdate) SessionUpdateKind() string { return "agent_message_chunk" }
func (u AgentMessageChunkUpdate) MarshalJSON() ([]byte, error) {
	type alias AgentMessageChunkUpdate
	return marshalSessionUpdate(u.SessionUpdateKind(), alias(u))
}

type UserMessageChunkUpdate struct {
	Content ContentBlock `json:"content"`
}

func (UserMessageChunkUpdate) SessionUpdateKind() string { return "user_message_chunk" }
func (u UserMessageChunkUpdate) MarshalJSON() ([]byte, error) {
	type alias UserMessageChunkUpdate
	return marshalSessionUpdate(u.SessionUpdateKind(), alias(u))
}

type AvailableCommandsUpdate struct {
	AvailableCommands []AvailableCommand `json:"availableCommands"`
}

func (AvailableCommandsUpdate) SessionUpdateKind() string { return "available_commands_update" }
func (u AvailableCommandsUpdate) MarshalJSON() ([]byte, error) {
	type alias AvailableCommandsUpdate
	return marshalSessionUpdate(u.SessionUpdateKind(), alias(u))
}

type ToolCallUpdate struct {
	ToolCallID string             `json:"toolCallId"`
	Name       string             `json:"name"`
	Args       any                `json:"args,omitempty"`
	Status     string             `json:"status"`
	Kind       string             `json:"kind,omitempty"`
	Title      string             `json:"title,omitempty"`
	Locations  []ToolCallLocation `json:"locations,omitempty"`
	RawInput   any                `json:"rawInput,omitempty"`
	RawOutput  any                `json:"rawOutput,omitempty"`
	Content    []ToolCallContent  `json:"content,omitempty"`
}

func (ToolCallUpdate) SessionUpdateKind() string { return "tool_call" }
func (u ToolCallUpdate) MarshalJSON() ([]byte, error) {
	type alias ToolCallUpdate
	return marshalSessionUpdate(u.SessionUpdateKind(), alias(u))
}

type ToolCallStatusUpdate struct {
	ToolCallID string             `json:"toolCallId"`
	Name       string             `json:"name"`
	Args       any                `json:"args,omitempty"`
	Status     string             `json:"status"`
	Kind       string             `json:"kind,omitempty"`
	Title      string             `json:"title,omitempty"`
	Locations  []ToolCallLocation `json:"locations,omitempty"`
	RawInput   any                `json:"rawInput,omitempty"`
	RawOutput  any                `json:"rawOutput,omitempty"`
	Content    []ToolCallContent  `json:"content,omitempty"`
}

func (ToolCallStatusUpdate) SessionUpdateKind() string { return "tool_call_update" }
func (u ToolCallStatusUpdate) MarshalJSON() ([]byte, error) {
	type alias ToolCallStatusUpdate
	return marshalSessionUpdate(u.SessionUpdateKind(), alias(u))
}

type ConfigOptionUpdate struct {
	ConfigOptions any `json:"configOptions"`
}

func (ConfigOptionUpdate) SessionUpdateKind() string { return "config_option_update" }
func (u ConfigOptionUpdate) MarshalJSON() ([]byte, error) {
	type alias ConfigOptionUpdate
	return marshalSessionUpdate(u.SessionUpdateKind(), alias(u))
}

type CurrentModeUpdate struct {
	CurrentModeID string `json:"currentModeId"`
}

func (CurrentModeUpdate) SessionUpdateKind() string { return "current_mode_update" }
func (u CurrentModeUpdate) MarshalJSON() ([]byte, error) {
	type alias CurrentModeUpdate
	return marshalSessionUpdate(u.SessionUpdateKind(), alias(u))
}

type SessionInfoUpdate struct {
	Title     string `json:"title,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

func (SessionInfoUpdate) SessionUpdateKind() string { return "session_info_update" }
func (u SessionInfoUpdate) MarshalJSON() ([]byte, error) {
	type alias SessionInfoUpdate
	return marshalSessionUpdate(u.SessionUpdateKind(), alias(u))
}

type UsageUpdate struct {
	Used int64 `json:"used"`
	Size int64 `json:"size"`
}

func (UsageUpdate) SessionUpdateKind() string { return "usage_update" }
func (u UsageUpdate) MarshalJSON() ([]byte, error) {
	type alias UsageUpdate
	return marshalSessionUpdate(u.SessionUpdateKind(), alias(u))
}
