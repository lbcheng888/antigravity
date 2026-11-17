// Binary VA: 0x102f93b00..0x102f94460 | Source: third_party/jetski/cli/acp/server/turn_cancel.go

package server

import (
	"context"
	"fmt"

	"google3/base/go/log"
	"google3/third_party/gemini_coder/proto/gemini_coder_go_proto"
	"google3/third_party/golang/connectrpc_com/connect/v/v1/connect"
	"google3/third_party/jetski/cli/acp/messages/messages"
	"google3/third_party/jetski/cli/acp/schema/schema"
	"google3/third_party/jetski/cli/acp/session/session"
	"google3/third_party/jetski/cli/acp/stepview/stepview"
	"google3/third_party/jetski/cli/backend/backend"
	"google3/third_party/jetski/cli/types/types"
	"google3/third_party/jetski/codeium_common_pb/codeium_common_go_proto"
	"google3/third_party/jetski/language_server_pb/language_server_go_proto"
)

// Binary VA: 0x10a1099f8 | Size: 16 B
type cascadeTreeStopper interface {
	ForceStopCascadeTree(context.Context, *connect.Request[language_server_go_proto.ForceStopCascadeTreeRequest]) (*connect.Response[language_server_go_proto.ForceStopCascadeTreeResponse], error)
}

// Binary VA: 0x10a109a60 | Size: 16 B
type treeStopper interface {
	ForceStopTree(context.Context, string) error
}

// Binary VA: 0x10aa70868 | Size: 24 B
type serverTurns struct {
	*backend.ServerBackend
	ls cascadeTreeStopper
}

// Binary VA: 0x102f93b00 | Size: 944 B | Source: third_party/jetski/cli/acp/server/turn_cancel.go:L23
func (s *Server) resolveToolPermission(ctx context.Context, sessionID string, stepIdx int, step *gemini_coder_go_proto.Step, tc *codeium_common_go_proto.ChatToolCall, toolName string, args map[string]interface{}) {
	auto /* inferred */ := s.autoApprovesWrite(sessionID, step)
	var allowed /* inferred */ bool
	if auto {
		allowed = true
	} else {
		allowed = s.requestPermissionFromClient(ctx, sessionID, stepIdx, step, tc, toolName, args)
	}
	if ctx.Err() != nil {
		return
	}
	var grants /* inferred */ []string
	if allowed && !auto && toolName == "ask_permission" {
		action, _ /* inferred */ := stepview.Arg[string](args, "action")
		target, _ /* inferred */ := stepview.Arg[string](args, "target")
		if action != "" && target != "" {
			grants = []string{fmt.Sprintf("%s(%s)", action, target)}
		}
	}
	tb /* inferred */ := s.turns()
	if tb == nil {
		return
	}
	if err := tb.RespondToToolConfirmation(sessionID, stepIdx, step, allowed, grants); err != nil {
		log.Errorf("Failed to respond to tool confirmation: %v", err) // log.ctxlogf(nil, ...)
	}
}

// Binary VA: 0x102f93eb0 | Size: 320 B | Source: third_party/jetski/cli/acp/server/turn_cancel.go:L50
func (s *Server) autoApprovesWrite(sessionID string, step *gemini_coder_go_proto.Step) bool {
	b /* inferred */ := s.currentBackend()
	if b == nil {
		return false
	}
	mode /* inferred */ := s.effectiveMode(sessionID)
	if mode != "accept-edits" && mode != "auto" {
		return false
	}
	perm /* inferred */ := step.GetRequestedInteraction().GetPermission()
	_, allowNonWorkspace /* inferred */, _ := s.cliSettings.get()
	return types.AutoApprovableWrite(perm, allowNonWorkspace, b.GetWorkspaceDirs())
}

// Binary VA: 0x102f93ff0 | Size: 272 B | Source: third_party/jetski/cli/acp/server/turn_cancel.go:L61
func (s *Server) failPendingToolCalls(sessionID string, state *session.State) {
	updates /* inferred */ := state.SweepPendingToolCalls()
	for _, u /* inferred */ := range updates {
		_ = s.conn.SendNotification("session/update", schema.SessionNotification{
			SessionID: sessionID,
			Update:    u,
		})
	}
}

// Binary VA: 0x102f94100 | Size: 576 B | Source: third_party/jetski/cli/acp/server/turn_cancel.go:L98
func (t serverTurns) ForceStopTree(ctx context.Context, conversationID string) error {
	if t.ls != nil {
		reqCtx /* inferred */ := context.WithoutCancel(ctx)
		req /* inferred */ := connect.NewRequest(&language_server_go_proto.ForceStopCascadeTreeRequest{
			CascadeId: conversationID,
		})
		if _, err := t.ls.ForceStopCascadeTree(reqCtx, req); err == nil {
			return nil
		} else {
			log.WarningContextf(ctx, "ForceStopCascadeTree(%s) failed; cancelling its root cascade and background tasks only: %v", conversationID, err)
		}
	}
	return t.Cancel(conversationID) // inlined (*ServerBackend).Cancel -> cancelConversation(conversationID, true)
}

// Binary VA: 0x102f94340 | Size: 288 B | Source: third_party/jetski/cli/acp/server/turn_cancel.go:L114
func stopConversationTree(ctx context.Context, tb turnBackend, sessionID string) error {
	if stopper, ok := tb.(treeStopper); ok {
		return stopper.ForceStopTree(ctx, sessionID)
	}
	return tb.Cancel(sessionID)
}
