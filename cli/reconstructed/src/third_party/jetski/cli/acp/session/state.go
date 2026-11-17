// Reconstructed from agy v1.5.10 binary evidence (disassembly + Go type table).
// Binary VA: 0x10231d8c0 | Size: 80 funcs (first func usageFromModelUsageStats, 272 B) | Source: third_party/jetski/cli/acp/session/state.go:L26
// Reconstructed from agy v1.5.10 arm64 binary.
// Source: third_party/jetski/cli/acp/session/state.go

package session

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"time"

	"google3/third_party/jetski/cli/acp/messages/messages"
	"google3/third_party/jetski/cli/types/types"
	cortex_go_proto "google3/third_party/jetski/cortex_pb/cortex_go_proto"
	codeium_common_go_proto "google3/third_party/jetski/codeium_common_pb/codeium_common_go_proto"
	"google3/third_party/golang/protobuf/v2/proto/proto"
)

// Binary VA: 0x10231d8c0 | Size: 272 B | Source: third_party/jetski/cli/acp/session/state.go:L26
func usageFromModelUsageStats(stats *codeium_common_go_proto.ModelUsageStats) (messages.Usage, bool) {
	if stats == nil {
		return messages.Usage{}, false
	}
	inputTokens := stats.GetInputTokens()
	thinkingTokens := stats.GetThinkingOutputTokens()
	responseTokens := stats.GetResponseOutputTokens()
	outputTokens := stats.GetOutputTokens()
	cacheWriteTokens := stats.GetCacheWriteTokens()
	cacheReadTokens := stats.GetCacheReadTokens()

	if thinkingTokens == 0 && outputTokens > thinkingTokens {
		// Wait: disassembly checks outputTokens > thinkingTokens (actually L35: cbnz x9, L38; cmp x8, x4; b.le L38; sub x9, x8, x4)
		// x9 is thinkingTokens (+0x20), x4 is... let's check offsets:
		// +0x10: x7 = inputTokens
		// +0x18: x8 = outputTokens
		// +0x20: x4 = thinkingTokens
		// +0x28: x9 = responseTokens
		// +0x30: x3 = cacheWriteTokens
		// +0x38: x2 = cacheReadTokens
		// L35: if responseTokens == 0 && outputTokens > thinkingTokens { responseTokens = outputTokens - thinkingTokens }
		responseTokens = outputTokens - thinkingTokens
	}
	resolvedOutput := responseTokens + thinkingTokens
	total := inputTokens + cacheReadTokens + cacheWriteTokens + resolvedOutput
	if total <= 0 && inputTokens <= 0 && resolvedOutput <= 0 && cacheReadTokens <= 0 && cacheWriteTokens <= 0 && thinkingTokens <= 0 {
		return messages.Usage{}, false
	}
	return messages.Usage{
		InputTokens:       inputTokens,
		OutputTokens:      resolvedOutput,
		CachedReadTokens:  cacheReadTokens,
		CachedWriteTokens: cacheWriteTokens,
		ThoughtTokens:     thinkingTokens,
		TotalTokens:       total,
	}, true
}

// State holds per-session synchronization, deduplication, usage, and turn state.
// Struct size: 320 B (0x140).
type State struct {
	mu                  sync.Mutex
	sentText            map[int]int
	lastTextStep        int
	hasLastText         bool
	ownInputs           map[int]struct{}
	sentUserText        map[int]map[int]int
	sentThoughts        map[int]int
	sentToolCalls       map[string]*toolCallRecord
	sentItems           map[int]map[int]struct{}
	sentToolPermissions map[ToolPermission]struct{}
	sentQuestions       map[int]struct{}
	sentTitle           string
	askedQuestion       *AskedQuestion
	turnOverlay         *cortex_go_proto.CascadeConfig
	consumedUsageSteps  map[int]struct{}
	pendingStepUsage    map[int]messages.Usage
	occupancy           contextUsage
	nextStepIdx         int
	minTurnStepIdx      int
	turnCancelled       bool
	cancelsInFlight     int
	cancelsDone         chan struct{}
	closed              bool
	turn                *Turn
	openInteractions    map[*clientInteraction]struct{}
	skippedHistory      int
	interactionCtx      context.Context
	cancelInteraction   context.CancelFunc
	mode                types.AgentMode
	cancelGrace         time.Duration
	streamMu            sync.Mutex
	stream              *Stream
}

type toolCallRecord struct {
	fingerprint [32]uint8
	name        string
	title       string
	pending     bool
	swept       bool
}

type contextUsage struct {
	known        bool
	tokens       int64
	step         int
	reported     bool
	reportedUsed int64
	reportedSize int64
}

type ToolPermission struct {
	ToolCallID string
	Answered   int
}

type AskedQuestion struct {
	Spec *cortex_go_proto.AskQuestionInteractionSpec
	Step int
}

// Binary VA: 0x10231d9d0 | Size: 288 B | Source: third_party/jetski/cli/acp/session/state.go:L132
func (s *State) CloseStream() {
	s.streamMu.Lock()
	defer s.streamMu.Unlock()
	if s.stream != nil {
		s.stream.close()
		s.stream = nil
	}
}

// Binary VA: 0x10231daf0 | Size: 272 B | Source: third_party/jetski/cli/acp/session/state.go:L142
func (s *State) Mode() types.AgentMode {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mode
}

// Binary VA: 0x10231dc00 | Size: 336 B | Source: third_party/jetski/cli/acp/session/state.go:L152
func (s *State) SetMode(mode types.AgentMode) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := s.mode != mode
	s.mode = mode
	return changed
}

// Binary VA: 0x10231dd50 | Size: 240 B | Source: third_party/jetski/cli/acp/session/state.go:L168
func (s *State) RecordStepSeen(stepIdx int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordStepSeenWithMuHeld(stepIdx)
}

func (s *State) recordStepSeenWithMuHeld(stepIdx int) {
	if next := stepIdx + 1; s.nextStepIdx < next {
		s.nextStepIdx = next
	}
}

// Binary VA: 0x10231de40 | Size: 256 B | Source: third_party/jetski/cli/acp/session/state.go:L177
func (s *State) SkipHistory(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.skippedHistory = n
	if n > 0 && s.nextStepIdx < n {
		s.nextStepIdx = n
	}
}

// Binary VA: 0x10231df40 | Size: 240 B | Source: third_party/jetski/cli/acp/session/state.go:L189
func (s *State) IsSkippedHistory(stepIdx int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return stepIdx < s.skippedHistory
}

// Binary VA: 0x10231e030 | Size: 368 B | Source: third_party/jetski/cli/acp/session/state.go:L198
func (s *State) SetTurnOverlay(overlay *cortex_go_proto.CascadeConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if overlay == nil {
		s.turnOverlay = nil
		return
	}
	s.turnOverlay = proto.Clone(overlay).(*cortex_go_proto.CascadeConfig)
}

// Binary VA: 0x10231e1a0 | Size: 336 B | Source: third_party/jetski/cli/acp/session/state.go:L209
func (s *State) TurnOverlay() *cortex_go_proto.CascadeConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.turnOverlay == nil {
		return nil
	}
	return proto.Clone(s.turnOverlay).(*cortex_go_proto.CascadeConfig)
}

// Binary VA: 0x10231e2f0 | Size: 240 B | Source: third_party/jetski/cli/acp/session/state.go:L235
func (s *State) MarkTurnCancelled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.markTurnCancelledWithMuHeld()
}

// Binary VA: 0x10231e3e0 | Size: 272 B | Source: third_party/jetski/cli/acp/session/state.go:L243
func (s *State) BeginTurnCancel() (bool, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hadTurn := s.markTurnCancelledWithMuHeld()
	release := s.holdTurnWithMuHeld()
	return hadTurn, release
}

// Binary VA: 0x10231e4f0 | Size: 272 B | Source: third_party/jetski/cli/acp/session/state.go:L251
func (s *State) BeginClose() (bool, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	hadTurn := s.markTurnCancelledWithMuHeld()
	release := s.holdTurnWithMuHeld()
	return hadTurn, release
}

// Binary VA: 0x10231e600 | Size: 192 B | Source: third_party/jetski/cli/acp/session/state.go:L260
func (s *State) markTurnCancelledWithMuHeld() bool {
	turn := s.turn
	s.turnCancelled = true
	s.askedQuestion = nil
	if s.cancelInteraction != nil {
		s.cancelInteraction()
		s.cancelInteraction = nil
	}
	clear(s.openInteractions)
	return turn != nil
}

// Binary VA: 0x10231e6c0 | Size: 400 B | Source: third_party/jetski/cli/acp/session/state.go:L289
func (s *State) InteractionContext() (context.Context, context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.turnCancelled {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx, cancel
	}
	if s.cancelInteraction == nil {
		s.interactionCtx, s.cancelInteraction = context.WithCancel(context.Background())
	}
	return s.interactionCtx, s.cancelInteraction
}

// Binary VA: 0x10231e850 | Size: 240 B | Source: third_party/jetski/cli/acp/session/state.go:L304
func (s *State) Closed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// Binary VA: 0x10231e940 | Size: 256 B | Source: third_party/jetski/cli/acp/session/state.go:L313
func (s *State) CancelledWhileOpen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turnCancelled && !s.closed
}

// Binary VA: 0x10231ea40 | Size: 240 B | Source: third_party/jetski/cli/acp/session/state.go:L320
func (s *State) TurnCancelled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turnCancelled
}

// Binary VA: 0x10231eb30 | Size: 352 B | Source: third_party/jetski/cli/acp/session/state.go:L328
func (s *State) MarkStepUsageConsumed(stepIdx int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordStepSeenWithMuHeld(stepIdx)
	if s.consumedUsageSteps == nil {
		s.consumedUsageSteps = make(map[int]struct{})
	}
	s.consumedUsageSteps[stepIdx] = struct{}{}
	delete(s.pendingStepUsage, stepIdx)
}

// Binary VA: 0x10231ec90 | Size: 544 B | Source: third_party/jetski/cli/acp/session/state.go:L342
func (s *State) RecordStepUsage(stepIdx int, stats *codeium_common_go_proto.ModelUsageStats) bool {
	usage, ok := usageFromModelUsageStats(stats)
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordStepSeenWithMuHeld(stepIdx)
	if _, consumed := s.consumedUsageSteps[stepIdx]; consumed {
		return false
	}
	if s.minTurnStepIdx >= 0 && stepIdx <= s.minTurnStepIdx {
		return false
	}
	if s.pendingStepUsage == nil {
		s.pendingStepUsage = make(map[int]messages.Usage)
	}
	s.pendingStepUsage[stepIdx] = usage
	return true
}

// Binary VA: 0x10231eeb0 | Size: 256 B | Source: third_party/jetski/cli/acp/session/state.go:L382
func (s *State) MarkContextEmpty() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.occupancy.known {
		s.occupancy = contextUsage{
			known: true,
			step:  -1,
		}
	}
}

// Binary VA: 0x10231efb0 | Size: 368 B | Source: third_party/jetski/cli/acp/session/state.go:L395
func (s *State) RecordContextUsage(stepIdx int, stats *codeium_common_go_proto.ModelUsageStats) {
	usage, ok := usageFromModelUsageStats(stats)
	if !ok || usage.TotalTokens <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.occupancy.known && stepIdx < s.occupancy.step {
		return
	}
	s.occupancy.known = true
	s.occupancy.step = stepIdx
	s.occupancy.tokens = usage.TotalTokens
}

// Binary VA: 0x10231f120 | Size: 432 B | Source: third_party/jetski/cli/acp/session/state.go:L414
func (s *State) UsageToReport(contextSize int64) (int64, bool) {
	if contextSize <= 0 {
		return 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.occupancy.known {
		return 0, false
	}
	if s.occupancy.reported && s.occupancy.reportedUsed == s.occupancy.tokens && s.occupancy.reportedSize == contextSize {
		return 0, false
	}
	s.occupancy.reported = true
	s.occupancy.reportedUsed = s.occupancy.tokens
	s.occupancy.reportedSize = contextSize
	return s.occupancy.tokens, true
}

// Binary VA: 0x10231f2d0 | Size: 544 B | Source: third_party/jetski/cli/acp/session/state.go:L435
func (s *State) consumeTurnUsageWithMuHeld() *messages.Usage {
	if len(s.pendingStepUsage) == 0 {
		return nil
	}
	if s.consumedUsageSteps == nil {
		s.consumedUsageSteps = make(map[int]struct{}, len(s.pendingStepUsage))
	}
	total := new(messages.Usage)
	for stepIdx, u := range s.pendingStepUsage {
		if s.minTurnStepIdx >= 0 && stepIdx <= s.minTurnStepIdx {
			continue
		}
		total.InputTokens += u.InputTokens
		total.OutputTokens += u.OutputTokens
		total.CachedReadTokens += u.CachedReadTokens
		total.CachedWriteTokens += u.CachedWriteTokens
		total.ThoughtTokens += u.ThoughtTokens
		total.TotalTokens += u.TotalTokens
		s.consumedUsageSteps[stepIdx] = struct{}{}
	}
	clear(s.pendingStepUsage)
	if total.TotalTokens <= 0 && total.InputTokens <= 0 && total.OutputTokens <= 0 && total.CachedReadTokens <= 0 && total.CachedWriteTokens <= 0 && total.ThoughtTokens <= 0 {
		return nil
	}
	return total
}

// Binary VA: 0x10231f4f0 | Size: 480 B | Source: third_party/jetski/cli/acp/session/state.go:L463
func (s *State) PendingText(stepIdx int, text string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordStepSeenWithMuHeld(stepIdx)
	if s.sentText == nil {
		s.sentText = make(map[int]int)
	}
	prev := s.sentText[stepIdx]
	if len(text) <= prev {
		return ""
	}
	s.sentText[stepIdx] = len(text)
	return text[prev:]
}

// Binary VA: 0x10231f6d0 | Size: 304 B | Source: third_party/jetski/cli/acp/session/state.go:L482
func (s *State) AgentTextPrefix(stepIdx int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	prefix := ""
	if s.hasLastText && s.lastTextStep != stepIdx {
		prefix = "\n\n"
	}
	s.lastTextStep = stepIdx
	s.hasLastText = true
	return prefix
}

// Binary VA: 0x10231f800 | Size: 208 B | Source: third_party/jetski/cli/acp/session/state.go:L495
func (s *State) MarkUserMessage() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hasLastText = false
}

// Binary VA: 0x10231f8d0 | Size: 304 B | Source: third_party/jetski/cli/acp/session/state.go:L504
func (s *State) MarkOwnInput(stepIdx int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ownInputs == nil {
		s.ownInputs = make(map[int]struct{})
	}
	s.ownInputs[stepIdx] = struct{}{}
}

// Binary VA: 0x10231fa00 | Size: 256 B | Source: third_party/jetski/cli/acp/session/state.go:L514
func (s *State) IsOwnInput(stepIdx int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.ownInputs[stepIdx]
	return ok
}

// Binary VA: 0x10231fb00 | Size: 640 B | Source: third_party/jetski/cli/acp/session/state.go:L522
func (s *State) PendingUserInputText(stepIdx int, itemIdx int, text string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordStepSeenWithMuHeld(stepIdx)
	if s.sentUserText == nil {
		s.sentUserText = make(map[int]map[int]int)
	}
	if s.sentUserText[stepIdx] == nil {
		s.sentUserText[stepIdx] = make(map[int]int)
	}
	prev := s.sentUserText[stepIdx][itemIdx]
	if len(text) <= prev {
		return ""
	}
	s.sentUserText[stepIdx][itemIdx] = len(text)
	return text[prev:]
}

// Binary VA: 0x10231fd80 | Size: 480 B | Source: third_party/jetski/cli/acp/session/state.go:L541
func (s *State) PendingThoughts(stepIdx int, text string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordStepSeenWithMuHeld(stepIdx)
	if s.sentThoughts == nil {
		s.sentThoughts = make(map[int]int)
	}
	prev := s.sentThoughts[stepIdx]
	if len(text) <= prev {
		return ""
	}
	s.sentThoughts[stepIdx] = len(text)
	return text[prev:]
}

// Binary VA: 0x10231ff60 | Size: 848 B | Source: third_party/jetski/cli/acp/session/state.go:L579
func (s *State) RecordToolCall(id, name, status, title string, fingerprint [32]uint8) (bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	terminal := status == "completed" || status == "failed"
	rec, ok := s.sentToolCalls[id]
	if ok && (rec.fingerprint == fingerprint || (rec.swept && !terminal)) {
		return false, false
	}
	if s.sentToolCalls == nil {
		s.sentToolCalls = make(map[string]*toolCallRecord)
	}
	rec = &toolCallRecord{
		fingerprint: fingerprint,
		name:        name,
		title:       title,
		pending:     status == "pending",
	}
	s.sentToolCalls[id] = rec
	return !ok, true
}

// Binary VA: 0x1023202b0 | Size: 752 B | Source: third_party/jetski/cli/acp/session/state.go:L604
func (s *State) SweepPendingToolCalls() []messages.ToolCallUpdateSessionUpdate {
	s.mu.Lock()
	defer s.mu.Unlock()
	var updates []messages.ToolCallUpdateSessionUpdate
	for id, rec := range s.sentToolCalls {
		if !rec.pending {
			continue
		}
		rec.pending = false
		rec.swept = true
		updates = append(updates, messages.ToolCallUpdateSessionUpdate{
			ToolCallID: id,
			Name:       rec.name,
			Status:     "failed",
			Title:      rec.title,
		})
	}
	slices.SortFunc(updates, func(a, b messages.ToolCallUpdateSessionUpdate) int {
		// Binary VA: 0x102326750 | Size: 64 B | Source: third_party/jetski/cli/acp/session/state.go:L615
		return cmp.Compare(a.ToolCallID, b.ToolCallID)
	})
	return updates
}

// Binary VA: 0x1023205a0 | Size: 576 B | Source: third_party/jetski/cli/acp/session/state.go:L622
func (s *State) SetPendingItem(stepIdx, itemIdx int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordStepSeenWithMuHeld(stepIdx)
	if s.sentItems == nil {
		s.sentItems = make(map[int]map[int]struct{})
	}
	if s.sentItems[stepIdx] == nil {
		s.sentItems[stepIdx] = make(map[int]struct{})
	}
	if _, exists := s.sentItems[stepIdx][itemIdx]; exists {
		return false
	}
	s.sentItems[stepIdx][itemIdx] = struct{}{}
	return true
}

// Binary VA: 0x1023207e0 | Size: 400 B | Source: third_party/jetski/cli/acp/session/state.go:L650
func (s *State) SetPendingToolPermission(perm ToolPermission) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sentToolPermissions == nil {
		s.sentToolPermissions = make(map[ToolPermission]struct{})
	}
	if _, exists := s.sentToolPermissions[perm]; exists {
		return false
	}
	s.sentToolPermissions[perm] = struct{}{}
	return true
}

// Binary VA: 0x102320970 | Size: 416 B | Source: third_party/jetski/cli/acp/session/state.go:L664
func (s *State) SetPendingQuestion(stepIdx int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordStepSeenWithMuHeld(stepIdx)
	if s.sentQuestions == nil {
		s.sentQuestions = make(map[int]struct{})
	}
	if _, exists := s.sentQuestions[stepIdx]; exists {
		return false
	}
	s.sentQuestions[stepIdx] = struct{}{}
	return true
}

// Binary VA: 0x102320b10 | Size: 384 B | Source: third_party/jetski/cli/acp/session/state.go:L679
func (s *State) SetPendingTitle(title string) bool {
	if title == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sentTitle == title {
		return false
	}
	s.sentTitle = title
	return true
}

// Binary VA: 0x102320c90 | Size: 240 B | Source: third_party/jetski/cli/acp/session/state.go:L693
func (s *State) HasAskedQuestion() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.askedQuestion != nil
}

// Binary VA: 0x102320d80 | Size: 240 B | Source: third_party/jetski/cli/acp/session/state.go:L700
func (s *State) AskedQuestion() *AskedQuestion {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.askedQuestion
}

// Binary VA: 0x102320e70 | Size: 400 B | Source: third_party/jetski/cli/acp/session/state.go:L709
func (s *State) SetAskedQuestion(stepIdx int, spec *cortex_go_proto.AskQuestionInteractionSpec) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordStepSeenWithMuHeld(stepIdx)
	if s.turnCancelled {
		return false
	}
	s.askedQuestion = &AskedQuestion{
		Spec: spec,
		Step: stepIdx,
	}
	return true
}

// Binary VA: 0x102321000 | Size: 272 B | Source: third_party/jetski/cli/acp/session/state.go:L725
func (s *State) ClearAskedQuestionAt(stepIdx int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.askedQuestion != nil && s.askedQuestion.Step == stepIdx {
		s.askedQuestion = nil
	}
}

// Binary VA: 0x102321110 | Size: 240 B | Source: third_party/jetski/cli/acp/session/state.go:L734
func (s *State) ClearAskedQuestion() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.askedQuestion = nil
}
