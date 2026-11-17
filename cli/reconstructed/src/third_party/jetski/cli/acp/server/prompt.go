// Reconstructed from agy v1.5.10 binary evidence (disassembly + Go type table).
// Binary VA: 0x102f87d60 | Size: 20 funcs (first func prepareTurnRequest, 544 B) | Source: third_party/jetski/cli/acp/server/prompt.go:L31
// Binary source: third_party/jetski/cli/acp/server/prompt.go
// Reconstructed from agy v1.5.10 binary evidence.

package server

import (
	"context"
	"fmt"
	"sync"

	"google3/base/go/log"
	"google3/third_party/jetski/cli/acp/jsonrpc/jsonrpc"
	"google3/third_party/jetski/cli/acp/messages/messages"
	"google3/third_party/jetski/cli/acp/schema/schema"
	"google3/third_party/jetski/cli/acp/session/session"
	"google3/third_party/jetski/cli/backend/backend"
	"google3/third_party/jetski/cortex_pb/cortex_go_proto"
	"google3/third_party/jetski/language_server_pb/language_server_go_proto"
)

// Binary VA: 0x102f87d60 | Size: 544 B | Source: third_party/jetski/cli/acp/server/prompt.go:L31
func (s *Server) prepareTurnRequest(ctx context.Context, sessionID string, base *cortex_go_proto.CascadeConfig) (context.Context, *cortex_go_proto.CascadeConfig) {
	s.mu.Lock()
	st := s.sessionStates[sessionID]
	s.mu.Unlock()

	ctx = withTurnContext(ctx, sessionID)
	if st == nil {
		return ctx, base
	}
	cfg := mergeTurnOverlay(base, st.TurnOverlay())
	if st.Mode() == "auto" {
		if b := s.currentBackend(); b != nil && s.autoModeInEffect(b, st) {
			cfg = s.applyAutoMode(b, cfg)
		}
	}
	return ctx, cfg
}

// Binary VA: 0x102f87f80 | Size: 2448 B | Source: third_party/jetski/cli/acp/server/prompt.go:L51
func (s *Server) handleSessionPrompt(ctx context.Context, req *jsonrpc.Request) {
	if !s.requireAuth(ctx, req.ID) {
		return
	}
	var params schema.PromptParams
	if !s.parseParams(req, &params) {
		return
	}
	if b := s.currentBackend(); b != nil && params.Meta != nil {
		for _, root := range params.Meta.AdditionalRootList() {
			if err := b.AddWorkspaceDir(root); err != nil {
				log.ctxlogf(ctx, 1 /* inferred */, "Failed to add additional workspace directory %s from prompt _meta: %v", root, err)
			}
		}
	}

	s.mu.Lock()
	st, ok := s.sessionStates[params.SessionID]
	s.mu.Unlock()
	if !ok || st == nil {
		s.conn.writeMessage(&jsonrpc.Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &jsonrpc.Error{
				Code:    -32602,
				Message: fmt.Sprintf("Session not found: %s", params.SessionID),
			},
		})
		return
	}

	// Binary VA: 0x102f97410 | Size: 64 B | Source: third_party/jetski/cli/acp/server/prompt.go:L83
	slashCmdsOnce := sync.OnceValue(func() []*language_server_go_proto.SlashCommandDefinition {
		return s.conversationSlashCommands(params.SessionID)
	})
	var items []*codeium_common_go_proto.TextOrScopeItem
	var media []*codeium_common_go_proto.Media
	var err error
	if st.Mode() == "plan" {
		items, media, err = planPrompt(params.Prompt, slashCmdsOnce())
	} else {
		items, media, err = promptToUserMessage(params.Prompt)
	}
	if err != nil {
		s.conn.writeMessage(&jsonrpc.Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &jsonrpc.Error{
				Code:    -32602,
				Message: fmt.Sprintf("Invalid prompt: %v", err),
			},
		})
		return
	}
	if len(items) == 0 && len(media) == 0 {
		s.conn.writeMessage(&jsonrpc.Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &jsonrpc.Error{
				Code:    -32602,
				Message: "Empty prompt",
			},
		})
		return
	}
	if st.HasAskedQuestion() && len(media) > 0 {
		s.conn.writeMessage(&jsonrpc.Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &jsonrpc.Error{
				Code:    -32602,
				Message: "Images can't answer the agent's pending question; reply in text",
			},
		})
		return
	}

	stopReason, usage, rpcErr := s.promptTurn(ctx, st, params, items, media)
	if rpcErr != nil {
		if !s.conn.IsClosed() {
			s.conn.writeMessage(&jsonrpc.Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   rpcErr,
			})
		}
		return
	}
	s.conn.writeMessage(&jsonrpc.Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: schema.PromptResult{
			StopReason: stopReason,
			Usage:      usage,
		},
	})
}

// Binary VA: 0x102f88910 | Size: 320 B | Source: third_party/jetski/cli/acp/server/prompt.go:L127
func (s *Server) conversationSlashCommands(sessionID string) []*language_server_go_proto.SlashCommandDefinition {
	b := s.currentBackend()
	if b == nil {
		return nil
	}
	return b.slashCommandsFor(sessionID)
}

// Binary VA: 0x102f88a50 | Size: 16 B | Source: third_party/jetski/cli/acp/server/prompt.go:L138
func noRelease() {}

// Binary VA: 0x102f88a60 | Size: 4576 B | Source: third_party/jetski/cli/acp/server/prompt.go:L145
func (s *Server) promptTurn(
	ctx context.Context,
	st *session.State,
	params schema.PromptParams,
	items []*codeium_common_go_proto.TextOrScopeItem,
	media []*codeium_common_go_proto.Media,
) (string, *messages.Usage, *jsonrpc.Error) {
	lease, err := st.AcquireTurn()
	if err != nil {
		return "", nil, &jsonrpc.Error{
			Code:    -32603,
			Message: fmt.Sprintf("Can't add this prompt to session %s: %v", params.SessionID, err),
		}
	}

	st.MarkUserMessage()

	if lease.Steer() {
		tb := s.turns()
		if tb == nil {
			st.FinishSteer()
			return "", nil, &jsonrpc.Error{
				Code:    -32603,
				Message: fmt.Sprintf("No %s backend to run session %s", s.productLabel() /* inferred */, params.SessionID),
			}
		}
		if err := tb.SendUserMessage(params.SessionID, items, media...); err != nil {
			st.FinishSteer()
			return "", nil, &jsonrpc.Error{
				Code:    -32603,
				Message: fmt.Sprintf("Failed to send user message: %v", err),
			}
		}
		if st.CancelledWhileOpen() {
			s.cancelBackend(ctx, params.SessionID)
		}
		st.FinishSteer()
		select {
		case <-lease.Finished():
		case <-ctx.Done():
			return "cancelled", nil, nil
		}
		return s.awaitTurnRelease(ctx, params.SessionID, st, lease)
	}

	asked := st.AskedQuestion()
	tb := s.turns()
	if tb == nil {
		st.FinishTurn(lease)
		return "", nil, &jsonrpc.Error{
			Code:    -32603,
			Message: fmt.Sprintf("No %s backend to run session %s", s.productLabel() /* inferred */, params.SessionID),
		}
	}

	var mu sync.Mutex
	var sawOwnInput bool
	var pendingInputs int
	var seenRunStatus bool
	var lastStatus cortex_go_proto.CascadeRunStatus
	var baseStep int
	var maxStep int
	var streamLost bool
	var turnFinished bool

	fence := &session.Fence{}
	callbacks := backend.StreamCallbacks{
		// Binary VA: 0x102f98060 | Size: 1696 B | Source: third_party/jetski/cli/acp/server/prompt.go:L218
		OnSteps: func(indices []int, steps []*gemini_coder_go_proto.Step) {
			if !fence.Enter() {
				return
			}
			defer fence.Leave() /* inferred */
			mu.Lock()
			// [unrecovered] inspect step indices and types (user input=14/4, etc.), update sawOwnInput/pendingInputs/maxStep
			mu.Unlock()
			st.MarkOwnInput()
			s.sendTurnStepUpdatesAs(params.SessionID, st, indices, steps, lease)
			mu.Lock()
			// [unrecovered] check if turn finished based on run status and pending inputs
			shouldFinish := turnFinished
			mu.Unlock()
			if shouldFinish {
				st.SignalTurnFinished()
			}
		},
		// Binary VA: 0x102f97f30 | Size: 176 B | Source: third_party/jetski/cli/acp/server/prompt.go:L280
		OnStreamError: func(err error) {
			if !fence.Enter() {
				return
			}
			defer fence.Leave() /* inferred */
			st.SignalStreamLost()
			_ = lease
		},
		// Binary VA: 0x102f97d00 | Size: 432 B | Source: third_party/jetski/cli/acp/server/prompt.go:L288
		OnStatus: func(status cortex_go_proto.CascadeRunStatus) {
			if !fence.Enter() {
				return
			}
			defer fence.Leave() /* inferred */
			mu.Lock()
			lastStatus = status
			seenRunStatus = true
			// [unrecovered] update streamLost/turnFinished state
			mu.Unlock()
		},
		// Binary VA: 0x102f97970 | Size: 784 B | Source: third_party/jetski/cli/acp/server/prompt.go:L308
		OnFullyIdle: func(idle bool) {
			if !fence.Enter() {
				return
			}
			defer fence.Leave() /* inferred */
			if st.TurnCancelled() {
				st.SignalTurnFinished()
				return
			}
			mu.Lock()
			if pendingInputs > 0 {
				log.ctxlogf(ctx, 2 /* inferred */, "Session %s: IDLE while %d of the turn's %d user inputs are still to arrive; waiting for the turn's run", params.SessionID, pendingInputs, baseStep)
				mu.Unlock()
				return
			}
			mu.Unlock()
			st.SignalTurnFinished()
		},
	}

	stream := s.attachTurn(params.SessionID, st, lease)
	// Binary VA: 0x102f97900 | Size: 112 B | Source: third_party/jetski/cli/acp/server/prompt.go:L333
	detach := sync.OnceValue(func() func() {
		return func() {
			stream.Detach()
			fence.Close()
		}
	})
	defer detach()()

	if asked != nil {
		msgStr := params.Prompt.String()
		log.ctxlogf(ctx, 1 /* inferred */, "Responding to pending ask_question for session %s step %d: %q", params.SessionID, asked.Step, msgStr)
		// [unrecovered] build []*cortex_go_proto.AskQuestionEntry from asked.Spec and msgStr
		var entries []*cortex_go_proto.AskQuestionEntry
		st.ClearAskedQuestion()
		if err := tb.RespondToAskQuestion(params.SessionID, asked.Step, entries); err != nil {
			st.SetAskedQuestion(asked)
			st.FinishTurn(lease)
			return "", nil, &jsonrpc.Error{
				Code:    -32603,
				Message: fmt.Sprintf("Failed to respond to ask_question: %v", err),
			}
		}
	} else {
		if err := tb.SendUserMessage(params.SessionID, items, media...); err != nil {
			st.FinishTurn(lease)
			return "", nil, &jsonrpc.Error{
				Code:    -32603,
				Message: fmt.Sprintf("Failed to send user message: %v", err),
			}
		}
	}

	if st.CancelledWhileOpen() {
		s.cancelBackend(ctx, params.SessionID)
	}
	// Binary VA: 0x102f97450 | Size: 304 B | Source: third_party/jetski/cli/acp/server/prompt.go:L390
	st.MarkTurnReady(func() bool {
		mu.Lock()
		defer mu.Unlock()
		return sawOwnInput && seenRunStatus
	})

	_ = callbacks
	_ = maxStep
	_ = streamLost
	return s.awaitTurnRelease(ctx, params.SessionID, st, lease)
}

// Binary VA: 0x102f89c40 | Size: 1280 B | Source: third_party/jetski/cli/acp/server/prompt.go:L412
func (s *Server) awaitTurnRelease(ctx context.Context, sessionID string, st *session.State, lease session.Lease) (string, *messages.Usage, *jsonrpc.Error) {
	select {
	case <-lease.Finished():
		st.FinishTurn(lease)
		return "end_turn", nil, nil
	case usage := <-lease.Superseded():
		st.FinishTurn(lease)
		return "end_turn", usage, nil
	case <-ctx.Done():
		st.HoldOwnedTurn(lease)
		s.cancelBackend(ctx, sessionID)
		st.FinishTurn(lease)
		return "cancelled", nil, nil
	case <-s.connCtx.Done():
		st.CloseStream()
		return "", nil, &jsonrpc.Error{
			Code:    -32603,
			Message: "connection closed",
		}
	}
}

// Binary VA: 0x102f8a140 | Size: 256 B | Source: third_party/jetski/cli/acp/server/prompt.go:L471
func (s *Server) turns() turnBackend {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.turnSource != nil {
		return s.turnSource()
	}
	return nil
}
