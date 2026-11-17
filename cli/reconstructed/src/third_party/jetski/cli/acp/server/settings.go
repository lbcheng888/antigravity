// Binary VA: 0x102f92c50 | Source: third_party/jetski/cli/acp/server/settings.go
package server

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"os"

	"google3/third_party/jetski/cli/backend/backend"
	"google3/third_party/jetski/cli/types/types"
)

// Binary VA: 0x10a62d680 | Size: 24 B | Source: third_party/jetski/cli/acp/server/settings.go
type cliSettings struct {
	ToolPermission               string `json:"toolPermission"`
	EnableTerminalSandbox        bool   `json:"enableTerminalSandbox"`
	AllowNonWorkspaceAccess      bool   `json:"allowNonWorkspaceAccess"`
	UseG1Credits                 bool   `json:"useG1Credits"`
	IncludeDefaultCustomizations bool   `json:"includeDefaultCustomizations"`
}

// Binary VA: 0x102f92c50 | Size: 640 B | Source: third_party/jetski/cli/acp/server/settings.go:L28
func loadSettings(s *Server /* inferred */) (cliSettings, error) {
	cfg := cliSettings{
		ToolPermission:               "request-review",
		IncludeDefaultCustomizations: true,
	}
	data, err := os.ReadFile(s.settingsPath().DisplayString()) /* inferred */
	if err != nil {
		return cfg, nil
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cliSettings{
			ToolPermission:               "request-review",
			IncludeDefaultCustomizations: true,
		}, fmt.Errorf("malformed settings json: %w", err)
	}
	return cfg, nil
}

// Binary VA: 0x102f92ed0 | Size: 128 B | Source: third_party/jetski/cli/acp/server/settings.go:L45
func (s *Server) bindCLISettings(b *backend.ServerBackend) {
	if b == nil {
		return
	}
	s.cliSettings.Set(providerSettings{})
	b.BindInMemoryCLISettingsProvider(&s.cliSettings)
}

// Binary VA: 0x102f92f50 | Size: 784 B | Source: third_party/jetski/cli/acp/server/settings.go:L55
func (s *Server) applySettings(b *backend.ServerBackend, cfg cliSettings) {
	perm := cfg.ToolPermission
	if s.params.DangerouslySkipPermissions {
		perm = "always-proceed"
	}
	enableSandbox := s.params.EnableTerminalSandbox || cfg.EnableTerminalSandbox
	var mode types.PermissionMode
	switch perm {
	case "strict":
		mode = types.PermissionModeStrict
	case "always-proceed":
		mode = types.PermissionModeAlwaysProceed
	case "proceed-in-sandbox":
		mode = types.PermissionModeProceedInSandbox
	default:
		mode = types.PermissionModeRequestReview
	}
	b.SetUseG1Credits(cfg.UseG1Credits) /* inlined interface call */
	s.cliSettings.Set(providerSettings{
		PermissionMode:          mode,
		AllowNonWorkspaceAccess: cfg.AllowNonWorkspaceAccess,
		EnableTerminalSandbox:   enableSandbox,
	})
	includeDefault := cfg.IncludeDefaultCustomizations
	if s.params.IncludeDefaultCustomizations != nil {
		includeDefault = *s.params.IncludeDefaultCustomizations
	}
	if p, ok := any(b).(backend.IncludeDefaultCustomizationsProvider); ok {
		p.SetIncludeDefaultCustomizations(includeDefault)
	}
}
