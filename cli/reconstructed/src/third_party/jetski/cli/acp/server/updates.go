// Binary VA: 0x102f94460..0x102f968f8 | Source: third_party/jetski/cli/acp/server/updates.go

package server

import (
	"context"
	"crypto/sha256"
	"encoding/json/v2"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"

	"google3/base/go/log"
	"google3/third_party/gemini_coder/proto/gemini_coder_go_proto"
	"google3/third_party/jetski/cli/acp/messages/messages"
	"google3/third_party/jetski/cli/acp/schema/schema"
	"google3/third_party/jetski/cli/acp/session/session"
	"google3/third_party/jetski/cli/acp/stepview/stepview"
	"google3/third_party/jetski/cli/backend/backend"
	"google3/third_party/jetski/cli/steps/steps"
	"google3/third_party/jetski/codeium_common_pb/codeium_common_go_proto"
	"google3/third_party/jetski/cortex_pb/cortex_go_proto"
	"google3/third_party/jetski/jetski_cortex_pb/jetski_cortex_go_proto"
)

// Binary VA: 0x10a307150 | Size: 32 B
type titleUpdate struct {
	id    string
	title string
}

// stepUpdateSource /* inferred */ identifies how a step update was observed.
type stepUpdateSource /* inferred */ int

const (
	stepUpdateLive      stepUpdateSource = 1 // /* inferred */ active turn live stream
	stepUpdateHistory   stepUpdateSource = 2 // /* inferred */ historical replay
	stepUpdateOutOfTurn stepUpdateSource = 3 // /* inferred */ out-of-turn running stream
	stepUpdateIdle      stepUpdateSource = 4 // /* inferred */ out-of-turn idle stream
)

// Binary VA: 0x102f94460 | Size: 368 B | Source: third_party/jetski/cli/acp/server/updates.go:L28
func (s *Server) sendHistoricalStepUpdates(sessionID string, stepIdx int, step *gemini_coder_go_proto.Step) {
	s.mu.Lock()
	state, ok := s.sessionStates[sessionID]
	s.mu.Unlock()
	if !ok {
		return
	}
	s.sendStepUpdatesAs(state, sessionID, stepIdx, step, stepUpdateHistory)
}

// Binary VA: 0x102f945d0 | Size: 368 B | Source: third_party/jetski/cli/acp/server/updates.go:L41
func (s *Server) sendTurnStepUpdatesAs(expectedState *session.State, sessionID string, stepIdx int, step *gemini_coder_go_proto.Step, source stepUpdateSource /* inferred */) {
	s.mu.Lock()
	state := s.sessionStates[sessionID]
	s.mu.Unlock()
	if state != expectedState || state.Closed() {
		return
	}
	s.sendStepUpdatesAs(state, sessionID, stepIdx, step, source)
}

// Binary VA: 0x102f94740 | Size: 384 B | Source: third_party/jetski/cli/acp/server/updates.go:L83
func (s *Server) sendOutOfTurnStepUpdates(expectedState *session.State, sessionID string, stepIdx int, step *gemini_coder_go_proto.Step, running bool) {
	s.mu.Lock()
	state := s.sessionStates[sessionID]
	s.mu.Unlock()
	if state != expectedState || state.Closed() {
		return
	}
	source /* inferred */ := stepUpdateIdle
	if running {
		source = stepUpdateOutOfTurn
	}
	s.sendStepUpdatesAs(state, sessionID, stepIdx, step, source)
}

// Binary VA: 0x102f998c0 | Size: 480 B | Source: third_party/jetski/cli/acp/server/updates.go:L98 (inlined in attachTurn.func2)
func (s *Server) outOfTurnCallbacks(sessionID string, state *session.State) backend.StreamCallbacks {
	var running atomic.Bool
	return backend.StreamCallbacks{
		// Binary VA: 0x102f99c80 | Size: 80 B | Source: third_party/jetski/cli/acp/server/updates.go:L102
		OnStatus: func(status cortex_go_proto.CascadeRunStatus) {
			running.Store(status == 2) // CASCADE_RUN_STATUS_RUNNING
		},
		// Binary VA: 0x102f99ba0 | Size: 224 B | Source: third_party/jetski/cli/acp/server/updates.go:L105
		OnSteps: func(indices []int, steps []*gemini_coder_go_proto.Step) {
			for i, stepIdx := range indices {
				s.sendOutOfTurnStepUpdates(state, sessionID, stepIdx, steps[i], running.Load())
			}
		},
		// Binary VA: 0x102f99aa0 | Size: 256 B | Source: third_party/jetski/cli/acp/server/updates.go:L110
		OnStreamError: func(err error) {
			log.Warningf("Conversation stream of session %s lost between turns; the next prompt reopens it: %v", sessionID, err)
			state.WithdrawOpenInteractions()
		},
	}
}

// Binary VA: 0x102f948c0 | Size: 128 B | Source: third_party/jetski/cli/acp/server/updates.go:L128
func (s *Server) attachTurn(sessionID string, cb *backend.StreamCallbacks, state *session.State) *session.Stream {
	return state.AttachTurn(
		sessionID,
		cb,
		// Binary VA: 0x102f99cd0 | Size: 160 B | Source: third_party/jetski/cli/acp/server/updates.go:L129
		func() session.Streamer {
			tb := s.turns()
			if tb == nil {
				return nil
			}
			streamer, _ := tb.(session.Streamer)
			return streamer
		},
		// Binary VA: 0x102f998c0 | Size: 480 B | Source: third_party/jetski/cli/acp/server/updates.go:L129
		func() backend.StreamCallbacks {
			return s.outOfTurnCallbacks(sessionID, state)
		},
	)
}

// Binary VA: 0x102f94940 | Size: 400 B | Source: third_party/jetski/cli/acp/server/updates.go:L134
func (s *Server) markStaleWaitingStep(state *session.State, stepIdx int, step *gemini_coder_go_proto.Step) {
	tc /* inferred */ := step.GetMetadata().GetToolCall()
	if tc == nil {
		return
	}
	if strings.TrimPrefix(tc.Name, "default_api:") != "ask_question" {
		state.SetPendingToolPermission(session.ToolPermission{
			ToolCallID: tc.Id,
			Answered:   stepIdx,
		})
	}
	if step.GetRequestedInteraction().GetAskQuestion() != nil {
		state.SetPendingQuestion(stepIdx)
	}
}

// Binary VA: 0x102f94ad0 | Size: 4448 B | Source: third_party/jetski/cli/acp/server/updates.go:L147
func (s *Server) sendStepUpdatesAs(state *session.State, sessionID string, stepIdx int, step *gemini_coder_go_proto.Step, source stepUpdateSource /* inferred */) {
	if step == nil {
		return
	}
	state.RecordStepSeen(stepIdx)
	if state.IsSkippedHistory(stepIdx) {
		return
	}

	if usage := step.GetMetadata().GetModelUsage(); usage != nil {
		switch source {
		case stepUpdateLive, stepUpdateOutOfTurn, stepUpdateIdle:
			state.MarkStepUsageConsumed(stepIdx)
			state.RecordContextUsage(stepIdx, usage)
			s.sendStateUsageUpdate(state, sessionID)
		case stepUpdateHistory:
			state.MarkStepUsageConsumed(stepIdx)
			state.RecordContextUsage(stepIdx, usage)
		default:
			if state.RecordStepUsage(stepIdx, usage) {
				state.RecordContextUsage(stepIdx, usage)
				s.sendStateUsageUpdate(state, sessionID)
			}
		}
	}

	args := make(map[string]interface{})
	tc := step.GetMetadata().GetToolCall()
	if tc != nil {
		var status string
		switch step.Status {
		case 2: // CORTEX_STEP_STATUS_RUNNING
			status = "in_progress"
		case 3: // CORTEX_STEP_STATUS_DONE
			status = "completed"
		case 6, 7: // CORTEX_STEP_STATUS_ERROR, CORTEX_STEP_STATUS_CANCELLED
			status = "failed"
		case 9: // CORTEX_STEP_STATUS_WAITING
			status = "pending"
		default:
			return
		}

		if status != "pending" && source != stepUpdateHistory {
			state.WithdrawInteraction(tc.Id)
			state.ClearAskedQuestionAt(stepIdx)
		}

		if tc.ArgumentsJson != "" {
			_ = json.Unmarshal([]byte(tc.ArgumentsJson), &args)
		}

		kind, title, locations := stepview.KindTitleAndLocations(step, tc.Name, args, s.params.WorkingDir)
		upd /* inferred */ := messages.ToolCallSessionUpdate{
			ToolCallID: tc.Id,
			Name:       tc.Name,
			Args:       args,
			Status:     status,
			Kind:       kind,
			Title:      title,
			Locations:  locations,
			RawInput:   args,
		}
		if status == "completed" || status == "failed" {
			rawOut, content := stepview.ToolOutput(step)
			if rawOut != nil {
				upd.RawOutput = rawOut
				upd.Content = content
			}
		}

		encoded, err := json.Marshal(upd)
		if err != nil {
			log.Errorf("Failed to encode tool call update %s: %v", tc.Id, err)
		}
		fp /* inferred */ := sha256.Sum256(encoded)
		isNew, shouldSend := state.RecordToolCall(upd, fp)
		if shouldSend {
			var sessionUpd messages.SessionUpdate
			if isNew {
				sessionUpd = upd
			} else {
				sessionUpd = messages.ToolCallUpdateSessionUpdate(upd)
			}
			_ = s.conn.SendNotification("session/update", schema.SessionNotification{
				SessionID: sessionID,
				Update:    sessionUpd,
			})
		}
	}

	allowInteractive /* inferred */ := source == 0 || source == stepUpdateLive || source == stepUpdateOutOfTurn
	if source == stepUpdateIdle && step.Status == 9 {
		s.markStaleWaitingStep(state, stepIdx, step)
	}
	if allowInteractive && step.Status == 9 {
		if tc != nil && strings.TrimPrefix(tc.Name, "default_api:") != "ask_question" {
			perm /* inferred */ := session.ToolPermission{
				ToolCallID: tc.Id,
				Answered:   stepIdx,
			}
			if state.SetPendingToolPermission(perm) {
				interCtx /* inferred */, release /* inferred */ := state.BeginClientInteraction(perm)
				tcName /* inferred */ := strings.TrimPrefix(tc.Name, "default_api:")
				// Binary VA: 0x102f99d70 | Size: 144 B | Source: third_party/jetski/cli/acp/server/updates.go:L257
				go func() {
					defer release()
					s.resolveToolPermission(interCtx, sessionID, stepIdx, step, tc, tcName, args)
				}()
			}
		}
		if spec := step.GetRequestedInteraction().GetAskQuestion(); spec != nil {
			if state.SetPendingQuestion(stepIdx) {
				var toolCallID string
				if tc != nil {
					toolCallID = tc.Id
				}
				s.askQuestion(sessionID, stepIdx, toolCallID, state, spec)
			}
		}
	}

	if step.Type == 14 && !state.IsOwnInput(stepIdx) {
		if ui := step.GetUserInput(); ui != nil {
			hasNonEmptyText /* inferred */ := false
			for itemIdx, item := range ui.Items {
				if t, ok := item.Chunk.(*codeium_common_go_proto.TextOrScopeItem_Text); ok && t.Text != "" {
					if !hasNonEmptyText {
						hasNonEmptyText = strings.TrimSpace(t.Text) != ""
					}
					if chunk := state.PendingUserInputText(stepIdx, itemIdx, t.Text); chunk != "" {
						state.MarkUserMessage()
						_ = s.conn.SendNotification("session/update", schema.SessionNotification{
							SessionID: sessionID,
							Update: messages.UserMessageChunkSessionUpdate{
								Content: messages.TextContentBlock{Text: chunk},
							},
						})
					}
					continue
				}
				if hasNonEmptyText {
					continue
				}
				if si, ok := item.Chunk.(*codeium_common_go_proto.TextOrScopeItem_Item); ok && si.Item != nil {
					if sc, ok := si.Item.ScopeItem.(*codeium_common_go_proto.ContextScopeItem_SlashCommand); ok && sc.SlashCommand != nil {
						if cmdName := sc.SlashCommand.Name; cmdName != "" {
							if state.SetPendingItem(stepIdx, itemIdx) {
								state.MarkUserMessage()
								_ = s.conn.SendNotification("session/update", schema.SessionNotification{
									SessionID: sessionID,
									Update: messages.UserMessageChunkSessionUpdate{
										Content: messages.TextContentBlock{Text: "/" + cmdName},
									},
								})
							}
						}
					} else if f, ok := si.Item.ScopeItem.(*codeium_common_go_proto.ContextScopeItem_File); ok && f.File != nil {
						if uri := f.File.AbsoluteUri; uri != "" {
							if state.SetPendingItem(stepIdx, itemIdx) {
								state.MarkUserMessage()
								_ = s.conn.SendNotification("session/update", schema.SessionNotification{
									SessionID: sessionID,
									Update: messages.UserMessageChunkSessionUpdate{
										Content: messages.ResourceLinkContentBlock{
											URI:  uri,
											Name: filepath.Base(uri),
										},
									},
								})
							}
						}
					}
				}
			}
		}
		return
	}

	if step.Type == 17 {
		if em := step.GetErrorMessage(); em != nil && em.Error != nil {
			msg /* inferred */ := steps.ErrorMessage(em.Error)
			if msg == "" {
				msg = "Agent execution terminated due to error."
			}
			if chunk := state.PendingText(stepIdx, fmt.Sprintf("\n⚠ %s\n", msg)); chunk != "" {
				_ = s.conn.SendNotification("session/update", schema.SessionNotification{
					SessionID: sessionID,
					Update: messages.AgentMessageChunkSessionUpdate{
						Content: messages.TextContentBlock{Text: chunk},
					},
				})
			}
		}
		return
	}

	if step.Type == 15 {
		if pr := step.GetPlannerResponse(); pr != nil {
			if thoughts := state.PendingThoughts(stepIdx, pr.Thought); thoughts != "" {
				_ = s.conn.SendNotification("session/update", schema.SessionNotification{
					SessionID: sessionID,
					Update: messages.AgentThoughtChunkSessionUpdate{
						Content: messages.TextContentBlock{Text: thoughts},
					},
				})
			}
			if text := state.PendingText(stepIdx, pr.Response); text != "" {
				prefix := state.AgentTextPrefix(stepIdx)
				_ = s.conn.SendNotification("session/update", schema.SessionNotification{
					SessionID: sessionID,
					Update: messages.AgentMessageChunkSessionUpdate{
						Content: messages.TextContentBlock{Text: prefix + text},
					},
				})
			}
		}
	}
}

// Binary VA: 0x102f95c30 | Size: 768 B | Source: third_party/jetski/cli/acp/server/updates.go:L362
func (s *Server) handleSummariesUpdate(summaries map[string]*jetski_cortex_go_proto.CascadeTrajectorySummary, deleted []string) {
	var updates /* inferred */ []titleUpdate
	s.mu.Lock()
	for id, state := range s.sessionStates {
		title := summaries[id].GetSummary()
		if state.SetPendingTitle(title) {
			updates = append(updates, titleUpdate{id: id, title: title})
		}
	}
	s.mu.Unlock()
	for _, u := range updates {
		s.writeSessionInfoUpdate(u.id, u.title)
	}
}

// Binary VA: 0x102f95f30 | Size: 384 B | Source: third_party/jetski/cli/acp/server/updates.go:L382
func (s *Server) sendSessionInfoUpdate(sessionID, title string) {
	s.mu.Lock()
	state, ok := s.sessionStates[sessionID]
	s.mu.Unlock()
	if !ok || !state.SetPendingTitle(title) {
		return
	}
	s.writeSessionInfoUpdate(sessionID, title)
}

// Binary VA: 0x102f960b0 | Size: 400 B | Source: third_party/jetski/cli/acp/server/updates.go:L392
func (s *Server) writeSessionInfoUpdate(sessionID, title string) {
	if s.conn == nil {
		return
	}
	log.Infof("Sending session_info_update for session %s: title=%q", sessionID, title)
	_ = s.conn.SendNotification("session/update", schema.SessionNotification{
		SessionID: sessionID,
		Update: messages.SessionInfoUpdateSessionUpdate{
			Title: title,
		},
	})
}

// Binary VA: 0x102f96240 | Size: 224 B | Source: third_party/jetski/cli/acp/server/updates.go:L407
func (s *Server) contextWindowSize(sessionID string) int64 {
	m, ok := s.currentModel(s.effectiveModelConfig(), sessionID)
	if !ok {
		return 0
	}
	return int64(m.MaxTokens)
}

// Binary VA: 0x102f96320 | Size: 352 B | Source: third_party/jetski/cli/acp/server/updates.go:L417
func (s *Server) sendUsageUpdate(sessionID string) {
	s.mu.Lock()
	state, ok := s.sessionStates[sessionID]
	s.mu.Unlock()
	if !ok {
		return
	}
	s.sendStateUsageUpdate(state, sessionID)
}

// Binary VA: 0x102f96480 | Size: 256 B | Source: third_party/jetski/cli/acp/server/updates.go:L429
func (s *Server) sendStateUsageUpdate(state *session.State, sessionID string) {
	if s.conn == nil {
		return
	}
	windowSize := s.contextWindowSize(sessionID)
	used, ok := state.UsageToReport(windowSize)
	if !ok {
		return
	}
	_ = s.conn.SendNotification("session/update", schema.SessionNotification{
		SessionID: sessionID,
		Update: messages.UsageUpdateSessionUpdate{
			Used: used,
			Size: windowSize,
		},
	})
}

// Binary VA: 0x102f96580 | Size: 896 B | Source: third_party/jetski/cli/acp/server/updates.go:L447
func (s *Server) sendAvailableCommands(ctx context.Context, sessionID string) {
	if ctx != nil && ctx.Err() != nil {
		return
	}
	defs := s.conversationSlashCommands(sessionID)
	if ctx != nil && ctx.Err() != nil {
		return
	}
	if len(defs) == 0 {
		log.Infof("No slash commands available to advertise for session %s", sessionID)
		return
	}
	cmds := make([]messages.AvailableCommand, 0, len(defs))
	for _, def := range defs {
		if def == nil || def.Command == nil || def.Command.Name == "" {
			continue
		}
		cmds = append(cmds, messages.AvailableCommand{
			Name:        def.Command.Name,
			Description: def.Description,
		})
	}
	log.Infof("Advertising %d slash commands for session %s", len(cmds), sessionID)
	_ = s.conn.SendNotification("session/update", schema.SessionNotification{
		SessionID: sessionID,
		Update: messages.AvailableCommandsSessionUpdate{
			AvailableCommands: cmds,
		},
	})
}
