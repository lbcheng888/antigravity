// Reconstructed from agy v1.5.10 binary evidence (disassembly + Go type table).
// Binary VA: 0x102f8db70 | Size: 38 funcs (first func GetAgentCustomizations, 96 B) | Source: third_party/jetski/cli/acp/server/session_handlers.go:L45
// Binary source: third_party/jetski/cli/acp/server/session_handlers.go
// Reconstructed from agy v1.5.10 binary evidence.

package server

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"sync"
	"time"

	"google3/base/go/log"
	"google3/third_party/golang/protobuf/v2/proto/proto"
	"google3/third_party/jetski/cli/acp/jsonrpc/jsonrpc"
	"google3/third_party/jetski/cli/acp/mcp/mcp"
	"google3/third_party/jetski/cli/acp/schema/schema"
	"google3/third_party/jetski/cli/acp/session/session"
	"google3/third_party/jetski/cli/acp/sessionconfig/sessionconfig"
	"google3/third_party/jetski/cli/backend/backend"
	"google3/third_party/jetski/cortex/config/config"
	"google3/third_party/jetski/cortex_pb/cortex_go_proto"
	"google3/third_party/jetski/fs/local/local"
)

// Binary VA: 0x10a392208 | Size: 8 B
type backendCatalog struct {
	s *Server
}

// Binary VA: 0x102f8db70 | Size: 96 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L45
func (c backendCatalog) GetAgentCustomizations() []*cortex_go_proto.AgentCustomization {
	b := c.s.currentBackend()
	if b == nil {
		return nil
	}
	return b.GetAgentCustomizations()
}

// Binary VA: 0x102f8dbd0 | Size: 96 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L54
func (c backendCatalog) GetModelConfig() *backend.ModelConfig {
	b := c.s.currentBackend()
	if b == nil {
		return nil
	}
	return b.GetModelConfig()
}

// Binary VA: 0x102f8dc30 | Size: 640 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L69
func (s *Server) resolveAgentCustomization(ctx context.Context /* inferred */, name string) *cortex_go_proto.AgentCustomization {
	if name == "" || name == "default" {
		return nil
	}
	s.mu.Lock()
	cat := s.catalog
	s.mu.Unlock()
	if cat == nil {
		return nil
	}
	for _, custom := range cat.GetAgentCustomizations() {
		// [unrecovered] compare customization name against name
		if custom != nil && custom.GetName() == name /* inferred */ {
			return custom
		}
	}
	log.ctxlogf(ctx, 2 /* inferred */, "Agent %q not found, falling back to default", name)
	return nil
}

// Binary VA: 0x102f8deb0 | Size: 1328 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L86
func (s *Server) handleSessionNew(ctx context.Context, req *jsonrpc.Request) {
	var params schema.NewSessionParams
	if !s.parseParams(req, &params) {
		return
	}
	if !s.openSession(ctx, req.ID, params) {
		return
	}
	if err := s.checkSessionCapacity(""); err != nil {
		s.conn.writeMessage(&jsonrpc.Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   err,
		})
		return
	}
	s.applySessionEnvironment(ctx, params)
	s.preloadMCPServers(ctx, params)

	b := s.currentBackend()
	agent := s.conversationAgent(params)
	sessionID, err := b.StartConversation(agent /* [unrecovered] extra args */)
	if err != nil {
		s.conn.writeMessage(&jsonrpc.Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &jsonrpc.Error{
				Code:    -32603,
				Message: fmt.Sprintf("Failed to start %s conversation", s.productLabel() /* inferred */, err),
			},
		})
		return
	}

	st := &session.State{} /* inferred */
	st.MarkContextEmpty()
	sysPrompt := extractAntigravitySystemPrompt(params.Meta)
	st.SetTurnOverlay(mcpAndProjectConfig(s.clientMCPSpecs(params), s.collectProjectDirs(params), sysPrompt))

	s.mu.Lock()
	s.sessionStates[sessionID] = st
	s.mu.Unlock()

	s.attachTurn(sessionID, st, nil)
	s.completeSessionSetup(ctx, req.ID, sessionID, st)
	s.recordSessionStarted("new", sessionID, st)
}

// Binary VA: 0x102f8e3e0 | Size: 272 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L131
func (s *Server) effectiveModelConfig() *backend.ModelConfig {
	s.mu.Lock()
	cat := s.catalog
	s.mu.Unlock()
	if cat == nil {
		return nil
	}
	return cat.GetModelConfig()
}

// Binary VA: 0x102f8e4f0 | Size: 1632 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L138
func (s *Server) completeSessionSetup(ctx context.Context, id *jsontext.Value /* inferred */, sessionID string, st *session.State) {
	s.ensureDefaultModelOverride()
	modelCfg := s.effectiveModelConfig()

	var models *schema.SessionModelState
	if modelCfg != nil && len(modelCfg.Models /* inferred */) > 0 {
		available := make([]schema.ModelInfo, 0, len(modelCfg.Models /* inferred */))
		for _, m := range modelCfg.Models /* inferred */ {
			modelID := sessionconfig.ModelOptionID(m /* inferred */)
			available = append(available, schema.ModelInfo{
				ModelID:     modelID,
				Name:        m.Name /* inferred */,
				Description: m.Description /* inferred */,
			})
		}
		currentID := s.currentModelID(sessionID, st, modelCfg /* inferred */)
		models = &schema.SessionModelState{
			AvailableModels: available,
			CurrentModelID:  currentID,
		}
		log.ctxlogf(ctx, 1 /* inferred */, "Advertising available models to client: current=%s, count=%d", currentID, len(available))
	}

	mode := s.sessionMode(st)
	autoAvail := s.autoModeAvailable()
	// [unrecovered] build schema.SessionMode slice from modes.go helpers (checks "auto" mode)
	modes := &schema.SessionModeState{
		CurrentModeID:  string(mode),
		AvailableModes: []schema.SessionMode{}, /* inferred */
	}
	_ = autoAvail

	configOpts := s.buildConfigOptions(sessionID, st)
	meta := s.tierMeta()

	res := schema.NewSessionResult{
		SessionID:     sessionID,
		Models:        models,
		Modes:         modes,
		ConfigOptions: configOpts,
		Meta:          meta,
	}
	s.conn.writeMessage(&jsonrpc.Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  res,
	})
	s.sendAvailableCommands(ctx, sessionID)
	s.sendUsageUpdate(sessionID)
}

// Binary VA: 0x102f8eb50 | Size: 336 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L176
func (s *Server) sessionOverlay(params schema.NewSessionParams) *cortex_go_proto.CascadeConfig {
	specs := s.sessionMCPSpecs(params)
	dirs := s.collectProjectDirs(params)
	sysPrompt := extractAntigravitySystemPrompt(params.Meta)
	return mcpAndProjectConfig(specs, dirs, sysPrompt)
}

// Binary VA: 0x102f8eca0 | Size: 176 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L182
func (s *Server) processOverlay() *cortex_go_proto.CascadeConfig {
	var specs []*cortex_go_proto.McpServerSpec
	if ideSpec := mcp.IDEPortSpec(s.params.IDEMCPPort); ideSpec != nil {
		specs = []*cortex_go_proto.McpServerSpec{ideSpec}
	}
	return mcpAndProjectConfig(specs, nil, "")
}

// Binary VA: 0x102f8ed50 | Size: 320 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L190
func (s *Server) prebootedWithoutSessionOverlay(overlay *cortex_go_proto.CascadeConfig) bool {
	s.bootMu.Lock()
	defer s.bootMu.Unlock()
	if !s.booted {
		return false
	}
	if !proto.Equal(s.bootOverlay, overlay) {
		return false
	}
	return s.currentBackend() != nil
}

// Binary VA: 0x102f8ee90 | Size: 1072 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L201
func (s *Server) conversationAgent(params schema.NewSessionParams) *cortex_go_proto.AgentCustomization {
	specs := s.clientMCPSpecs(params)
	dirs := s.collectProjectDirs(params)
	sysPrompt := extractAntigravitySystemPrompt(params.Meta)
	overlay := mcpAndProjectConfig(specs, dirs, sysPrompt)

	var mcpSpecs []*cortex_go_proto.McpServerSpec
	if !s.prebootedWithoutSessionOverlay(overlay) {
		mcpSpecs = specs
	}

	if custom := s.resolveAgentCustomization(context.Background() /* inferred */, s.params.Agent); custom != nil {
		return sessionCustomAgent(custom, mcpSpecs, overlay)
	}

	// [unrecovered] construct default BuiltinAgentConfig_DefaultAgent + CustomAgentSpec_builder + AgentScriptItem_builder + AgentCustomization
	return &cortex_go_proto.AgentCustomization{}
}

// Binary VA: 0x102f8f2c0 | Size: 1712 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L229
func sessionCustomAgent(base *cortex_go_proto.AgentCustomization, mcpSpecs []*cortex_go_proto.McpServerSpec, overlay *cortex_go_proto.CascadeConfig) *cortex_go_proto.AgentCustomization {
	cloned := proto.Clone(base).(*cortex_go_proto.AgentCustomization)
	// [unrecovered] iterate cloned script items; if AgentScriptItem_ConfigPath resolve via config.AgentScriptStaticConfig(local.NewFS(), ...) logging "Failed to resolve static agent config from config path %v: %v"; if AgentScriptItem_Config wrap CustomAgentSpec_CodingAgent / CodingAgentConfig, append mcpSpecs, proto.Merge CascadeConfig, set CodeSearchToolConfig
	_ = local.NewFS
	_ = config.AgentScriptStaticConfig
	_ = proto.Merge
	return cloned
}

// Binary VA: 0x102f8f970 | Size: 1280 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L287
func mcpAndProjectConfig(mcpSpecs []*cortex_go_proto.McpServerSpec, projectDirs []string, systemPrompt string) *cortex_go_proto.CascadeConfig {
	// [unrecovered] build CascadePlannerConfig with McpDiscoveryConfig, CustomizationDiscoveryConfig_builder, PromptSectionCustomizationConfig (name="acp_system_prompt" if systemPrompt != ""), CascadeConfig_builder.Build
	_ = cortex_go_proto.CascadePlannerConfig{}
	_ = cortex_go_proto.McpDiscoveryConfig{}
	_ = cortex_go_proto.PromptSection{}
	_ = cortex_go_proto.PromptSectionCustomizationConfig{}
	return nil
}

// Binary VA: 0x102f8fe70 | Size: 320 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L323
func (s *Server) sessionMCPSpecs(params schema.NewSessionParams) []*cortex_go_proto.McpServerSpec {
	specs := s.clientMCPSpecs(params)
	if ideSpec := mcp.IDEPortSpec(s.params.IDEMCPPort); ideSpec != nil {
		specs = append(specs, ideSpec)
	}
	return specs
}

// Binary VA: 0x102f8ffb0 | Size: 576 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L333
func (s *Server) clientMCPSpecs(params schema.NewSessionParams) []*cortex_go_proto.McpServerSpec {
	var specs []*cortex_go_proto.McpServerSpec
	if ideSpec := mcp.IDEPortSpec(s.params.IDEMCPPort); ideSpec != nil {
		s.mcpNames.Assign(ideSpec /* inferred */)
	}
	for _, cfg := range params.MCPServerList() {
		spec := mcp.FromConfig(cfg)
		if spec != nil {
			s.mcpNames.Assign(spec /* inferred */)
			specs = append(specs, spec)
		}
	}
	return specs
}

// Binary VA: 0x102f901f0 | Size: 752 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L345
func (s *Server) applySessionEnvironment(ctx context.Context, params schema.NewSessionParams) {
	b := s.currentBackend()
	if b == nil {
		return
	}
	if params.Cwd != "" {
		if err := b.AddWorkspaceDir(params.Cwd); err != nil {
			log.ctxlogf(ctx, 1 /* inferred */, "Failed to add workspace directory %s (it might already be tracked): %v", params.Cwd, err)
		}
	}
	for _, dir := range params.AdditionalDirs() {
		if err := b.AddWorkspaceDir(dir); err != nil {
			log.ctxlogf(ctx, 1 /* inferred */, "Failed to add additional workspace directory %s: %v", dir, err)
		}
	}
	if params.DisableBuiltInTools() {
		if s.params.Agent == "" || s.params.Agent == "default" {
			log.ctxlogf(ctx, 1 /* inferred */, "Session asked to disable built-in tools; not supported by the default agent yet, keeping them")
		}
	}
}

// Binary VA: 0x102f904e0 | Size: 704 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L368
func (s *Server) preloadMCPServers(ctx context.Context, params schema.NewSessionParams) {
	specs := s.sessionMCPSpecs(params)
	if len(specs) == 0 {
		return
	}
	if s.params.MCPManager == nil {
		log.ctxlogf(ctx, 1 /* inferred */, "MCPManager is nil, cannot preload %d MCP servers", len(specs))
		return
	}
	log.ctxlogf(ctx, 1 /* inferred */, "Preloading %d MCP servers of the session", len(specs))
	loadCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.params.MCPManager.LoadFromSpecs(loadCtx, specs /* [unrecovered] extra args */); err != nil {
		log.ctxlogf(ctx, 1 /* inferred */, "Failed to preload the session's MCP servers: %v", err)
	}
}

// Binary VA: 0x102f907a0 | Size: 416 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L385
func (s *Server) collectProjectDirs(params schema.NewSessionParams) []string {
	// [unrecovered] deduplicate/collect s.params.WorkingDir / params.Cwd / s.params.AddDirs / params.AdditionalDirs()
	var dirs []string
	if params.Cwd != "" {
		dirs = append(dirs, params.Cwd)
	}
	dirs = append(dirs, params.AdditionalDirs()...)
	return dirs
}

// Binary VA: 0x102f90940 | Size: 2576 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L406
func (s *Server) handleSessionLoad(ctx context.Context, req *jsonrpc.Request) {
	var params struct {
		schema.NewSessionParams
		SessionID      string `json:"sessionId"`
		SessionIDSnake string `json:"session_id"`
	}
	if !s.parseParams(req, &params) {
		return
	}
	sessionID := strings.TrimSpace(params.SessionID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(params.SessionIDSnake)
	}
	if sessionID == "" {
		s.conn.writeMessage(&jsonrpc.Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &jsonrpc.Error{
				Code:    -32602,
				Message: "Missing sessionId",
			},
		})
		return
	}
	if !s.openSession(ctx, req.ID, params.NewSessionParams) {
		return
	}
	if err := s.reserveSession(ctx, sessionID); err != nil {
		s.conn.writeMessage(&jsonrpc.Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   err,
		})
		return
	}

	overlay := s.sessionOverlay(params.NewSessionParams)
	var turnOverlay *cortex_go_proto.CascadeConfig
	if !s.prebootedWithoutSessionOverlay(overlay) {
		specs := s.clientMCPSpecs(params.NewSessionParams)
		dirs := s.collectProjectDirs(params.NewSessionParams)
		sysPrompt := extractAntigravitySystemPrompt(params.Meta)
		turnOverlay = mcpAndProjectConfig(specs, dirs, sysPrompt)
	}

	s.mu.Lock()
	if st := s.sessionStates[sessionID]; st != nil {
		st.SetTurnOverlay(turnOverlay)
	}
	s.mu.Unlock()

	log.ctxlogf(ctx, 1 /* inferred */, "Loading/Resuming session: %s, cwd: %s", sessionID, params.Cwd)
	s.applySessionEnvironment(ctx, params.NewSessionParams)
	s.preloadMCPServers(ctx, params.NewSessionParams)

	b := s.currentBackend()
	if b == nil {
		return
	}
	s.replayHistory(ctx, b, sessionID)

	detail := b.GetConversationDetail(sessionID)
	if detail == nil {
		s.mu.Lock()
		delete(s.sessionStates, sessionID)
		s.mu.Unlock()
		s.conn.writeMessage(&jsonrpc.Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &jsonrpc.Error{
				Code:    -32603,
				Message: fmt.Sprintf("Failed to read session %s; retry, or load it with session/load", sessionID),
			},
		})
		return
	}

	if detail.NumSteps == 0 {
		s.mu.Lock()
		if st := s.sessionStates[sessionID]; st != nil {
			st.MarkContextEmpty()
		}
		s.mu.Unlock()
	}

	s.mu.Lock()
	if st := s.sessionStates[sessionID]; st != nil {
		st.SkipHistory(detail.NumSteps /* inferred */)
	}
	s.mu.Unlock()

	st := s.sessionState(sessionID)
	s.attachTurn(sessionID, st, nil)
	s.completeSessionSetup(ctx, req.ID, sessionID, st)
	s.recordSessionStarted("load", sessionID, st)
	s.sendSessionInfoUpdate(sessionID, detail.Title)
}

// Binary VA: 0x102f91350 | Size: 720 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L501
func (s *Server) replayHistory(ctx context.Context, b *backend.ServerBackend, sessionID string) {
	done := make(chan bool, 1)
	stop := b.StreamConversationUpdates(sessionID, backend.StreamCallbacks{
		// Binary VA: 0x102f990c0 | Size: 192 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L504
		OnSteps: func(indices []int, steps []*gemini_coder_go_proto.Step) {
			s.sendHistoricalStepUpdates(sessionID, indices, steps)
		},
		// Binary VA: 0x102f99060 | Size: 96 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L509
		OnFullyIdle: func(idle bool) {
			if idle {
				select {
				case done <- true:
				default:
				}
			}
		},
	})
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
		time.Sleep(100 * time.Millisecond)
	case <-timer.C:
		log.ctxlogf(ctx, 1 /* inferred */, "Timeout waiting for history stream for session %s", sessionID)
	case <-ctx.Done():
	}
	if stop != nil {
		stop()
	}
}

// Binary VA: 0x102f91620 | Size: 496 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L530
func (s *Server) openSession(ctx context.Context, id *jsontext.Value /* inferred */, params schema.NewSessionParams) bool {
	overlay := s.sessionOverlay(params)
	if err := s.ensureBackendWithOptions(ctx, bootOptions{overlay: overlay}); err != nil {
		s.conn.writeMessage(&jsonrpc.Response{
			JSONRPC: "2.0",
			ID:      id,
			Error: &jsonrpc.Error{
				Code:    -32603,
				Message: fmt.Sprintf("Failed to start %s: %v", s.productLabel() /* inferred */, err),
			},
		})
		return false
	}
	return s.requireAuth(ctx, id)
}

// Binary VA: 0x102f91810 | Size: 592 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L542
func (s *Server) checkSessionCapacity(sessionID string) *jsonrpc.Error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.singleSession && len(s.sessionStates) > 0 {
		if _, ok := s.sessionStates[sessionID]; !ok {
			return &jsonrpc.Error{
				Code:    -32603,
				Message: fmt.Sprintf("This %s process already serves another session", s.productLabel() /* inferred */),
			}
		}
	}
	return nil
}

// Binary VA: 0x102f91a60 | Size: 960 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L554
func (s *Server) reserveSession(ctx context.Context, sessionID string) *jsonrpc.Error {
	if err := s.checkSessionCapacity(sessionID); err != nil {
		return err
	}
	s.mu.Lock()
	existing := s.sessionStates[sessionID]
	s.mu.Unlock()
	if existing != nil {
		s.closeSessionState(ctx, sessionID, existing)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionStates[sessionID] != nil {
		return &jsonrpc.Error{
			Code:    -32603,
			Message: fmt.Sprintf("Session %s was closed while it was being reopened", sessionID),
		}
	}
	st := &session.State{} /* inferred */
	if existing != nil {
		_ = existing.Mode()
	}
	s.sessionStates[sessionID] = st
	return nil
}

// Binary VA: 0x102f91e20 | Size: 544 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L586
func (s *Server) handleSessionCancel(ctx context.Context, req *jsonrpc.Request) {
	var params struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(*req.Params, &params); err != nil {
		return
	}
	s.mu.Lock()
	st := s.sessionStates[params.SessionID]
	s.mu.Unlock()
	if st == nil {
		return
	}
	if st.BeginTurnCancel() {
		st.FinishTurnAfter(5 * time.Second)
	}
	s.cancelBackend(ctx, params.SessionID)
}

// Binary VA: 0x102f92040 | Size: 304 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L610
func (s *Server) cancelBackend(ctx context.Context, sessionID string) {
	tb := s.turns()
	if tb == nil {
		return
	}
	if err := stopConversationTree(ctx, tb, sessionID); err != nil {
		log.ctxlogf(ctx, 2 /* inferred */, "Failed to cancel session %s: %v", sessionID, err)
	}
}

// Binary VA: 0x102f92170 | Size: 256 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L627
func (s *Server) closeSessionState(ctx context.Context, sessionID string, st *session.State) {
	if st == nil {
		return
	}
	st.BeginClose()
	st.CloseStream()
	st.FinishTurnAfter(5 * time.Second)
	s.cancelBackend(ctx, sessionID)
}

// Binary VA: 0x102f92270 | Size: 720 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L643
func (s *Server) handleSessionClose(ctx context.Context, req *jsonrpc.Request) {
	var params struct {
		SessionID string `json:"sessionId"`
	}
	if !s.parseParams(req, &params) {
		return
	}
	s.mu.Lock()
	st := s.sessionStates[params.SessionID]
	delete(s.sessionStates, params.SessionID)
	remaining := len(s.sessionStates)
	cb := s.onLastSessionClosed
	s.mu.Unlock()

	s.closeSessionState(ctx, params.SessionID, st)
	s.conn.writeMessage(&jsonrpc.Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  struct{}{},
	})
	if remaining == 0 && cb != nil {
		log.ctxlogf(ctx, 1 /* inferred */, "Last session %s closed", params.SessionID)
		go cb()
	}
}

// Binary VA: 0x102f92540 | Size: 288 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L668
func (s *Server) sessionState(sessionID string) *session.State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionStates[sessionID]
}

// Binary VA: 0x102f92660 | Size: 1056 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L674
func (s *Server) handleSessionList(ctx context.Context, req *jsonrpc.Request) {
	if !s.requireAuth(ctx, req.ID) {
		return
	}
	b := s.currentBackend()
	if b == nil {
		s.conn.writeMessage(&jsonrpc.Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  schema.ListSessionsResult{},
		})
		return
	}
	convs := b.ListConversations()
	var infos []schema.SessionInfo
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, c := range convs {
		wg.Add(1)
		// Binary VA: 0x102f99400 | Size: 928 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L691
		go func(summary backend.ConversationSummary) {
			defer wg.Add(-1)
			detail := b.GetConversationDetail(summary.ID)
			if detail == nil || detail.NumSteps == 0 {
				return
			}
			cwd := ""
			for _, uri := range detail.WorkspaceURIs {
				if strings.HasPrefix(uri, "file://") {
					cwd = strings.TrimPrefix(uri, "file://")
					break
				}
			}
			if cwd == "" {
				cwd = s.params.WorkingDir
			}
			title := cleanSessionTitle(detail.Title)
			if title == "" {
				shortID := summary.ID
				if len(shortID) > 8 {
					shortID = shortID[:8]
				}
				title = "Conversation " + shortID
			}
			updatedAt := detail.UpdatedAt.Format("2006-01-02T15:04:05Z07:00")
			mu.Lock()
			infos = append(infos, schema.SessionInfo{
				SessionID: summary.ID,
				Cwd:       cwd,
				Title:     title,
				UpdatedAt: updatedAt,
			})
			mu.Unlock()
		}(c)
	}
	wg.Wait()
	// Binary VA: 0x102f99260 | Size: 304 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L739
	sort.SliceStable(infos, func(i, j int) bool {
		di := pathDistance(s.params.WorkingDir, infos[i].Cwd)
		dj := pathDistance(s.params.WorkingDir, infos[j].Cwd)
		if di != dj {
			return di < dj
		}
		return infos[i].UpdatedAt > infos[j].UpdatedAt
	})
	if infos == nil {
		infos = []schema.SessionInfo{}
	}
	s.conn.writeMessage(&jsonrpc.Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  schema.ListSessionsResult{Sessions: infos},
	})
}

// Binary VA: 0x102f92a80 | Size: 464 B | Source: third_party/jetski/cli/acp/server/session_handlers.go:L754
func cleanSessionTitle(title string) string {
	// [unrecovered] split/trim lines, truncate to 80 chars + "...", fallback "Untitled Conversation" if empty
	if idx := strings.Index(title, "\n"); idx >= 0 {
		title = title[:idx]
	}
	title = strings.TrimSpace(title)
	if len(title) > 80 {
		return title[:80] + "..."
	}
	if title == "" {
		return "Untitled Conversation"
	}
	return title
}
