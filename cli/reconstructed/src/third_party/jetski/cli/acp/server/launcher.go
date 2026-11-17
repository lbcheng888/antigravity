// Reconstructed from agy v1.5.10 binary evidence (disassembly + Go type table).
// Binary VA: 0x102f84f30 | Size: 12 funcs (first func ResolveWorkingDir, 96 B) | Source: third_party/jetski/cli/acp/server/launcher.go:L75
package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google3/base/go/log"
	"google3/third_party/jetski/cli/acp/auth"
	"google3/third_party/jetski/cli/acp/mcp"
	"google3/third_party/jetski/cli/acp/schema"
	"google3/third_party/jetski/cli/backend"
	"google3/third_party/jetski/cortex/utils/authstore"
	"google3/third_party/jetski/cortex/utils/mcp" // mcp.McpManager
	"google3/third_party/jetski/cortex_pb"
	"google3/third_party/jetski/fs"
	"google3/third_party/jetski/fs/local"
	"google3/third_party/jetski/language_server"
)

// acpLanguageServer wraps backend.LanguageServerService to inject per-turn overlay/context.
// go_type 0x10aa17c70, size 24.
type acpLanguageServer struct {
	backend.LanguageServerService // +0x00
	prepareTurn                   func(context.Context, string, *cortex_pb.CascadeConfig) (context.Context, *cortex_pb.CascadeConfig) // +0x10
}

// bootedBackend holds the started backend and resolved metadata. go_type 0x10a62d5a8, size 288.
type bootedBackend struct {
	backend           *backend.ServerBackend    // +0x0000
	fields            language_server.CLIFields // +0x0008
	appDataDir        string                    // +0x00a8
	selection         auth.BackendSelection     // +0x00b8
	telemetryDisabled bool                      // +0x0118
}

// ResolveWorkingDir returns BUILD_WORKING_DIRECTORY or os.Getwd().
// Binary VA: 0x102f84f30 | Size: 96 B | Source: third_party/jetski/cli/acp/server/launcher.go:L75
func ResolveWorkingDir() (string, error) {
	if wd := os.Getenv("BUILD_WORKING_DIRECTORY"); wd != "" { // L76
		return wd, nil
	}
	return os.Getwd() // L79
}

// ResolveAppDataDirPath resolves the app data directory under ResolveLogDir.
// Binary VA: 0x102f84f90 | Size: 256 B | Source: third_party/jetski/cli/acp/server/launcher.go:L86
func ResolveAppDataDirPath(appDataDir, cliDirName string, isGoogleEnv bool) string {
	dir, err := ResolveLogDir(appDataDir, cliDirName, isGoogleEnv) // L93
	if err != nil {
		return ""
	}
	// [unrecovered] joins/normalizes ".gemini" or dir
	_ = ".gemini"
	return dir
}

// SendUserCascadeMessage applies prepareTurn before delegating to the embedded LanguageServerService.
// Binary VA: 0x102f85090 | Size: 192 B | Source: third_party/jetski/cli/acp/server/launcher.go:L109
func (a *acpLanguageServer) SendUserCascadeMessage(ctx context.Context, req any /* *connect.Request[...] */) (any, error) {
	if a.prepareTurn != nil { // L114
		// [unrecovered] calls a.prepareTurn(ctx, cascadeID, config)
	}
	return nil, nil // L118: delegates to a.LanguageServerService.SendUserCascadeMessage
}

// newBackendBooter returns a backendBooter closure bound to cfg and lsCfg.
// Binary VA: 0x102f96d50 | Size: 320 B | Source: third_party/jetski/cli/acp/server/launcher.go:L129
func newBackendBooter(cfg Config, lsCfg language_server.LanguageServerConfig) backendBooter {
	return func(ctx context.Context, opts bootOptions) (bootedBackend, error) {
		return bootBackend(ctx, cfg, lsCfg, opts) // L138
	}
}

// resetLanguageServerGlobals clears language_server/authstore global hooks on boot/teardown.
// Binary VA: 0x102f85150 | Size: 288 B | Source: third_party/jetski/cli/acp/server/launcher.go:L142
func resetLanguageServerGlobals() {
	// L142-145: clears global callbacks in local/utils
	authstore.SetUserIDResolver(nil) // L146
}

// bootBackend resolves auth backend selection, starts the embedded language server,
// registers workspace/add-dir folders, resolves app data/cache paths, wraps ServerBackend,
// and optionally preloads the IDE tools MCP server.
// Binary VA: 0x102f85270 | Size: 5184 B | Source: third_party/jetski/cli/acp/server/launcher.go:L149
func bootBackend(ctx context.Context, cfg Config, lsCfg language_server.LanguageServerConfig, opts bootOptions) (bootedBackend, error) {
	appDataDir := ResolveAppDataDirPath(cfg.AppDataDir, cfg.CLIDirName, cfg.IsGoogleEnvironment) // L161
	var sel auth.BackendSelection
	if opts.selection != nil {
		sel = *opts.selection
	} else {
		var err error
		sel, err = auth.ResolveBackendSelection(appDataDir, auth.IsGatewayAuthEnabled(), cfg.IsGoogleEnvironment) // L166, L172
		if err != nil {
			log.Warningf(ctx, "Failed to resolve pre-boot backend selection, defaulting to CCPA: %v", err) // L175
			sel.Mode = "ccpa"                                                                               // L176
		}
	}

	_ = io.Discard // L195
	// L199: SurfaceSegments from clientInfo + "cli" auth_provider
	_ = schema.ClientInfo.SurfaceSegments

	resetLanguageServerGlobals() // L202
	_ = local.NewFS()            // L203

	// L213-217: manage AGY_LLM_GATEWAY_URL env var around language server start
	prevGatewayURL, hadGatewayURL := os.LookupEnv("AGY_LLM_GATEWAY_URL") // L215
	_ = prevGatewayURL
	_ = hadGatewayURL
	// os.Setenv("AGY_LLM_GATEWAY_URL", ...) // L216
	// Binary VA: 0x102f99e10 | Size: 80 B | Source: third_party/jetski/cli/acp/server/launcher.go:L217 (bootBackend.func2)
	defer func() {
		os.Unsetenv("AGY_LLM_GATEWAY_URL") // L217
	}()

	lsCh := make(chan *language_server.Server, 1) // L223
	errCh := make(chan error, 1)                  // L224
	// Binary VA: 0x102f97000 | Size: 240 B | Source: third_party/jetski/cli/acp/server/launcher.go:L225 (bootBackend.func3)
	go func() {
		srv, err := language_server.CreateLanguageServerAndServe(ctx, lsCfg) // L226
		if err != nil {
			log.Infof(ctx, "Language server exited with error: %+v", err) // L227
			select {                                                      // L229
			case errCh <- err:
			default:
			}
			return
		}
		lsCh <- srv
	}()

	var ls *language_server.Server
	select { // L236
	case err := <-errCh: // L239
		return bootedBackend{}, fmt.Errorf("language server failed to start: %w", err) // L240
	case ls = <-lsCh: // L242
		if ls == nil {
			return bootedBackend{}, errors.New("language server did not start") // L244
		}
	}

	fields := ls.GetFieldsForCLI() // L246
	if cfg.WorkingDir != "" {
		wdPath, err := fs.AbsolutePath(cfg.WorkingDir) // L249
		if err != nil {
			log.Errorf(ctx, "Failed to resolve working directory path: %v", err) // L250
		} else if err := fields.WorkspaceManager.AddWorkspaceFolder(wdPath.Uri(), true); err != nil { // L251
			log.Errorf(ctx, "Failed to add workspace folder: %v", err) // L252
		}
	}
	for _, addDir := range cfg.AddDirs { // L257
		p, err := fs.AbsolutePath(addDir)
		if err != nil {
			log.Errorf(ctx, "Failed to resolve add-dir path %q: %v", addDir, err) // L259
			continue
		}
		if err := fields.WorkspaceManager.AddWorkspaceFolder(p.Uri(), true); err != nil { // L262
			log.Errorf(ctx, "Failed to add workspace folder %q: %v", addDir, err) // L263
		}
	}

	// L270-285: resolve GeminiDir -> appDataDir -> cache/default_project_id.txt
	geminiPath, err := fs.AbsolutePath(cfg.GeminiDir) // L270
	if err != nil {
		log.Errorf(ctx, "Failed to resolve GeminiDir %q: %v, falling back to default", cfg.GeminiDir, err) // L277
		geminiPath, err = fs.AbsolutePath(".gemini")                                                        // L275
		if err != nil {
			log.Errorf(ctx, "Failed to get default GeminiDir: %v, using hardcoded .gemini", err) // L279
		}
	}
	_ = geminiPath.Join(cfg.AppDataDir) // L283
	log.Infof(ctx, "ACP app data directory: %s (cliDir=%q, mode=%s)", geminiPath.DisplayString(), cfg.CLIDirName, sel.Mode) // L285

	projIDBytes, _ := os.ReadFile(geminiPath.Join("cache", "default_project_id.txt").DisplayString()) // L288
	projID := strings.TrimSpace(string(projIDBytes))                                                   // L290

	wrappedLS := &acpLanguageServer{ // L297
		LanguageServerService: ls, // L296/L298
	}
	sb, err := backend.NewServerBackend(ctx, lsCfg, wrappedLS) // L311/L326
	if err != nil {
		return bootedBackend{}, err
	}
	if projID != "" {
		sb.SetProjectID(projID) // L327
	}

	if cfg.IDEMCPPort > 0 {
		spec := mcp.IDEPortSpec(cfg.IDEMCPPort)                                  // L329
		log.Infof(ctx, "Preloading the ide-tools MCP server on port %d", cfg.IDEMCPPort) // L330
		// Binary VA: 0x102f96e90 | Size: 368 B | Source: third_party/jetski/cli/acp/server/launcher.go:L331 (bootBackend.func4)
		go func() {
			loadCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second /* inferred */) // L332
			defer cancel()                                                                             // L337
			if err := fields.MCPManager.LoadFromSpecs(loadCtx, []*cortex_pb.McpServerSpec{spec}); err != nil { // L334
				log.Warningf(loadCtx, "Failed to preload the ide-tools MCP server: %v", err) // L335
			}
		}()
	}

	return bootedBackend{ // L340
		backend:    sb,
		fields:     fields,
		appDataDir: appDataDir,
		selection:  sel,
	}, nil
}

// mergeTurnOverlay merges per-turn CascadeConfig into the base overlay.
// Binary VA: 0x102f866b0 | Size: 128 B | Source: third_party/jetski/cli/acp/server/launcher.go:L351
func mergeTurnOverlay(base, turn *cortex_pb.CascadeConfig) *cortex_pb.CascadeConfig {
	// L356, L358: config.MergeConfigsDedup
	return base
}

// ResolveLogDir returns the directory for ACP logs (~/.gemini/<appDataDir>/<cliDirName> or TMPDIR fallback).
// Binary VA: 0x102f86730 | Size: 384 B | Source: third_party/jetski/cli/acp/server/launcher.go:L364
func ResolveLogDir(appDataDir, cliDirName string, isGoogleEnv bool) (string, error) {
	home, err := os.UserHomeDir() // L369
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err) // L371
	}
	if appDataDir == "" {
		appDataDir = "antigravity-cli" /* inferred from --app-data-dir default */
	}
	dir := filepath.Join(home, ".gemini", appDataDir) // L371+
	if cliDirName != "" {
		dir = filepath.Join(dir, cliDirName)
	}
	_ = os.Getenv("TMPDIR") // file_unix.go:400 (os.TempDir fallback if needed)
	return dir, nil
}
