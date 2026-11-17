// Reconstructed from agy v1.5.10 binary evidence (disassembly + Go type table).
// Binary VA: 0x10232a3b0 | Size: 8 funcs (first func SupportsTerminal, 32 B) | Source: third_party/jetski/cli/acp/schema/protocol.go:L23
// Reconstructed from binary: /Users/mac/.local/bin/agy (v1.5.10)
// Source: third_party/jetski/cli/acp/schema/protocol.go

package schema

import (
	"encoding/json/jsontext"

	"google3/third_party/jetski/cli/acp/messages/messages"
)

// Binary VA: 0x10a1b1fc0 | Size: 8 B
type AgentAuthCapabilities struct {
	Logout *struct{} `json:"logout,omitempty"`
}

// Binary VA: 0x10a88d060 | Size: 64 B
type AgentCapabilities struct {
	LoadSession              bool                   `json:"loadSession"`
	LoadSessionSnake         bool                   `json:"load_session"`
	PromptCapabilities       *PromptCapabilities    `json:"promptCapabilities,omitempty"`
	SessionCapabilities      *SessionCapabilities   `json:"sessionCapabilities,omitempty"`
	SessionCapabilitiesSnake *SessionCapabilities   `json:"session_capabilities,omitempty"`
	MCPCapabilities          *MCPCapabilities       `json:"mcpCapabilities,omitempty"`
	MCPCapabilitiesSnake     *MCPCapabilities       `json:"mcp_capabilities,omitempty"`
	Auth                     *AgentAuthCapabilities `json:"auth,omitempty"`
	Meta                     *AgentCapabilitiesMeta `json:"_meta,omitempty"`
}

// Binary VA: 0x10a1b2038 | Size: 8 B
type AgentCapabilitiesMeta struct {
	Antigravity *AntigravityCapabilitiesMeta `json:"antigravity,omitempty"`
}

// Binary VA: 0x10a1b20b0 | Size: 16 B
type AllowedOutcome struct {
	OptionID string `json:"optionId"`
}

// Binary VA: 0x10a307a50 | Size: 16 B
type AntigravityCapabilitiesMeta struct {
	Tier         bool                        `json:"tier"`
	SystemPrompt *AntigravitySystemPromptCap `json:"systemPrompt,omitempty"`
}

// Binary VA: 0x10a1b2128 | Size: 1 B
type AntigravitySystemPromptCap struct {
	Append bool `json:"append"`
}

// Binary VA: 0x10a307b70 | Size: 16 B
type ClientAuthCapabilities struct {
	Terminal bool           `json:"terminal,omitempty"`
	Meta     map[string]any `json:"_meta,omitempty"`
}

// Binary VA: 0x10232a3b0 | Size: 32 B | Source: third_party/jetski/cli/acp/schema/protocol.go:L23
func (c *ClientAuthCapabilities) SupportsTerminal() bool {
	return c != nil && c.Terminal
}

// Binary VA: 0x10a307c00 | Size: 16 B
type ClientCapabilities struct {
	Auth        *ClientAuthCapabilities        `json:"auth,omitempty"`
	Elicitation *ClientElicitationCapabilities `json:"elicitation,omitempty"`
}

// Binary VA: 0x10a7b92f8 | Size: 120 B
type ConfigOption struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Description  string              `json:"description,omitempty"`
	Category     string              `json:"category,omitempty"`
	Type         string              `json:"type"`
	CurrentValue string              `json:"currentValue"`
	Options      []ConfigOptionValue `json:"options,omitempty"`
}

// Binary VA: 0x10a45c728 | Size: 48 B
type ConfigOptionValue struct {
	Value       string `json:"value"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Binary VA: 0x10a45c878 | Size: 48 B
type Implementation struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
}

// Binary VA: 0x10a7b9400 | Size: 72 B
type InitializeParams struct {
	ProtocolVersion         int                 `json:"protocolVersion"`
	ClientInfo              jsontext.Value      `json:"clientInfo,omitempty"`
	ClientInfoSnake         jsontext.Value      `json:"client_info,omitempty"`
	ClientCapabilities      *ClientCapabilities `json:"clientCapabilities,omitempty"`
	ClientCapabilitiesSnake *ClientCapabilities `json:"client_capabilities,omitempty"`
}

// Binary VA: 0x10232a3d0 | Size: 160 B | Source: third_party/jetski/cli/acp/schema/protocol.go:L44
func (p InitializeParams) ParsedClientInfo() ClientInfo {
	if len(p.ClientInfo) > 0 {
		return parseClientInfo(p.ClientInfo)
	}
	return parseClientInfo(p.ClientInfoSnake)
}

// Binary VA: 0x10232a470 | Size: 32 B | Source: third_party/jetski/cli/acp/schema/protocol.go:L53
func (p InitializeParams) Capabilities() *ClientCapabilities {
	if p.ClientCapabilities != nil {
		return p.ClientCapabilities
	}
	return p.ClientCapabilitiesSnake
}

// Binary VA: 0x10232a490 | Size: 144 B | Source: third_party/jetski/cli/acp/schema/protocol.go:L62
func (p InitializeParams) SupportsElicitationForm() bool {
	caps := p.Capabilities()
	return caps != nil && caps.Elicitation.SupportsForm()
}

// Binary VA: 0x10a55b028 | Size: 144 B
type InitializeResult struct {
	ProtocolVersion   int               `json:"protocolVersion"`
	AgentCapabilities AgentCapabilities `json:"agentCapabilities"`
	AgentInfo         Implementation    `json:"agentInfo"`
	AuthMethods       []AuthMethod      `json:"authMethods"`
}

// Binary VA: 0x10a307db0 | Size: 40 B
type ListSessionsResult struct {
	Sessions   []SessionInfo `json:"sessions"`
	NextCursor string        `json:"nextCursor,omitempty"`
}

// Binary VA: 0x10a1b2308 | Size: 1 B
type MCPCapabilities struct {
	HTTP bool `json:"http,omitempty"`
}

// Binary VA: 0x10a7b9508 | Size: 120 B
type MCPServerConfig struct {
	Type    string   `json:"type,omitempty"`
	Name    string   `json:"name"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	Env     any      `json:"env,omitempty"`
	URL     string   `json:"url,omitempty"`
	Headers any      `json:"headers,omitempty"`
}

// Binary VA: 0x10a45c920 | Size: 48 B
type ModelInfo struct {
	ModelID     string `json:"modelId"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Binary VA: 0x10a62dd40 | Size: 80 B
type SessionMeta struct {
	DisableBuiltInTools      bool           `json:"disable_built_in_tools"`
	DisableBuiltInToolsCamel bool           `json:"disableBuiltInTools"`
	AdditionalRoots          []string       `json:"additional_roots,omitempty"`
	AdditionalRootsCamel     []string       `json:"additionalRoots,omitempty"`
	Antigravity              jsontext.Value `json:"antigravity,omitempty"`
}

// Binary VA: 0x10232a520 | Size: 64 B | Source: third_party/jetski/cli/acp/schema/protocol.go:L167
func (m *SessionMeta) AdditionalRootList() []string {
	if m == nil {
		return nil
	}
	if len(m.AdditionalRoots) > 0 {
		return m.AdditionalRoots
	}
	return m.AdditionalRootsCamel
}

// Binary VA: 0x10a842798 | Size: 120 B
type NewSessionParams struct {
	Cwd                   string            `json:"cwd"`
	MCPServers            []MCPServerConfig `json:"mcp_servers,omitempty"`
	MCPServersCamel       []MCPServerConfig `json:"mcpServers,omitempty"`
	AdditionalDirectories []string          `json:"additional_directories,omitempty"`
	AdditionalDirsCamel   []string          `json:"additionalDirectories,omitempty"`
	Meta                  *SessionMeta      `json:"_meta,omitempty"`
}

// Binary VA: 0x10232a560 | Size: 48 B | Source: third_party/jetski/cli/acp/schema/protocol.go:L189
func (p NewSessionParams) DisableBuiltInTools() bool {
	return p.Meta != nil && (p.Meta.DisableBuiltInTools || p.Meta.DisableBuiltInToolsCamel)
}

// Binary VA: 0x10232a590 | Size: 64 B | Source: third_party/jetski/cli/acp/schema/protocol.go:L195
func (p NewSessionParams) MCPServerList() []MCPServerConfig {
	if len(p.MCPServers) > 0 {
		return p.MCPServers
	}
	return p.MCPServersCamel
}

// Binary VA: 0x10232a5d0 | Size: 1248 B | Source: third_party/jetski/cli/acp/schema/protocol.go:L204
func (p NewSessionParams) AdditionalDirs() []string {
	seen := make(map[string]bool) /* inferred */
	var out []string               /* inferred */
	for _, s := range p.AdditionalDirectories {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, s := range p.AdditionalDirsCamel {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, s := range p.Meta.AdditionalRootList() {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// Binary VA: 0x10a62dab8 | Size: 64 B
type NewSessionResult struct {
	SessionID     string             `json:"sessionId"`
	Models        *SessionModelState `json:"models,omitempty"`
	Modes         *SessionModeState  `json:"modes,omitempty"`
	ConfigOptions []ConfigOption     `json:"configOptions,omitempty"`
	Meta          map[string]any     `json:"_meta,omitempty"`
}

// Binary VA: 0x10a45c9c8 | Size: 48 B
type PermissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind,omitempty"`
}

// Binary VA: 0x10a45ca70 | Size: 3 B
type PromptCapabilities struct {
	Image           bool `json:"image"`
	Audio           bool `json:"audio"`
	EmbeddedContext bool `json:"embeddedContext"`
}

// Binary VA: 0x10a55b0e8 | Size: 64 B
type PromptParams struct {
	SessionID string                 `json:"sessionId"`
	Prompt    messages.ContentBlocks `json:"prompt"`
	MessageID string                 `json:"messageId,omitempty"`
	Meta      *SessionMeta           `json:"_meta,omitempty"`
}

// Binary VA: 0x10a307e40 | Size: 24 B
type PromptResult struct {
	StopReason string          `json:"stopReason"`
	Usage      *messages.Usage `json:"usage,omitempty"`
}

// Binary VA: 0x10a45cb18 | Size: 216 B
type RequestPermissionParams struct {
	SessionID string                         `json:"sessionId"`
	ToolCall  messages.ToolCallSessionUpdate `json:"toolCall"`
	Options   []PermissionOption             `json:"options"`
}

// Binary VA: 0x10a1b2380 | Size: 16 B
type RequestPermissionResult struct {
	Outcome AllowedOutcome `json:"outcome"`
}

// Binary VA: 0x10a0c0338 | Size: 0 B
type SessionAdditionalDirectoriesCapabilities struct{}

// Binary VA: 0x10a62db90 | Size: 40 B
type SessionCapabilities struct {
	Resume                *SessionResumeCapabilities                `json:"resume,omitempty"`
	Close                 *SessionCloseCapabilities                 `json:"close,omitempty"`
	Load                  *SessionLoadCapabilities                  `json:"load,omitempty"`
	List                  *SessionListCapabilities                  `json:"list,omitempty"`
	AdditionalDirectories *SessionAdditionalDirectoriesCapabilities `json:"additionalDirectories,omitempty"`
}

// Binary VA: 0x10a0c0398 | Size: 0 B
type SessionCloseCapabilities struct{}

// Binary VA: 0x10a62dc68 | Size: 88 B
type SessionInfo struct {
	SessionID             string   `json:"sessionId"`
	Cwd                   string   `json:"cwd"`
	Title                 string   `json:"title,omitempty"`
	UpdatedAt             string   `json:"updatedAt,omitempty"`
	AdditionalDirectories []string `json:"additionalDirectories,omitempty"`
}

// Binary VA: 0x10a0c03f8 | Size: 0 B
type SessionListCapabilities struct{}

// Binary VA: 0x10a0c0458 | Size: 0 B
type SessionLoadCapabilities struct{}

// Binary VA: 0x10a45cbc0 | Size: 48 B
type SessionMode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Binary VA: 0x10a307ed0 | Size: 40 B
type SessionModeState struct {
	CurrentModeID  string        `json:"currentModeId"`
	AvailableModes []SessionMode `json:"availableModes"`
}

// Binary VA: 0x10a307f60 | Size: 40 B
type SessionModelState struct {
	AvailableModels []ModelInfo `json:"availableModels"`
	CurrentModelID  string      `json:"currentModelId"`
}

// Binary VA: 0x10a307ff0 | Size: 32 B
type SessionNotification struct {
	SessionID string                  `json:"sessionId"`
	Update    messages.SessionUpdate `json:"update"`
}

// Binary VA: 0x10a0c04b8 | Size: 0 B
type SessionResumeCapabilities struct{}
