// Binary VA: 0x102f81880 | Source: third_party/jetski/cli/acp/server/config_options.go
package server

import (
	"context"
	"fmt"

	"google3/base/go/log"
	"google3/third_party/jetski/cli/acp/jsonrpc/jsonrpc"
	"google3/third_party/jetski/cli/acp/messages/messages"
	"google3/third_party/jetski/cli/acp/schema/schema"
	"google3/third_party/jetski/cli/acp/session/session"
	"google3/third_party/jetski/cli/acp/sessionconfig/sessionconfig"
	"google3/third_party/jetski/cli/backend/backend"
	"google3/third_party/jetski/proto/codeium_common_go_proto"
)

// Binary VA: 0x102f96a50 | Size: 320 B | Source: third_party/jetski/cli/acp/server/config_options.go:L21 (inlined setActiveModel)
func setActiveModel(modelID string) func(*backend.ModelConfig, codeium_common_go_proto.Model) (codeium_common_go_proto.Model, string, error) /* inferred */ {
	return func(cfg *backend.ModelConfig, cur codeium_common_go_proto.Model) (codeium_common_go_proto.Model, string, error) {
		return sessionconfig.ResolveModelSelection(cfg, cur, modelID)
	}
}

// Binary VA: 0x102f96b90 | Size: 320 B | Source: third_party/jetski/cli/acp/server/config_options.go:L27 (inlined setActiveThoughtLevel)
func setActiveThoughtLevel(level string) func(*backend.ModelConfig, codeium_common_go_proto.Model) (codeium_common_go_proto.Model, string, error) /* inferred */ {
	return func(cfg *backend.ModelConfig, cur codeium_common_go_proto.Model) (codeium_common_go_proto.Model, string, error) {
		return sessionconfig.ResolveThoughtLevelSelection(cfg, cur, level)
	}
}

// Binary VA: 0x102f81880 | Size: 1072 B | Source: third_party/jetski/cli/acp/server/config_options.go:L35
func (s *Server) selectSessionModel(
	ctx context.Context,
	sessionID string,
	resolve func(*backend.ModelConfig, codeium_common_go_proto.Model) (codeium_common_go_proto.Model, string, error), /* inferred */
) *jsonrpc.Error {
	if s.sessionState(sessionID) == nil {
		return &jsonrpc.Error{Code: -32602, Message: fmt.Sprintf("Session not found: %s", sessionID)}
	}
	cfg := s.effectiveModelConfig()
	cur := s.currentModel(sessionID)
	next, label, err := resolve(cfg, cur)
	if err != nil {
		return &jsonrpc.Error{Code: -32602, Message: err.Error()} /* inferred */
	}
	log.InfoContextf(ctx, "Switching model in session %s to: %s (enum: %v)", sessionID, label, next) /* inferred */
	s.currentBackend().SetConversationModel(sessionID, next)
	return nil
}

// Binary VA: 0x102f81cb0 | Size: 512 B | Source: third_party/jetski/cli/acp/server/config_options.go:L58
func (s *Server) handleSessionSetModel(ctx context.Context, req *jsonrpc.Request) {
	if !s.requireAuth(ctx, req) {
		return
	}
	var params struct {
		SessionID string `json:"sessionId"`
		ModelID   string `json:"modelId"`
	}
	if !s.parseParams(req, &params) {
		return
	}
	if rpcErr := s.selectSessionModel(ctx, params.SessionID, setActiveModel(params.ModelID)); rpcErr != nil {
		s.conn.writeMessage(jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Error: rpcErr})
		return
	}
	s.conn.writeMessage(jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Result: struct{}{}})
	s.sendConfigOptionUpdate(params.SessionID)
	s.sendAvailableCommands(ctx, params.SessionID)
	s.sendUsageUpdate(params.SessionID)
}

// Binary VA: 0x102f81eb0 | Size: 768 B | Source: third_party/jetski/cli/acp/server/config_options.go:L82
func (s *Server) currentModel(sessionID string) codeium_common_go_proto.Model {
	b := s.currentBackend()
	if b == nil {
		return 0
	}
	model := b.ConversationModel(sessionID)
	// [unrecovered] checks model against ModelOrAlias_Model and sessionconfig.ModelOptionID
	return model /* inferred */
}

// Binary VA: 0x102f821b0 | Size: 240 B | Source: third_party/jetski/cli/acp/server/config_options.go:L107
func (s *Server) currentModelID(sessionID string) string {
	return sessionconfig.ModelOptionID(s.currentModel(sessionID)) /* inferred */
}

// Binary VA: 0x102f822a0 | Size: 1040 B | Source: third_party/jetski/cli/acp/server/config_options.go:L115
func (s *Server) buildConfigOptions(sessionID string) []schema.ConfigOption {
	opts := sessionconfig.ModelOptions(s.effectiveModelConfig(), s.currentModel(sessionID))
	curMode := s.sessionMode(sessionID)
	autoAvail := s.autoModeAvailable()
	var modeValues []schema.ConfigOptionValue
	for _, m := range session.Modes() {
		id := m.ID
		if id == "" {
			id = "default"
		}
		if id == "auto" && !autoAvail && curMode != "auto" {
			continue
		}
		modeValues = append(modeValues, schema.ConfigOptionValue{
			Value:       id,
			Name:        m.Name,
			Description: m.Description,
		})
	}
	opts = append(opts, schema.ConfigOption{
		ID:           "mode",
		Name:         "Mode",
		Category:     "mode",
		Type:         "select",
		CurrentValue: curMode,
		Options:      modeValues,
	})
	return opts
}

// Binary VA: 0x102f826b0 | Size: 208 B | Source: third_party/jetski/cli/acp/server/config_options.go:L124
func (s *Server) sendConfigOptionUpdate(sessionID string) {
	s.conn.SendNotification("session/update", schema.SessionNotification{
		SessionID: sessionID,
		Update:    messages.ConfigOptionUpdateSessionUpdate{ConfigOptions: s.buildConfigOptions(sessionID)},
	})
}

// Binary VA: 0x102f82780 | Size: 1216 B | Source: third_party/jetski/cli/acp/server/config_options.go:L137
func (s *Server) handleSessionSetConfigOption(ctx context.Context, req *jsonrpc.Request) {
	if !s.requireAuth(ctx, req) {
		return
	}
	var params struct {
		SessionID string `json:"sessionId"`
		ConfigID  string `json:"configId"`
		Value     string `json:"value"`
	}
	if !s.parseParams(req, &params) {
		return
	}
	switch params.ConfigID {
	case "mode":
		if _, rpcErr := s.setSessionMode(ctx, params.SessionID, params.Value); rpcErr != nil {
			s.conn.writeMessage(jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Error: rpcErr})
			return
		}
	case "model":
		if rpcErr := s.selectSessionModel(ctx, params.SessionID, setActiveModel(params.Value)); rpcErr != nil {
			s.conn.writeMessage(jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Error: rpcErr})
			return
		}
	case "thought_level":
		if rpcErr := s.selectSessionModel(ctx, params.SessionID, setActiveThoughtLevel(params.Value)); rpcErr != nil {
			s.conn.writeMessage(jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Error: rpcErr})
			return
		}
	default:
		s.conn.writeMessage(jsonrpc.Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &jsonrpc.Error{Code: -32602, Message: fmt.Sprintf("Unknown config option: %s", params.ConfigID)},
		})
		return
	}
	opts := s.buildConfigOptions(params.SessionID)
	s.conn.writeMessage(jsonrpc.Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: struct {
			ConfigOptions []schema.ConfigOption `json:"configOptions"`
		}{ConfigOptions: opts},
	})
	if params.ConfigID == "mode" {
		s.sendModeUpdate(params.SessionID)
	}
	s.sendConfigOptionUpdate(params.SessionID)
	s.sendAvailableCommands(ctx, params.SessionID)
	s.sendUsageUpdate(params.SessionID)
}

// Binary VA: 0x102f82c40 | Size: 624 B | Source: third_party/jetski/cli/acp/server/config_options.go:L192
func (s *Server) ensureDefaultModelOverride() {
	b := s.currentBackend()
	if b == nil {
		return
	}
	cfg := s.effectiveModelConfig()
	if cfg == nil || len(cfg.ClientModels) == 0 {
		return
	}
	cur := b.GetSelectedModelOverride()
	for _, m := range cfg.ClientModels {
		if m.ModelOrAlias.GetModel() == cur.GetModel() {
			return
		}
	}
	if cfg.DefaultModel != nil {
		log.InfoContextf(nil, "Setting default model override: %s", cfg.DefaultModel.String()) /* inferred */
		b.SetSelectedModelOverride(cfg.DefaultModel.GetModel())
		return
	}
	b.SetSelectedModelOverride(cfg.ClientModels[0].ModelOrAlias.GetModel())
}
