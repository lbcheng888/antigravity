// Reconstructed from agy v1.5.10 binary evidence (disassembly + Go type table).
// Binary VA: 0x102f8c810 | Size: 17 funcs (first func (*Server).currentClientInfo, 272 B) | Source: third_party/jetski/cli/acp/server/server.go:L92
package server

import (
	"context"
	jsonv2 "encoding/json/v2"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"google3/base/go/log"
	"google3/third_party/jetski/cli/acp/auth"
	"google3/third_party/jetski/cli/acp/jsonrpc"
	"google3/third_party/jetski/cli/acp/mcp"
	"google3/third_party/jetski/cli/acp/schema"
	"google3/third_party/jetski/cli/acp/session"
	"google3/third_party/jetski/cli/analytics"
	"google3/third_party/jetski/cli/backend"
	"google3/third_party/jetski/cortex_pb"
	"google3/third_party/jetski/language_server"
)

// Config holds ACP server configuration. go_type 0x10a99c178, size 344.
type Config struct {
	WorkingDir                   string                    // +0x0000
	AppDataDir                   string                    // +0x0010
	GeminiDir                    string                    // +0x0020
	CLIDirName                   string                    // +0x0030
	Version                      string                    // +0x0040
	Agent                        string                    // +0x0050
	IsGoogleEnvironment          bool                      // +0x0060
	IsJetski                     bool                      // +0x0061
	EnableTerminalSandbox        bool                      // +0x0062
	DangerouslySkipPermissions   bool                      // +0x0063
	IncludeDefaultCustomizations *bool                     // +0x0068
	AddDirs                      []string                  // +0x0070
	IDEMCPPort                   int                       // +0x0088
	CancelGrace                  time.Duration             // +0x0090
	AdminSettingsPath            string                    // +0x0098
	DisableDefaultAdminSettings  bool                      // +0x00a8
	MCPManager                   *mcp.McpManager           // +0x00b0
	ServerFields                 language_server.CLIFields // +0x00b8
}

type backendBooter func(context.Context, bootOptions) (bootedBackend, error) // go_type 0x10a109990

// acpAuthManager is the server's auth manager interface. go_type 0x10a2bd008.
type acpAuthManager interface {
	Authenticate(context.Context, schema.AuthenticateParams, bool) *jsonrpc.Error
	EnsureAuthenticated(context.Context) bool
	Logout(context.Context) error
	SetBackend(*backend.ServerBackend, auth.BackendMode, language_server.CLIFields)
	TierMeta() map[string]any
}

// pendingAuthRequest tracks an in-flight authenticate request for $/cancel_request. go_type 0x10a307030.
type pendingAuthRequest struct {
	id     string             // +0x00
	cancel context.CancelFunc // +0x10
}

// Server is the ACP JSON-RPC server. go_type 0x10aa070c8, size 728.
type Server struct {
	boot                   backendBooter                 // +0x0000
	backend                *backend.ServerBackend        // +0x0008
	auth                   acpAuthManager                // +0x0010
	clientInfo             schema.ClientInfo             // +0x0020
	terminalAuthCap        bool                          // +0x0040
	elicitationForm        bool                          // +0x0041
	pendingClientConnected *analytics.ACPClientConnected // +0x0048
	telemetryReady         atomic.Bool                   // +0x0050
	turnSource             func() turnBackend            // +0x0058
	params                 Config                        // +0x0060
	conn                   *jsonrpc.Conn                 // +0x01b8
	connCtx                context.Context               // +0x01c0
	sessionStates          map[string]*session.State     // +0x01d0
	catalog                sessionCatalog                // +0x01d8
	mu                     sync.Mutex                    // +0x01e8
	singleSession          bool                          // +0x01f0
	mcpNames               mcp.Names                     // +0x01f8
	onLastSessionClosed    func()                        // +0x0208
	pendingAuth            *pendingAuthRequest           // +0x0210
	bootMu                 sync.Mutex                    // +0x0218
	booted                 bool                          // +0x0220
	bootErr                error                         // +0x0228
	bootOverlay            *cortex_pb.CascadeConfig      // +0x0238
	bootSelection          auth.BackendSelection         // +0x0240
	stopSummaries          func()                        // +0x02a0
	shutdown               sync.Once                     // +0x02a8
	cliSettings            settingsProvider              // +0x02b8
}

// currentClientInfo returns the negotiated client info under s.mu.
// Binary VA: 0x102f8c810 | Size: 272 B | Source: third_party/jetski/cli/acp/server/server.go:L92
func (s *Server) currentClientInfo() schema.ClientInfo {
	s.mu.Lock()
	// Binary VA: 0x102f98ba0 | Size: 112 B | Source: third_party/jetski/cli/acp/server/server.go:L94 (deferwrap1)
	defer s.mu.Unlock()
	return s.clientInfo // L95
}

// requestContext returns the connection context if set, else context.Background().
// Binary VA: 0x102f8c920 | Size: 304 B | Source: third_party/jetski/cli/acp/server/server.go:L112
func (s *Server) requestContext() context.Context {
	s.mu.Lock()
	ctx := s.connCtx
	s.mu.Unlock()
	if ctx != nil {
		return ctx
	}
	return context.Background() // L119
}

// callClient sends an outbound JSON-RPC request to the connected client.
// Binary VA: 0x102f8ca50 | Size: 736 B | Source: third_party/jetski/cli/acp/server/server.go:L122
func (s *Server) callClient(ctx context.Context, method string, params any, result any) error {
	s.mu.Lock()
	conn := s.conn
	connCtx := s.connCtx
	s.mu.Unlock()
	if conn == nil { // L128
		return errors.New("ACP connection is not initialized")
	}
	if ctx == nil { // L130
		ctx = context.Background()
	}
	if connCtx != nil { // L133/L137
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx) // L140
		defer cancel()
		// Binary VA: 0x102f98c10 | Size: 80 B | Source: third_party/jetski/cli/acp/server/server.go:L142 (callClient.func1)
		stop := context.AfterFunc(connCtx, func() { // L141-142
			cancel() // L143-144
		})
		defer stop()
	}
	return conn.Call(ctx, method, params, result) // L148
}

// Shutdown tears down the booted backend once.
// Binary VA: 0x102f8cd30 | Size: 96 B | Source: third_party/jetski/cli/acp/server/server.go:L152
func (s *Server) Shutdown() {
	// Binary VA: 0x102f98c60 | Size: 256 B | Source: third_party/jetski/cli/acp/server/server.go:L153 (Shutdown.func1)
	s.shutdown.Do(func() {
		s.bootMu.Lock()
		// Binary VA: 0x102f98d60 | Size: 112 B | Source: third_party/jetski/cli/acp/server/server.go:L155 (Shutdown.func1.deferwrap1)
		defer s.bootMu.Unlock()
		s.teardownBootedWithBootMuHeld() // L159
	})
}

// Run runs the JSON-RPC loop with context.Background().
// Binary VA: 0x102f8cd90 | Size: 128 B | Source: third_party/jetski/cli/acp/server/server.go:L164
func (s *Server) Run(in io.Reader, out io.Writer) error {
	return s.runWithContext(context.Background(), in, out) // L165
}

// runWithContext creates the jsonrpc.Conn and runs its read loop until EOF/error.
// Binary VA: 0x102f8ce10 | Size: 704 B | Source: third_party/jetski/cli/acp/server/server.go:L168
func (s *Server) runWithContext(ctx context.Context, in io.Reader, out io.Writer) error {
	if ctx == nil { // L169
		ctx = context.Background()
	}
	conn := jsonrpc.NewConn(in, out, s.handleRequest) // L172
	connCtx, cancel := context.WithCancel(ctx)       // L173
	defer cancel()

	s.mu.Lock()
	s.conn = conn
	s.connCtx = connCtx
	s.mu.Unlock()

	// Binary VA: 0x102f98dd0 | Size: 320 B | Source: third_party/jetski/cli/acp/server/server.go:L181 (runWithContext.func1)
	defer func() {
		s.mu.Lock()
		if s.conn == conn { // L183
			s.conn = nil
			s.connCtx = nil
		}
		s.mu.Unlock()
	}()

	log.Infof(ctx, "Running ACP RPC loop...") // L189
	return conn.Run()                          // L190
}

// parseParams unmarshals req.Params into dst, sending a JSON-RPC -32602 error if invalid.
// Binary VA: 0x102f8d0d0 | Size: 496 B | Source: third_party/jetski/cli/acp/server/server.go:L193
func (s *Server) parseParams(req *jsonrpc.Request, dst any) bool {
	if req.Params == nil { // L195
		s.conn.SendResponse(req.ID, nil, &jsonrpc.Error{Code: -32602 /* inferred */, Message: "missing params" /* inferred */})
		return false
	}
	if err := jsonv2.Unmarshal(*req.Params, dst); err != nil {
		s.conn.SendResponse(req.ID, nil, &jsonrpc.Error{Code: -32602 /* inferred */, Message: err.Error()}) // L202
		return false
	}
	return true
}

// handleRequest dispatches an incoming JSON-RPC request/notification by method name.
// Binary VA: 0x102f8d2c0 | Size: 1552 B | Source: third_party/jetski/cli/acp/server/server.go:L211
func (s *Server) handleRequest(req *jsonrpc.Request) {
	ctx := s.requestContext()                            // L212
	log.Infof(ctx, "ACP Request received: %s", req.Method) // L213

	switch req.Method {
	case "initialize": // L216
		s.handleInitialize(req)
	case "authenticate": // L218
		s.handleAuthenticate(req)
	case "logout": // L220
		s.handleLogout(req)
	case "session/new": // L222
		s.handleSessionNew(req)
	case "session/load": // L224
		s.handleSessionLoad(req)
	case "session/resume": // L226
		s.handleSessionLoad(req)
	case "session/list": // L228
		s.handleSessionList(req)
	case "session/prompt": // L230
		s.handleSessionPrompt(req)
	case "session/cancel": // L232
		s.handleSessionCancel(req)
	case "session/close": // L234
		s.handleSessionClose(req)
	case "session/set_model": // L236
		s.handleSessionSetModel(req)
	case "session/set_config_option": // L238
		s.handleSessionSetConfigOption(req)
	case "$/cancel_request": // L240
		s.handleCancelRequest(req)
	case "session/set_mode": // L242
		s.handleSessionSetMode(req)
	default:
		if req.ID != nil {
			s.conn.SendResponse(req.ID, nil, &jsonrpc.Error{ // L248
				Code:    -32601, /* inferred */
				Message: fmt.Sprintf("Method not found: %s", req.Method), // L250
			})
		}
	}
}

// handleCancelRequest cancels a pending authenticate request matching requestId.
// Binary VA: 0x102f8d8d0 | Size: 432 B | Source: third_party/jetski/cli/acp/server/server.go:L257
func (s *Server) handleCancelRequest(req *jsonrpc.Request) {
	if req.Params == nil {
		return
	}
	var p struct { // L258 (go_type 0x109d163b0)
		RequestID jsontext.Value `json:"requestId"`
	}
	if err := jsonv2.Unmarshal(*req.Params, &p); err != nil {
		return
	}
	id := jsonrpc.NormalizeID(p.RequestID) // L264
	s.mu.Lock()
	// Binary VA: 0x102f98f10 | Size: 112 B | Source: third_party/jetski/cli/acp/server/server.go:L266 (deferwrap1)
	defer s.mu.Unlock()
	if s.pendingAuth != nil && s.pendingAuth.id == id && s.pendingAuth.cancel != nil { // L267-268
		s.pendingAuth.cancel() // L270
	}
}

// currentBackend returns s.backend under s.mu.
// Binary VA: 0x102f8da80 | Size: 240 B | Source: third_party/jetski/cli/acp/server/server.go:L273
func (s *Server) currentBackend() *backend.ServerBackend {
	s.mu.Lock()
	// Binary VA: 0x102f98f80 | Size: 112 B | Source: third_party/jetski/cli/acp/server/server.go:L275 (deferwrap1)
	defer s.mu.Unlock()
	return s.backend // L276
}
