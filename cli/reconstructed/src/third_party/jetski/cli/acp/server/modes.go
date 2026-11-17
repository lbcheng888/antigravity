// Binary VA: 0x102f868b0 | Source: third_party/jetski/cli/acp/server/modes.go
package server

import (
	"context"
	"fmt"

	"google3/base/go/log"
	"google3/third_party/jetski/cli/acp/jsonrpc/jsonrpc"
	"google3/third_party/jetski/cli/acp/messages/messages"
	"google3/third_party/jetski/cli/acp/schema/schema"
	"google3/third_party/jetski/cli/acp/session/session"
	"google3/third_party/jetski/cli/backend/backend"
	"google3/third_party/jetski/cli/store/store"
	"google3/third_party/jetski/proto/cortex_go_proto"
)

// Binary VA: 0x102f868b0 | Size: 128 B | Source: third_party/jetski/cli/acp/server/modes.go:L22
func autoMode(b *backend.ServerBackend) (bool, bool, bool) {
	if b == nil || !store.IsAutoModeFlagEnabled() /* store.go:37 indirect */ {
		return false, false, false
	}
	return b.AutoModeEnabled()
}

// Binary VA: 0x102f86930 | Size: 64 B | Source: third_party/jetski/cli/acp/server/modes.go:L30
func (s *Server) autoModeAvailable() bool {
	ok, _, _ := autoMode(s.currentBackend())
	return ok
}

// Binary VA: 0x102f86970 | Size: 176 B | Source: third_party/jetski/cli/acp/server/modes.go:L38
func (s *Server) autoModeInEffect(b *backend.ServerBackend) bool {
	ok, a, c := autoMode(b)
	if !ok {
		return false
	}
	return store.PermissionPresetFor("auto", s.cliSettings.get(), a, c) != 5
}

// Binary VA: 0x102f86a20 | Size: 160 B | Source: third_party/jetski/cli/acp/server/modes.go:L45
func (s *Server) effectiveMode(sessionID string) string {
	mode := s.sessionMode(sessionID)
	if mode == "auto" && !s.autoModeInEffect(s.currentBackend()) {
		return "default"
	}
	return mode
}

// Binary VA: 0x102f86ac0 | Size: 240 B | Source: third_party/jetski/cli/acp/server/modes.go:L57
func (s *Server) applyAutoMode(b *backend.ServerBackend, overlay *cortex_go_proto.CascadeConfig) *cortex_go_proto.CascadeConfig {
	policy := store.AutoExecPolicy("auto", s.cliSettings.get())
	toolCfg := b.AutoModeToolConfig(policy)
	return mergeTurnOverlay(overlay, &cortex_go_proto.CascadeConfig{
		PlannerConfig: &cortex_go_proto.CascadePlannerConfig{
			ToolConfig: toolCfg,
		},
	})
}

// Binary VA: 0x102f86bb0 | Size: 80 B | Source: third_party/jetski/cli/acp/server/modes.go:L66
func (s *Server) sessionMode(sessionID string) string {
	return s.sessionState(sessionID).Mode()
}

// Binary VA: 0x102f86c00 | Size: 992 B | Source: third_party/jetski/cli/acp/server/modes.go:L72
func (s *Server) setSessionMode(ctx context.Context, sessionID, modeID string) (string, *jsonrpc.Error) {
	st := s.sessionState(sessionID)
	if st == nil {
		return "", &jsonrpc.Error{Code: -32602, Message: fmt.Sprintf("Session not found: %s", sessionID)}
	}
	normalized := ""
	found := false
	for _, m := range session.Modes() /* @0x10d3fbe30 len=4 */ {
		id := m.ID
		if id == "" {
			id = "default"
		}
		if id == modeID {
			normalized = id
			found = true
			break
		}
	}
	if !found {
		return "", &jsonrpc.Error{Code: -32602, Message: fmt.Sprintf("Unknown mode: %s", modeID)}
	}
	if normalized == "auto" && !s.autoModeAvailable() && st.Mode() != "auto" {
		return "", &jsonrpc.Error{Code: -32602, Message: "Auto mode is not available: it needs the OS terminal sandbox and may not be rolled out to you yet"}
	}
	log.InfoContextf(ctx, "Session %s mode set to %s", sessionID, normalized) /* inferred */
	st.SetMode(normalized)
	return normalized, nil
}

// Binary VA: 0x102f86fe0 | Size: 480 B | Source: third_party/jetski/cli/acp/server/modes.go:L89
func (s *Server) handleSessionSetMode(ctx context.Context, req *jsonrpc.Request) {
	if !s.requireAuth(ctx, req) {
		return
	}
	var params struct {
		SessionID string `json:"sessionId"`
		ModeID    string `json:"modeId"`
	}
	if !s.parseParams(req, &params) {
		return
	}
	if _, rpcErr := s.setSessionMode(ctx, params.SessionID, params.ModeID); rpcErr != nil {
		s.conn.writeMessage(jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Error: rpcErr})
		return
	}
	s.sendModeUpdate(params.SessionID)
	s.sendConfigOptionUpdate(params.SessionID)
	s.conn.writeMessage(jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Result: struct{}{}})
}

// Binary VA: 0x102f871c0 | Size: 192 B | Source: third_party/jetski/cli/acp/server/modes.go:L115
func (s *Server) sendModeUpdate(sessionID string) {
	s.conn.SendNotification("session/update", schema.SessionNotification{
		SessionID: sessionID,
		Update:    messages.CurrentModeUpdateSessionUpdate{CurrentModeID: s.sessionMode(sessionID)},
	})
}
