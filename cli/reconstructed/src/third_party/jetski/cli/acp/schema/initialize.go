// Reconstructed from agy v1.5.10 binary evidence (disassembly + Go type table).
// Binary VA: 0x10232a0c0 | Size: 1 funcs (first func BuildInitializeResult, 752 B) | Source: third_party/jetski/cli/acp/schema/initialize.go:L24
// Reconstructed from binary: /Users/mac/.local/bin/agy (v1.5.10)
// Source: third_party/jetski/cli/acp/schema/initialize.go

package schema

import "cmp"

// Binary VA: 0x10232a0c0 | Size: 752 B | Source: third_party/jetski/cli/acp/schema/initialize.go:L24
func BuildInitializeResult(isJetski bool, version string, httpMCP bool, authMethods []AuthMethod) InitializeResult {
	sessionCaps := &SessionCapabilities{
		Resume:                &SessionResumeCapabilities{},
		Close:                 &SessionCloseCapabilities{},
		Load:                  &SessionLoadCapabilities{},
		List:                  &SessionListCapabilities{},
		AdditionalDirectories: &SessionAdditionalDirectoriesCapabilities{},
	}
	var mcpCaps *MCPCapabilities
	if httpMCP {
		mcpCaps = &MCPCapabilities{HTTP: true}
	}
	serverName := "Antigravity ACP Server"
	if isJetski {
		serverName = "Jetski ACP Server"
	}
	return InitializeResult{
		ProtocolVersion: 1,
		AgentCapabilities: AgentCapabilities{
			LoadSession:              true,
			LoadSessionSnake:         true,
			PromptCapabilities:       &PromptCapabilities{Image: true, EmbeddedContext: true},
			SessionCapabilities:      sessionCaps,
			SessionCapabilitiesSnake: sessionCaps,
			MCPCapabilities:          mcpCaps,
			MCPCapabilitiesSnake:     mcpCaps,
			Auth:                     &AgentAuthCapabilities{Logout: &struct{}{}},
			Meta: &AgentCapabilitiesMeta{
				Antigravity: &AntigravityCapabilitiesMeta{
					Tier:         true,
					SystemPrompt: &AntigravitySystemPromptCap{Append: true},
				},
			},
		},
		AgentInfo: Implementation{
			Name:    serverName,
			Version: cmp.Or(version, "0.2.0"),
		},
		AuthMethods: authMethods,
	}
}
