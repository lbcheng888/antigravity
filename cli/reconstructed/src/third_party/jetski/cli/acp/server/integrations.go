// Binary VA: 0x102f84950 | Source: third_party/jetski/cli/acp/server/integrations.go
package server

import (
	"context"

	"google3/third_party/jetski/cli/acp/auth/auth"
	"google3/third_party/jetski/cli/acp/jsonrpc/jsonrpc"
	"google3/third_party/jetski/cli/acp/schema/schema"
	"google3/third_party/jetski/cli/backend/backend"
	"google3/third_party/jetski/language_server/hostclientidentity/hostclientidentity"
	"google3/third_party/jetski/language_server/language_server"
)

// Binary VA: 0x10a1b1a20 | Size: 8 B | Source: third_party/jetski/cli/acp/server/integrations.go:L43
type acpAuthAdapter struct {
	mgr *auth.Manager
}

// Binary VA: 0x102f84950 | Size: 272 B | Source: third_party/jetski/cli/acp/server/integrations.go:L27
func withTurnContext(ctx context.Context, client schema.ClientInfo) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	a, b := client.SurfaceSegments()
	if id, ok := hostclientidentity.Parse(a + "/" + b); ok {
		return context.WithValue(ctx, hostclientidentity.ContextKey /* hostclientidentity.contextKey{} */, id)
	}
	return ctx
}

// Binary VA: 0x102f84a60 | Size: 432 B | Source: third_party/jetski/cli/acp/server/integrations.go:L61
func newACPAuthManager(b *backend.ServerBackend, params Config, rebind func(context.Context, auth.BackendSelection) (auth.Backend, error) /* inferred */) acpAuthManager {
	cliDir := params.CLIDirName
	if cliDir == "" && params.IsJetski {
		cliDir = "cli"
	}
	appName := ""
	if b != nil {
		appName = b.OAuthAppName() /* inferred from offset +0xc0 */
	}
	if appName == "" {
		if params.IsJetski {
			appName = "jetski"
		} else {
			appName = "antigravity"
		}
	}
	cfg := auth.Config{
		Backend:                     b,
		IsJetski:                    params.IsJetski,
		AppName:                     appName,
		Version:                     params.Version,
		CLIDirName:                  cliDir,
		AdminSettingsPath:           params.AdminSettingsPath,
		DisableDefaultAdminSettings: params.DisableDefaultAdminSettings,
		AppDataDir:                  params.AppDataDir,
		GeminiDir:                   params.GeminiDir,
		ServerFields:                params.ServerFields, /* inferred */
		RebindBackend:               rebind,              /* inferred */
	}
	return &acpAuthAdapter{mgr: auth.NewManager(cfg)}
}

// Binary VA: 0x102f84c10 | Size: 112 B | Source: third_party/jetski/cli/acp/server/integrations.go:L89
func (a *acpAuthAdapter) EnsureAuthenticated(ctx context.Context) bool {
	return a.mgr.EnsureAuthenticated(ctx)
}

// Binary VA: 0x102f84c80 | Size: 320 B | Source: third_party/jetski/cli/acp/server/integrations.go:L96
func (a *acpAuthAdapter) Authenticate(ctx context.Context, params schema.AuthenticateParams, interactive bool) *jsonrpc.Error {
	if err := a.mgr.Authenticate(ctx, params, interactive); err != nil {
		return auth.ToClientError(err)
	}
	return nil
}

// Binary VA: 0x102f84dc0 | Size: 112 B | Source: third_party/jetski/cli/acp/server/integrations.go:L111
func (a *acpAuthAdapter) Logout(ctx context.Context) error {
	return a.mgr.Logout(ctx)
}

// Binary VA: 0x102f84e30 | Size: 96 B | Source: third_party/jetski/cli/acp/server/integrations.go:L118
func (a *acpAuthAdapter) TierMeta() map[string]interface{} {
	return a.mgr.TierMeta()
}

// Binary VA: 0x102f84e90 | Size: 160 B | Source: third_party/jetski/cli/acp/server/integrations.go:L125
func (a *acpAuthAdapter) SetBackend(b *backend.ServerBackend, mode auth.BackendMode, fields language_server.CLIFields) {
	a.mgr.SetBackend(b, mode, fields)
}
