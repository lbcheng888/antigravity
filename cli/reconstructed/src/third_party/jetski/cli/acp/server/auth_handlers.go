// Reconstructed from agy v1.5.10 binary evidence (disassembly + Go type table).
// Binary VA: 0x102f7da10 | Size: 6 funcs (first func (*Server).handleInitialize, 1120 B) | Source: third_party/jetski/cli/acp/server/auth_handlers.go:L34
package server

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"

	"google3/base/go/log"
	"google3/third_party/jetski/cli/acp/auth"
	"google3/third_party/jetski/cli/acp/jsonrpc"
	"google3/third_party/jetski/cli/acp/schema"
	"google3/third_party/jetski/cli/acp/session"
	"google3/third_party/jetski/cli/analytics"
)

// handleInitialize negotiates ACP protocol capabilities, client info, and advertised auth methods.
// Binary VA: 0x102f7da10 | Size: 1120 B | Source: third_party/jetski/cli/acp/server/auth_handlers.go:L34
func (s *Server) handleInitialize(req *jsonrpc.Request) {
	var params schema.InitializeParams // L35
	if !s.parseParams(req, &params) {  // L36
		return
	}

	clientInfo := params.ParsedClientInfo()                 // L40
	support := auth.ClientSupportFrom(params)               // L41
	elicitationForm := params.SupportsElicitationForm()     // L42

	s.mu.Lock()
	s.clientInfo = clientInfo
	s.terminalAuthCap = support.TerminalAuth
	s.elicitationForm = elicitationForm
	s.pendingClientConnected = &analytics.ACPClientConnected{ /* L48 */ }
	s.mu.Unlock()

	methods := auth.AdvertisedMethods(support, s.params.IsGoogleEnvironment, s.params.UseStubbyAuth) // L61
	res := schema.BuildInitializeResult(params.ProtocolVersion, methods, s.params.Version)           // L57
	if b, err := jsonv2.Marshal(res); err == nil {                                                   // L63
		log.Infof(s.requestContext(), "Sending initialize response: %s", string(b)) // L64
	}
	s.conn.SendResponse(req.ID, res, nil) // L66
	s.recordPendingClientConnected()      // L67
}

// requireAuth boots the backend (if needed) and checks EnsureAuthenticated,
// sending an auth_required JSON-RPC error on failure.
// Binary VA: 0x102f7de70 | Size: 784 B | Source: third_party/jetski/cli/acp/server/auth_handlers.go:L70
func (s *Server) requireAuth(ctx context.Context, req *jsonrpc.Request) bool {
	if err := s.ensureBackendWithOptions(ctx, bootOptions{}); err != nil { // boot.go:37
		s.conn.SendResponse(req.ID, nil, &jsonrpc.Error{
			Code:    -32603, /* inferred */
			Message: fmt.Sprintf("Failed to start %s: %v", s.params.Agent, err), // L72
		})
		return false
	}
	s.mu.Lock()
	am := s.auth
	s.mu.Unlock()
	if am == nil || !am.EnsureAuthenticated(ctx) { // L78
		s.conn.SendResponse(req.ID, nil, &jsonrpc.Error{Code: -32000 /* auth_required */, Message: "auth_required" /* inferred */}) // L81
		return false
	}
	return true
}

// tierMeta returns the authenticated user's tier metadata from s.auth.
// Binary VA: 0x102f7e180 | Size: 320 B | Source: third_party/jetski/cli/acp/server/auth_handlers.go:L85
func (s *Server) tierMeta() map[string]any {
	s.mu.Lock()
	am := s.auth
	s.mu.Unlock()
	if am == nil {
		return nil
	}
	return am.TierMeta() // L92
}

// handleAuthenticate runs `authenticate` (possibly rebinding backend mode),
// supports $/cancel_request via pendingAuth, and returns AuthenticateResult with _meta.
// Binary VA: 0x102f7e2c0 | Size: 2432 B | Source: third_party/jetski/cli/acp/server/auth_handlers.go:L99
func (s *Server) handleAuthenticate(req *jsonrpc.Request) {
	var params schema.AuthenticateParams // L100
	if !s.parseParams(req, &params) {    // L101
		return
	}

	ctx, cancel := context.WithCancel(s.requestContext()) // L106
	defer cancel()

	var id string
	if req.ID != nil {
		id = jsonrpc.NormalizeID(*req.ID) // L110
	}
	s.mu.Lock()
	s.pendingAuth = &pendingAuthRequest{id: id, cancel: cancel} // L108
	terminalAuth := s.terminalAuthCap
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.pendingAuth != nil && s.pendingAuth.id == id {
			s.pendingAuth = nil
		}
		s.mu.Unlock()
	}()

	sel, err := s.resolveAuthBackendSelection(params) // L121
	if err != nil {
		s.conn.SendResponse(req.ID, nil, &jsonrpc.Error{Code: -32603, Message: err.Error()})
		return
	}
	if err := s.maybeRebindBackendBeforeAuth(ctx, sel); err != nil { // L125
		s.conn.SendResponse(req.ID, nil, &jsonrpc.Error{
			Code:    -32603,
			Message: fmt.Sprintf("Failed to start %s: %v", s.params.Agent, err), // L126
		})
		return
	}
	if err := s.ensureBackendWithOptions(ctx, bootOptions{skipSilentAuth: true}); err != nil { // boot.go:37
		s.conn.SendResponse(req.ID, nil, &jsonrpc.Error{
			Code:    -32603,
			Message: fmt.Sprintf("Failed to start %s: %v", s.params.Agent, err), // L130
		})
		return
	}

	s.mu.Lock()
	am := s.auth
	s.mu.Unlock()
	if am == nil {
		s.conn.SendResponse(req.ID, nil, &jsonrpc.Error{Code: -32603, Message: "auth manager not initialized" /* inferred */}) // L138
		return
	}

	if rpcErr := am.Authenticate(ctx, params, terminalAuth); rpcErr != nil { // L141
		s.recordAuthenticated(params.MethodID, false) // L142
		log.Warningf(ctx, "authenticate(%q) failed: %s (code %d)", params.MethodID, rpcErr.Message, rpcErr.Code) // L144
		s.conn.SendResponse(req.ID, nil, rpcErr)
		return
	}

	s.recordAuthenticated(params.MethodID, true) // L142
	res := schema.AuthenticateResult{            // L149
		Meta: s.tierMeta(),
	}
	s.conn.SendResponse(req.ID, res, nil)
}

// handleLogout signs out of the current backend and cancels all active sessions.
// Binary VA: 0x102f7ec40 | Size: 1088 B | Source: third_party/jetski/cli/acp/server/auth_handlers.go:L155
func (s *Server) handleLogout(req *jsonrpc.Request) {
	ctx := s.requestContext()
	if err := s.ensureBackendWithOptions(ctx, bootOptions{}); err != nil { // boot.go:37
		s.conn.SendResponse(req.ID, nil, &jsonrpc.Error{
			Code:    -32603,
			Message: fmt.Sprintf("Failed to start %s: %v", s.params.Agent, err), // L157
		})
		return
	}

	s.mu.Lock()
	am := s.auth
	s.mu.Unlock()
	if am != nil {
		if err := am.Logout(ctx); err != nil { // L165
			s.conn.SendResponse(req.ID, nil, &jsonrpc.Error{
				Code:    -32603,
				Message: fmt.Sprintf("Couldn't sign out: %v", err), // L169
			})
			return
		}
	}
	s.cancelAllSessions(ctx)              // L167
	s.conn.SendResponse(req.ID, struct{}{}, nil)
}

// cancelAllSessions closes and clears all active sessionStates and stops their conversation trees.
// Binary VA: 0x102f7f080 | Size: 1664 B | Source: third_party/jetski/cli/acp/server/auth_handlers.go:L175
func (s *Server) cancelAllSessions(ctx context.Context) {
	s.mu.Lock()
	ids := make([]string, 0, len(s.sessionStates))      // L177
	states := make([]*session.State, 0, len(s.sessionStates))
	for id, st := range s.sessionStates { // L179
		ids = append(ids, id) // L180
		st.BeginClose()       // L186
		states = append(states, st) // L189/L191
	}
	clear(s.sessionStates) // L194
	b := s.backend
	s.mu.Unlock()

	for _, st := range states {
		st.CloseStream() // L200
		if turn := st.FinishTurnAfter(0); turn != nil { // L203
			// cancels turn
		}
	}
	for _, id := range ids {
		if err := stopConversationTree(ctx, b, id); err != nil { // L210
			log.Warningf(ctx, "Failed to cancel session %s on logout: %v", id, err) // L211
		}
	}
}
