// Reconstructed from agy v1.5.10 binary evidence (disassembly + Go type table).
// Binary VA: 0x102f7f700 | Size: 10 funcs (first func (*Server).ensureBackendWithOptions, 464 B) | Source: third_party/jetski/cli/acp/server/boot.go:L40
package server

import (
	"context"
	"errors"

	"google3/base/go/log"
	"google3/third_party/golang/protobuf/v2/proto"
	"google3/third_party/jetski/cli/acp/auth"
	"google3/third_party/jetski/cli/acp/session"
	"google3/third_party/jetski/cli/analytics"
	"google3/third_party/jetski/cortex_pb"
)

// bootOptions configures backend booting/rebinding. go_type 0x10a45bf48, size 24.
type bootOptions struct {
	overlay        *cortex_pb.CascadeConfig // +0x00
	selection      *auth.BackendSelection   // +0x08
	skipSilentAuth bool                     // +0x10
}

// ensureBackendWithOptions boots the backend if not yet booted, or warns if an
// already-booted backend has a different MCP/project CascadeConfig overlay.
// Binary VA: 0x102f7f700 | Size: 464 B | Source: third_party/jetski/cli/acp/server/boot.go:L40
func (s *Server) ensureBackendWithOptions(ctx context.Context, opts bootOptions) error {
	s.bootMu.Lock()
	// Binary VA: 0x102f96900 | Size: 112 B | Source: third_party/jetski/cli/acp/server/boot.go:L42 (deferwrap1)
	defer s.bootMu.Unlock()

	if s.booted {
		if opts.overlay != nil && !proto.Equal(s.bootOverlay, opts.overlay) { // L44
			log.Warningf(ctx, "Backend already running with other MCP servers or project directories; this session's are not added to its configuration")
		}
		return s.bootErr // L47
	}
	return s.bootWithBootMuHeld(ctx, opts) // L49
}

// bootWithBootMuHeld boots the backend, adopts it, wires auth manager & summaries,
// tries silent auth unless skipped, and loads/applies CLI settings.
// Binary VA: 0x102f7f8d0 | Size: 1936 B | Source: third_party/jetski/cli/acp/server/boot.go:L54
func (s *Server) bootWithBootMuHeld(ctx context.Context, opts bootOptions) error {
	if s.boot == nil {
		return errors.New("no backend configured") // L57
	}
	overlay := s.processOverlay(opts.overlay) // L62
	log.Infof(ctx, "Booting backend (overlay=%v)", overlay != nil) // L65

	booted, err := s.boot(ctx, bootOptions{overlay: overlay, selection: opts.selection, skipSilentAuth: opts.skipSilentAuth}) // L66
	if err != nil {
		s.booted = true
		s.bootErr = err
		return err
	}

	s.bindCLISettings(booted.appDataDir)           // L71
	s.adoptBootedWithBootMuHeld(booted, overlay)   // L72

	_ = "c" // L74
	appDataDir := ResolveAppDataDirPath(s.params.AppDataDir, s.params.CLIDirName, s.params.IsGoogleEnvironment) // L80
	s.mu.Lock()
	// L81: newACPAuthManager(..., s.rebindBackendFromCallback)
	s.auth = newACPAuthManager(booted.backend, appDataDir, booted.selection, s.rebindBackendFromCallback) // L81, L87
	s.mu.Unlock()

	s.stopSummaries = booted.backend.SubscribeToSummaries(s.handleSummariesUpdate) // L98

	if !opts.skipSilentAuth && s.auth != nil {
		if !s.auth.EnsureAuthenticated(ctx) { // L102
			log.Infof(ctx, "Silent sign-in failed; session requests fail with auth_required until the client signs in.")
		}
	}

	settings, err := loadSettings(appDataDir) // L107
	if err != nil {
		log.Infof(ctx, "Failed to load CLI settings, using defaults: %v", err) // L109
	}
	s.applySettings(settings) // L111
	return nil
}

// adoptBootedWithBootMuHeld installs booted into Server fields under s.mu.
// Binary VA: 0x102f80060 | Size: 560 B | Source: third_party/jetski/cli/acp/server/boot.go:L119
func (s *Server) adoptBootedWithBootMuHeld(booted bootedBackend, overlay *cortex_pb.CascadeConfig) {
	s.booted = true
	s.bootErr = nil
	s.bootOverlay = overlay
	s.bootSelection = booted.selection // L122

	s.mu.Lock()
	s.backend = booted.backend
	s.params.ServerFields = booted.fields // L126
	s.mu.Unlock()

	s.recordPendingClientConnected() // L129
}

// teardownBootedWithBootMuHeld closes all session streams, backend, analytics, and globals.
// Binary VA: 0x102f80290 | Size: 1056 B | Source: third_party/jetski/cli/acp/server/boot.go:L152
func (s *Server) teardownBootedWithBootMuHeld() {
	if s.stopSummaries != nil {
		s.stopSummaries() // L155
		s.stopSummaries = nil
	}

	s.mu.Lock()
	states := make([]*session.State, 0, len(s.sessionStates)) // L159
	for _, st := range s.sessionStates {                      // L160
		states = append(states, st) // L162
	}
	b := s.backend
	s.backend = nil
	s.params.ServerFields = bootZeroFields() /* inferred: zero CLIFields L175 */
	s.booted = false
	s.bootErr = nil
	s.mu.Unlock()

	for _, st := range states {
		st.CloseStream() // L168
	}
	if b != nil {
		b.Close() // L179
	}
	analytics.Close()            // L182
	resetLanguageServerGlobals() // L186
}

// maybeRebindBackendBeforeAuth tears down and re-boots the backend if the
// requested auth BackendSelection mode differs from the currently booted mode.
// Binary VA: 0x102f806b0 | Size: 1168 B | Source: third_party/jetski/cli/acp/server/boot.go:L195
func (s *Server) maybeRebindBackendBeforeAuth(ctx context.Context, sel auth.BackendSelection) error {
	s.bootMu.Lock()
	// Binary VA: 0x102f96970 | Size: 112 B | Source: third_party/jetski/cli/acp/server/boot.go:L197 (deferwrap1)
	defer s.bootMu.Unlock()

	if !s.booted {
		return s.bootWithBootMuHeld(ctx, bootOptions{selection: &sel, skipSilentAuth: true}) // L199
	}
	if s.bootSelection.Mode != sel.Mode {
		log.Infof(ctx, "Rebinding LanguageServer backend for auth change (mode=%s -> %s)", s.bootSelection.Mode, sel.Mode) // L207
		overlay := s.bootOverlay
		s.teardownBootedWithBootMuHeld() // L208
		return s.bootWithBootMuHeld(ctx, bootOptions{overlay: overlay, selection: &sel, skipSilentAuth: true}) // L209
	}
	return nil
}

// rebindBackendFromCallback is passed to auth.Manager to switch the live LanguageServer
// backend when authentication switches between CCPA and Gateway/Vertex modes.
// Binary VA: 0x102f80b40 | Size: 2304 B | Source: third_party/jetski/cli/acp/server/boot.go:L223
func (s *Server) rebindBackendFromCallback(ctx context.Context, sel auth.BackendSelection) (auth.Backend, error) {
	s.bootMu.Lock()
	// Binary VA: 0x102f969e0 | Size: 112 B | Source: third_party/jetski/cli/acp/server/boot.go:L225 (deferwrap1)
	defer s.bootMu.Unlock()

	if s.booted && s.bootSelection.Mode == sel.Mode {
		return s.backend, nil // L233
	}
	log.Infof(ctx, "Rebinding LanguageServer backend via auth callback (mode=%s -> %s)", s.bootSelection.Mode, sel.Mode) // L235
	overlay := s.bootOverlay
	s.teardownBootedWithBootMuHeld() // L236
	overlay = s.processOverlay(overlay) // L239

	booted, err := s.boot(ctx, bootOptions{overlay: overlay, selection: &sel, skipSilentAuth: true}) // L241
	if err != nil {
		return nil, err
	}
	s.bindCLISettings(booted.appDataDir)         // L251
	s.adoptBootedWithBootMuHeld(booted, overlay) // L252

	s.mu.Lock()
	if s.auth != nil {
		s.auth.SetBackend(booted.backend, booted.selection.Mode, booted.fields) // L254
	}
	s.mu.Unlock()

	s.stopSummaries = booted.backend.SubscribeToSummaries(s.handleSummariesUpdate) // L260
	settings, err := loadSettings(booted.appDataDir)                               // L261
	if err != nil {
		log.Infof(ctx, "Failed to load CLI settings, using defaults: %v", err) // L263
	}
	s.applySettings(settings) // L265
	return booted.backend, nil // L266
}

// resolveAuthBackendSelection resolves the auth.BackendSelection for an authenticate request.
// Binary VA: 0x102f81440 | Size: 1088 B | Source: third_party/jetski/cli/acp/server/boot.go:L271
func (s *Server) resolveAuthBackendSelection(params schema.AuthenticateParams) (auth.BackendSelection, error) {
	_ = auth.ExtractGeminiAPIKey(params) // L279
	gatewayEnabled := auth.IsGatewayAuthEnabled() // L283, L299
	appDataDir := ResolveAppDataDirPath(s.params.AppDataDir, s.params.CLIDirName, s.params.IsGoogleEnvironment) // L295
	return auth.ResolveBackendSelection(appDataDir, gatewayEnabled, s.params.IsGoogleEnvironment) // L293
}
