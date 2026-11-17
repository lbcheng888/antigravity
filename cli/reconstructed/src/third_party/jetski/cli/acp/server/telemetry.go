// Binary VA: 0x102f936e0 | Source: third_party/jetski/cli/acp/server/telemetry.go
package server

import (
	"google3/third_party/jetski/cli/acp/auth/auth"
	"google3/third_party/jetski/cli/acp/jsonrpc/jsonrpc"
	"google3/third_party/jetski/cli/analytics/analytics"
)

// Binary VA: 0x102f936e0 | Size: 336 B | Source: third_party/jetski/cli/acp/server/telemetry.go:L33
func (s *Server) recordPendingClientConnected() {
	if s.singleSession || !s.telemetryReady.Load() {
		return
	}
	s.mu.Lock()
	ev := s.pendingClientConnected
	s.pendingClientConnected = nil
	s.mu.Unlock()
	if ev != nil {
		analytics.RecordACPClientConnected(*ev)
	}
}

// Binary VA: 0x102f93830 | Size: 352 B | Source: third_party/jetski/cli/acp/server/telemetry.go:L48
func (s *Server) recordAuthenticated(methodID string, rpcErr *jsonrpc.Error) {
	if s.singleSession || !s.telemetryReady.Load() {
		return
	}
	methodLabel := "other"
	if auth.IsKnownMethod(methodID) /* inferred from methods.go:155 mapaccess1_faststr */ {
		methodLabel = methodID
	}
	errCode := ""
	if rpcErr != nil {
		switch rpcErr.Code {
		case -32800:
			errCode = "cancelled"
		case -32603:
			errCode = "internal"
		case -32602:
			errCode = "invalid_params"
		case -32000:
			errCode = "auth_required"
		default:
			errCode = "other"
		}
	}
	analytics.RecordACPAuthenticated(methodLabel, errCode)
}

// Binary VA: 0x102f93990 | Size: 368 B | Source: third_party/jetski/cli/acp/server/telemetry.go:L63
func (s *Server) recordSessionStarted(source string /* inferred */, modelID string) {
	if s.singleSession || !s.telemetryReady.Load() {
		return
	}
	s.mu.Lock()
	clientName := s.clientInfo.Name
	s.mu.Unlock()
	analytics.RecordACPSessionStarted(source, clientName, modelID)
}
