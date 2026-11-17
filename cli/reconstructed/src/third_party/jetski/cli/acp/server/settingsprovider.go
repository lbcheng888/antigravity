// Binary VA: 0x102f93260 | Source: third_party/jetski/cli/acp/server/settingsprovider.go
package server

import (
	"context"
	"errors"
	"sync"

	"google3/third_party/jetski/cli/backend/backend"
	"google3/third_party/jetski/cli/settings/settings"
	"google3/third_party/jetski/cli/types/types"
	"google3/third_party/jetski/fs/fs"
	"google3/third_party/jetski/proto/codeium_common_go_proto"
)

// Binary VA: 0x10a45bff0 | Size: 24 B | Source: third_party/jetski/cli/acp/server/settingsprovider.go
type providerSettings struct {
	PermissionMode          types.PermissionMode
	AllowNonWorkspaceAccess bool
	EnableTerminalSandbox   bool
}

// Binary VA: 0x10a3070c0 | Size: 32 B | Source: third_party/jetski/cli/acp/server/settingsprovider.go
type settingsProvider struct {
	mu       sync.Mutex
	settings providerSettings
}

var errUnsupportedOperation = errors.New("unsupported operation")

// Binary VA: 0x102f93260 | Size: 304 B | Source: third_party/jetski/cli/acp/server/settingsprovider.go:L49
func (p *settingsProvider) Set(s providerSettings) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.settings = s
}

// Binary VA: 0x102f93390 | Size: 272 B | Source: third_party/jetski/cli/acp/server/settingsprovider.go:L55
func (p *settingsProvider) get() providerSettings {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.settings
}

// Binary VA: 0x102f934a0 | Size: 64 B | Source: third_party/jetski/cli/acp/server/settingsprovider.go:L65
func (p *settingsProvider) EffectivePermissionMode() types.PermissionMode {
	return p.get().PermissionMode
}

// Binary VA: 0x102f934e0 | Size: 16 B | Source: third_party/jetski/cli/acp/server/settingsprovider.go:L72
func (p *settingsProvider) GetGlobalPermissionGrants() *codeium_common_go_proto.PermissionGrantsConfig {
	return nil
}

// Binary VA: 0x102f934f0 | Size: 16 B | Source: third_party/jetski/cli/acp/server/settingsprovider.go:L79
func (p *settingsProvider) AddGlobalPermissionGrants(_ context.Context, _ *codeium_common_go_proto.PermissionGrantsConfig) error {
	return errUnsupportedOperation
}

// Binary VA: 0x102f93500 | Size: 64 B | Source: third_party/jetski/cli/acp/server/settingsprovider.go:L85
func (p *settingsProvider) GetAllowNonWorkspaceAccess() bool {
	return p.get().AllowNonWorkspaceAccess
}

// Binary VA: 0x102f93540 | Size: 64 B | Source: third_party/jetski/cli/acp/server/settingsprovider.go:L92
func (p *settingsProvider) GetEnableTerminalSandbox() bool {
	return p.get().EnableTerminalSandbox
}

// Binary VA: 0x102f93580 | Size: 16 B | Source: third_party/jetski/cli/acp/server/settingsprovider.go:L100
func (p *settingsProvider) GetSandboxMode() *bool {
	return nil
}

// Binary VA: 0x102f93590 | Size: 48 B | Source: third_party/jetski/cli/acp/server/settingsprovider.go:L106
func (p *settingsProvider) GetSandboxProxy() (string, string, string, string) {
	return "", "", "", ""
}

// Binary VA: 0x102f935c0 | Size: 16 B | Source: third_party/jetski/cli/acp/server/settingsprovider.go:L116
func (p *settingsProvider) GetAutoExecutionPolicy() *codeium_common_go_proto.CascadeCommandsAutoExecution {
	return nil
}

// Binary VA: 0x102f935d0 | Size: 16 B | Source: third_party/jetski/cli/acp/server/settingsprovider.go:L123
func (p *settingsProvider) GetArtifactReviewMode() *codeium_common_go_proto.ArtifactReviewMode {
	return nil
}

// Binary VA: 0x102f935e0 | Size: 256 B | Source: third_party/jetski/cli/acp/server/settingsprovider.go:L133
func (p *settingsProvider) PermissionGrants(workspaces []fs.Path /* inferred */) *settings.ClientPermissionGrants {
	s := p.get()
	return backend.PermissionGrantsFor(workspaces, s.PermissionMode == types.PermissionModeAlwaysProceed, s.PermissionMode == types.PermissionModeStrict, s.AllowNonWorkspaceAccess /* inferred arg order */)
}
