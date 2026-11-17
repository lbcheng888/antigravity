// Reconstructed from agy v1.5.10 binary evidence (disassembly + Go type table).
// Binary VA: 0x10025fe50 | Size: 20 funcs (first func SessionUpdate, 16 B) | Source: third_party/jetski/cli/acp/messages/session.go:L24
// Reconstructed from binary: /Users/mac/.local/bin/agy (v1.5.10)
// Source: third_party/jetski/cli/acp/messages/session.go

package messages

import jsonv2 "encoding/json/v2"

// Binary VA: 0x10a161cf0 | Size: 16 B
type SessionUpdate interface {
	SessionUpdate() string
	isSessionUpdate()
}

// Binary VA: 0x10a47e1f0 | Size: 16 B
type AgentThoughtChunkSessionUpdate struct {
	Content ContentBlock `json:"content"`
}

func (AgentThoughtChunkSessionUpdate) isSessionUpdate() {}

// Binary VA: 0x10025fe50 | Size: 16 B | Source: third_party/jetski/cli/acp/messages/session.go:L24
func (AgentThoughtChunkSessionUpdate) SessionUpdate() string {
	return "agent_thought_chunk"
}

// Binary VA: 0x10025fe60 | Size: 160 B | Source: third_party/jetski/cli/acp/messages/session.go:L27
func (u AgentThoughtChunkSessionUpdate) MarshalJSON() ([]byte, error) {
	type base AgentThoughtChunkSessionUpdate
	return jsonv2.Marshal(struct {
		SessionUpdate string `json:"sessionUpdate"`
		base
	}{
		SessionUpdate: u.SessionUpdate(),
		base:          base(u),
	})
}

// Binary VA: 0x10a47e148 | Size: 16 B
type AgentMessageChunkSessionUpdate struct {
	Content ContentBlock `json:"content"`
}

func (AgentMessageChunkSessionUpdate) isSessionUpdate() {}

// Binary VA: 0x10025ff00 | Size: 16 B | Source: third_party/jetski/cli/acp/messages/session.go:L43
func (AgentMessageChunkSessionUpdate) SessionUpdate() string {
	return "agent_message_chunk"
}

// Binary VA: 0x10025ff10 | Size: 160 B | Source: third_party/jetski/cli/acp/messages/session.go:L46
func (u AgentMessageChunkSessionUpdate) MarshalJSON() ([]byte, error) {
	type base AgentMessageChunkSessionUpdate
	return jsonv2.Marshal(struct {
		SessionUpdate string `json:"sessionUpdate"`
		base
	}{
		SessionUpdate: u.SessionUpdate(),
		base:          base(u),
	})
}

// Binary VA: 0x10a47e730 | Size: 16 B
type UserMessageChunkSessionUpdate struct {
	Content ContentBlock `json:"content"`
}

func (UserMessageChunkSessionUpdate) isSessionUpdate() {}

// Binary VA: 0x10025ffb0 | Size: 16 B | Source: third_party/jetski/cli/acp/messages/session.go:L62
func (UserMessageChunkSessionUpdate) SessionUpdate() string {
	return "user_message_chunk"
}

// Binary VA: 0x10025ffc0 | Size: 160 B | Source: third_party/jetski/cli/acp/messages/session.go:L65
func (u UserMessageChunkSessionUpdate) MarshalJSON() ([]byte, error) {
	type base UserMessageChunkSessionUpdate
	return jsonv2.Marshal(struct {
		SessionUpdate string `json:"sessionUpdate"`
		base
	}{
		SessionUpdate: u.SessionUpdate(),
		base:          base(u),
	})
}

// Binary VA: 0x10a1d3f80 | Size: 16 B
type AvailableCommandInput struct {
	Hint string `json:"hint"`
}

// Binary VA: 0x10a47e298 | Size: 40 B
type AvailableCommand struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Input       *AvailableCommandInput `json:"input,omitempty"`
}

// Binary VA: 0x10a47e340 | Size: 24 B
type AvailableCommandsSessionUpdate struct {
	AvailableCommands []AvailableCommand `json:"availableCommands"`
}

func (AvailableCommandsSessionUpdate) isSessionUpdate() {}

// Binary VA: 0x100260060 | Size: 16 B | Source: third_party/jetski/cli/acp/messages/session.go:L94
func (AvailableCommandsSessionUpdate) SessionUpdate() string {
	return "available_commands_update"
}

// Binary VA: 0x100260070 | Size: 160 B | Source: third_party/jetski/cli/acp/messages/session.go:L97
func (u AvailableCommandsSessionUpdate) MarshalJSON() ([]byte, error) {
	type base AvailableCommandsSessionUpdate
	return jsonv2.Marshal(struct {
		SessionUpdate string `json:"sessionUpdate"`
		base
	}{
		SessionUpdate: u.SessionUpdate(),
		base:          base(u),
	})
}

// Binary VA: 0x10a909280 | Size: 176 B
type ToolCallSessionUpdate struct {
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

func (ToolCallSessionUpdate) isSessionUpdate() {}

// Binary VA: 0x100260110 | Size: 16 B | Source: third_party/jetski/cli/acp/messages/session.go:L130
func (ToolCallSessionUpdate) SessionUpdate() string {
	return "tool_call"
}

// Binary VA: 0x100260120 | Size: 240 B | Source: third_party/jetski/cli/acp/messages/session.go:L133
func (u ToolCallSessionUpdate) MarshalJSON() ([]byte, error) {
	type base ToolCallSessionUpdate
	return jsonv2.Marshal(struct {
		SessionUpdate string `json:"sessionUpdate"`
		base
	}{
		SessionUpdate: u.SessionUpdate(),
		base:          base(u),
	})
}

// Binary VA: 0x10a909400 | Size: 176 B
type ToolCallUpdateSessionUpdate struct {
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

func (ToolCallUpdateSessionUpdate) isSessionUpdate() {}

// Binary VA: 0x100260210 | Size: 16 B | Source: third_party/jetski/cli/acp/messages/session.go:L161
func (ToolCallUpdateSessionUpdate) SessionUpdate() string {
	return "tool_call_update"
}

// Binary VA: 0x100260220 | Size: 240 B | Source: third_party/jetski/cli/acp/messages/session.go:L164
func (u ToolCallUpdateSessionUpdate) MarshalJSON() ([]byte, error) {
	type base ToolCallUpdateSessionUpdate
	return jsonv2.Marshal(struct {
		SessionUpdate string `json:"sessionUpdate"`
		base
	}{
		SessionUpdate: u.SessionUpdate(),
		base:          base(u),
	})
}

// Binary VA: 0x10a47e3e8 | Size: 16 B
type ConfigOptionUpdateSessionUpdate struct {
	ConfigOptions any `json:"configOptions"`
}

func (ConfigOptionUpdateSessionUpdate) isSessionUpdate() {}

// Binary VA: 0x100260310 | Size: 16 B | Source: third_party/jetski/cli/acp/messages/session.go:L183
func (ConfigOptionUpdateSessionUpdate) SessionUpdate() string {
	return "config_option_update"
}

// Binary VA: 0x100260320 | Size: 160 B | Source: third_party/jetski/cli/acp/messages/session.go:L186
func (u ConfigOptionUpdateSessionUpdate) MarshalJSON() ([]byte, error) {
	type base ConfigOptionUpdateSessionUpdate
	return jsonv2.Marshal(struct {
		SessionUpdate string `json:"sessionUpdate"`
		base
	}{
		SessionUpdate: u.SessionUpdate(),
		base:          base(u),
	})
}

// Binary VA: 0x10a47e538 | Size: 16 B
type CurrentModeUpdateSessionUpdate struct {
	CurrentModeID string `json:"currentModeId"`
}

func (CurrentModeUpdateSessionUpdate) isSessionUpdate() {}

// Binary VA: 0x1002603c0 | Size: 16 B | Source: third_party/jetski/cli/acp/messages/session.go:L204
func (CurrentModeUpdateSessionUpdate) SessionUpdate() string {
	return "current_mode_update"
}

// Binary VA: 0x1002603d0 | Size: 160 B | Source: third_party/jetski/cli/acp/messages/session.go:L207
func (u CurrentModeUpdateSessionUpdate) MarshalJSON() ([]byte, error) {
	type base CurrentModeUpdateSessionUpdate
	return jsonv2.Marshal(struct {
		SessionUpdate string `json:"sessionUpdate"`
		base
	}{
		SessionUpdate: u.SessionUpdate(),
		base:          base(u),
	})
}

// Binary VA: 0x10a58f2e8 | Size: 32 B
type SessionInfoUpdateSessionUpdate struct {
	Title     string `json:"title,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

func (SessionInfoUpdateSessionUpdate) isSessionUpdate() {}

// Binary VA: 0x100260470 | Size: 16 B | Source: third_party/jetski/cli/acp/messages/session.go:L226
func (SessionInfoUpdateSessionUpdate) SessionUpdate() string {
	return "session_info_update"
}

// Binary VA: 0x100260480 | Size: 160 B | Source: third_party/jetski/cli/acp/messages/session.go:L229
func (u SessionInfoUpdateSessionUpdate) MarshalJSON() ([]byte, error) {
	type base SessionInfoUpdateSessionUpdate
	return jsonv2.Marshal(struct {
		SessionUpdate string `json:"sessionUpdate"`
		base
	}{
		SessionUpdate: u.SessionUpdate(),
		base:          base(u),
	})
}

// Binary VA: 0x10a7413b0 | Size: 48 B
type Usage struct {
	InputTokens       int64 `json:"inputTokens"`
	OutputTokens      int64 `json:"outputTokens"`
	CachedReadTokens  int64 `json:"cachedReadTokens"`
	CachedWriteTokens int64 `json:"cachedWriteTokens"`
	ThoughtTokens     int64 `json:"thoughtTokens"`
	TotalTokens       int64 `json:"totalTokens"`
}

// Binary VA: 0x10a58f3a8 | Size: 16 B
type UsageUpdateSessionUpdate struct {
	Used int64 `json:"used"`
	Size int64 `json:"size"`
}

func (UsageUpdateSessionUpdate) isSessionUpdate() {}

// Binary VA: 0x100260520 | Size: 16 B | Source: third_party/jetski/cli/acp/messages/session.go:L252
func (UsageUpdateSessionUpdate) SessionUpdate() string {
	return "usage_update"
}

// Binary VA: 0x100260530 | Size: 144 B | Source: third_party/jetski/cli/acp/messages/session.go:L255
func (u UsageUpdateSessionUpdate) MarshalJSON() ([]byte, error) {
	type base UsageUpdateSessionUpdate
	return jsonv2.Marshal(struct {
		SessionUpdate string `json:"sessionUpdate"`
		base
	}{
		SessionUpdate: u.SessionUpdate(),
		base:          base(u),
	})
}
