package acp

import "encoding/json"

type Implementation struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
}

type AuthMethod struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type PromptCapabilities struct {
	Image           bool `json:"image"`
	Audio           bool `json:"audio"`
	EmbeddedContext bool `json:"embeddedContext"`
}

type SessionCapabilities struct {
	Resume                *struct{} `json:"resume,omitempty"`
	Close                 *struct{} `json:"close,omitempty"`
	Load                  *struct{} `json:"load,omitempty"`
	List                  *struct{} `json:"list,omitempty"`
	AdditionalDirectories *struct{} `json:"additionalDirectories,omitempty"`
}

type MCPCapabilities struct {
	HTTP bool `json:"http,omitempty"`
}

type AgentAuthCapabilities struct {
	Logout *struct{} `json:"logout,omitempty"`
}

type AntigravitySystemPromptCap struct {
	Append bool `json:"append"`
}

type AntigravityCapabilitiesMeta struct {
	Tier         bool                        `json:"tier"`
	SystemPrompt *AntigravitySystemPromptCap `json:"systemPrompt,omitempty"`
}

type AgentCapabilitiesMeta struct {
	Antigravity *AntigravityCapabilitiesMeta `json:"antigravity,omitempty"`
}

type AgentCapabilities struct {
	LoadSession              bool                   `json:"loadSession"`
	LoadSessionSnake         bool                   `json:"load_session"`
	PromptCapabilities       *PromptCapabilities    `json:"promptCapabilities,omitempty"`
	SessionCapabilities      *SessionCapabilities   `json:"sessionCapabilities,omitempty"`
	SessionCapabilitiesSnake *SessionCapabilities   `json:"session_capabilities,omitempty"`
	MCPCapabilities          *MCPCapabilities       `json:"mcpCapabilities,omitempty"`
	Auth                     *AgentAuthCapabilities `json:"auth,omitempty"`
	Meta                     *AgentCapabilitiesMeta `json:"_meta,omitempty"`
}

type ClientAuthCapabilities struct {
	Terminal bool           `json:"terminal,omitempty"`
	Meta     map[string]any `json:"_meta,omitempty"`
}

type ClientCapabilities struct {
	Auth *ClientAuthCapabilities `json:"auth,omitempty"`
}

type InitializeParams struct {
	ProtocolVersion         int                 `json:"protocolVersion"`
	ClientInfo              json.RawMessage     `json:"clientInfo,omitempty"`
	ClientInfoSnake         json.RawMessage     `json:"client_info,omitempty"`
	ClientCapabilities      *ClientCapabilities `json:"clientCapabilities,omitempty"`
	ClientCapabilitiesSnake *ClientCapabilities `json:"client_capabilities,omitempty"`
}

type InitializeResult struct {
	ProtocolVersion   int               `json:"protocolVersion"`
	AgentCapabilities AgentCapabilities `json:"agentCapabilities"`
	AgentInfo         Implementation    `json:"agentInfo"`
	AuthMethods       []AuthMethod      `json:"authMethods"`
}

// BuildInitializeResult mirrors agy acp schema.BuildInitializeResult with ACP protocol v1 caps.
func BuildInitializeResult(version string, authMethods []AuthMethod) InitializeResult {
	if version == "" {
		version = "0.2.0"
	}
	sessionCaps := &SessionCapabilities{
		Resume:                &struct{}{},
		Close:                 &struct{}{},
		Load:                  &struct{}{},
		List:                  &struct{}{},
		AdditionalDirectories: &struct{}{},
	}
	if authMethods == nil {
		authMethods = DefaultAuthMethods()
	}
	return InitializeResult{
		ProtocolVersion: 1,
		AgentCapabilities: AgentCapabilities{
			LoadSession:              true,
			LoadSessionSnake:         true,
			PromptCapabilities:       &PromptCapabilities{Image: true, EmbeddedContext: true},
			SessionCapabilities:      sessionCaps,
			SessionCapabilitiesSnake: sessionCaps,
			Auth:                     &AgentAuthCapabilities{Logout: &struct{}{}},
			Meta: &AgentCapabilitiesMeta{
				Antigravity: &AntigravityCapabilitiesMeta{
					Tier:         true,
					SystemPrompt: &AntigravitySystemPromptCap{Append: true},
				},
			},
		},
		AgentInfo: Implementation{
			Name:    "Antigravity ACP Server",
			Version: version,
		},
		AuthMethods: authMethods,
	}
}

func DefaultAuthMethods() []AuthMethod {
	return []AuthMethod{
		{ID: "google-login", Name: "Sign in with Google", Description: "Consumer Google OAuth PKCE flow"},
		{ID: "gemini-api-key", Name: "Gemini API Key", Description: "GEMINI_API_KEY direct authentication"},
		{ID: "adc", Name: "Google Application Default Credentials", Description: "Service Account / WIF / ADC"},
	}
}

type AuthenticateParams struct {
	MethodID string `json:"methodId"`
}

type AuthenticateResult struct {
	Authenticated bool   `json:"authenticated"`
	MethodID      string `json:"methodId"`
	AuthKind      string `json:"authKind,omitempty"`
	Email         string `json:"email,omitempty"`
}

type ModelInfo struct {
	ModelID     string `json:"modelId"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type SessionModelState struct {
	CurrentModelID  string      `json:"currentModelId"`
	AvailableModels []ModelInfo `json:"availableModels"`
}

type ModeInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type SessionModeState struct {
	CurrentModeID  string     `json:"currentModeId"`
	AvailableModes []ModeInfo `json:"availableModes"`
}

type ConfigOptionValue struct {
	Value       string `json:"value"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type ConfigOption struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Description  string              `json:"description,omitempty"`
	Category     string              `json:"category,omitempty"`
	Type         string              `json:"type"`
	CurrentValue string              `json:"currentValue"`
	Options      []ConfigOptionValue `json:"options,omitempty"`
}

type NewSessionParams struct {
	Cwd                   string   `json:"cwd"`
	AdditionalDirectories []string `json:"additional_directories,omitempty"`
	AdditionalDirsCamel   []string `json:"additionalDirectories,omitempty"`
}

type NewSessionResult struct {
	SessionID     string             `json:"sessionId"`
	Models        *SessionModelState `json:"models,omitempty"`
	Modes         *SessionModeState  `json:"modes,omitempty"`
	ConfigOptions []ConfigOption     `json:"configOptions,omitempty"`
	Meta          map[string]any     `json:"_meta,omitempty"`
}

type LoadSessionParams struct {
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd,omitempty"`
}

type SessionInfo struct {
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	Title     string `json:"title,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

type ListSessionsResult struct {
	Sessions []SessionInfo `json:"sessions"`
}

type SetModelParams struct {
	SessionID string `json:"sessionId"`
	ModelID   string `json:"modelId"`
}

type SetModeParams struct {
	SessionID string `json:"sessionId"`
	ModeID    string `json:"modeId"`
}

type CancelSessionParams struct {
	SessionID string `json:"sessionId"`
}

type CloseSessionParams struct {
	SessionID string `json:"sessionId"`
}

type PromptParams struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
	MessageID string         `json:"messageId,omitempty"`
}

type PromptResult struct {
	StopReason string `json:"stopReason"`
	Usage      *Usage `json:"usage,omitempty"`
}
