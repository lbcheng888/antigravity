package acp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"

	"antigravity.dev/agy-sdk/pkg/aicode"
	"antigravity.dev/agy-sdk/pkg/auth"
	"antigravity.dev/agy-sdk/pkg/cortex"
)

// Streamer abstracts model streaming so ACP Server can run against *aicode.Client or mocks.
type Streamer interface {
	Stream(ctx context.Context, model string, req *aicode.GeminiRequest, onChunk func(*aicode.SSEChunk) error) error
}

// AICodeStreamer adapts *aicode.Client to the Streamer interface.
type AICodeStreamer struct {
	Client *aicode.Client
}

func (a AICodeStreamer) Stream(ctx context.Context, model string, req *aicode.GeminiRequest, onChunk func(*aicode.SSEChunk) error) error {
	return a.Client.StreamGenerateContent(ctx, model, req, onChunk)
}

// ServerConfig configures the ACP JSON-RPC stdio server.
type ServerConfig struct {
	Version                    string
	DefaultModel               string
	DefaultCwd                 string
	Auth                       *auth.ChainedAuth
	Streamer                   Streamer
	Models                     []ModelInfo
	ToolRegistry               *cortex.Registry
	DangerouslySkipPermissions bool
	MaxToolSteps               int
}

// Server implements Antigravity's ACP JSON-RPC 2.0 protocol over bidirectional streams.
type Server struct {
	cfg      ServerConfig
	mu       sync.RWMutex
	sessions map[string]*SessionState
	conn     *Conn
}

func NewServer(cfg ServerConfig) *Server {
	if cfg.Version == "" {
		cfg.Version = aicode.DefaultClientVersion
	}
	if cfg.DefaultModel == "" {
		cfg.DefaultModel = "gemini-3.5-pro"
	}
	if len(cfg.Models) == 0 {
		cfg.Models = []ModelInfo{
			{ModelID: cfg.DefaultModel, Name: cfg.DefaultModel},
			{ModelID: "gemini-4-hydrogen-medium", Name: "Gemini 4 Hydrogen (Medium)"},
			{ModelID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash"},
		}
	}
	return &Server{
		cfg:      cfg,
		sessions: make(map[string]*SessionState),
	}
}

// Serve runs the JSON-RPC server loop over r and w until EOF or ctx cancellation.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	stream := NewStream(r, w)
	conn := NewConn(stream, s.handleRequest)
	s.mu.Lock()
	s.conn = conn
	s.mu.Unlock()
	return conn.Serve(ctx)
}

func (s *Server) handleRequest(ctx context.Context, req *Request) {
	res, rpcErr := s.dispatch(ctx, req)
	if len(req.ID) == 0 || string(req.ID) == "null" {
		return
	}
	s.mu.RLock()
	conn := s.conn
	s.mu.RUnlock()
	if conn != nil {
		_ = conn.Reply(req.ID, res, rpcErr)
	}
}

func (s *Server) dispatch(ctx context.Context, req *Request) (any, *Error) {
	switch req.Method {
	case "initialize":
		var params InitializeParams
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return nil, &Error{Code: CodeInvalidParams, Message: err.Error()}
			}
		}
		return BuildInitializeResult(s.cfg.Version, DefaultAuthMethods()), nil

	case "authenticate":
		var params AuthenticateParams
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return nil, &Error{Code: CodeInvalidParams, Message: err.Error()}
			}
		}
		if s.cfg.Auth != nil {
			creds, err := s.cfg.Auth.Resolve(ctx)
			if err != nil {
				return nil, &Error{Code: -32001, Message: fmt.Sprintf("authentication required: %v", err)}
			}
			return AuthenticateResult{
				Authenticated: true,
				MethodID:      params.MethodID,
				AuthKind:      string(creds.Kind),
				Email:         creds.Email,
			}, nil
		}
		return AuthenticateResult{Authenticated: true, MethodID: params.MethodID}, nil

	case "session/new":
		var params NewSessionParams
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return nil, &Error{Code: CodeInvalidParams, Message: err.Error()}
			}
		}
		cwd := params.Cwd
		if cwd == "" {
			cwd = s.cfg.DefaultCwd
		}
		if cwd == "" {
			cwd, _ = os.Getwd()
		}
		id := newSessionID()
		st := NewSessionState(id, cwd, s.cfg.DefaultModel)
		s.mu.Lock()
		s.sessions[id] = st
		s.mu.Unlock()
		return s.sessionResult(st), nil

	case "session/load":
		var params LoadSessionParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, &Error{Code: CodeInvalidParams, Message: err.Error()}
		}
		st, ok := s.getSession(params.SessionID)
		if !ok {
			return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("unknown session %q", params.SessionID)}
		}
		return s.sessionResult(st), nil

	case "session/list":
		s.mu.RLock()
		infos := make([]SessionInfo, 0, len(s.sessions))
		for _, st := range s.sessions {
			infos = append(infos, st.Info())
		}
		s.mu.RUnlock()
		sort.Slice(infos, func(i, j int) bool { return infos[i].SessionID < infos[j].SessionID })
		return ListSessionsResult{Sessions: infos}, nil

	case "session/set_model":
		var params SetModelParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, &Error{Code: CodeInvalidParams, Message: err.Error()}
		}
		st, ok := s.getSession(params.SessionID)
		if !ok {
			return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("unknown session %q", params.SessionID)}
		}
		st.SetModel(params.ModelID)
		return map[string]any{"modelId": st.Model}, nil

	case "session/set_mode":
		var params SetModeParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, &Error{Code: CodeInvalidParams, Message: err.Error()}
		}
		st, ok := s.getSession(params.SessionID)
		if !ok {
			return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("unknown session %q", params.SessionID)}
		}
		st.SetMode(params.ModeID)
		s.emitUpdate(st.ID, CurrentModeUpdate{CurrentModeID: st.Mode})
		return map[string]any{"modeId": st.Mode}, nil

	case "session/cancel":
		var params CancelSessionParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, &Error{Code: CodeInvalidParams, Message: err.Error()}
		}
		st, ok := s.getSession(params.SessionID)
		if !ok {
			return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("unknown session %q", params.SessionID)}
		}
		return map[string]any{"cancelled": st.CancelTurn()}, nil

	case "session/close":
		var params CloseSessionParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, &Error{Code: CodeInvalidParams, Message: err.Error()}
		}
		s.mu.Lock()
		st, ok := s.sessions[params.SessionID]
		if ok {
			delete(s.sessions, params.SessionID)
		}
		s.mu.Unlock()
		if ok {
			st.CancelTurn()
		}
		return map[string]any{"closed": ok}, nil

	case "session/prompt":
		var params PromptParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, &Error{Code: CodeInvalidParams, Message: err.Error()}
		}
		return s.handlePrompt(ctx, params)

	default:
		return nil, &Error{Code: CodeMethodNotFound, Message: fmt.Sprintf("method not found: %s", req.Method)}
	}
}

func (s *Server) handlePrompt(ctx context.Context, params PromptParams) (any, *Error) {
	st, ok := s.getSession(params.SessionID)
	if !ok {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("unknown session %q", params.SessionID)}
	}
	if s.cfg.Streamer == nil {
		return nil, &Error{Code: CodeInternalError, Message: "no model streamer configured"}
	}

	userText := ExtractPromptText(params.Prompt)
	turnCtx, turnSeq := st.StartTurn(ctx)
	defer st.FinishTurn(turnSeq)

	if s.cfg.ToolRegistry != nil {
		return s.handlePromptWithTools(turnCtx, st, userText)
	}

	st.AppendContent(&aicode.Content{
		Role:  "user",
		Parts: []*aicode.Part{{Text: userText}},
	})

	var modelParts []*aicode.Part
	var usage *Usage
	var toolCounter int

	gemReq := &aicode.GeminiRequest{
		Contents: st.SnapshotHistory(),
	}
	err := s.cfg.Streamer.Stream(turnCtx, st.Model, gemReq, func(ch *aicode.SSEChunk) error {
		if ch == nil || ch.Response == nil {
			return nil
		}
		for _, cand := range ch.Response.Candidates {
			if cand.Content == nil {
				continue
			}
			for _, p := range cand.Content.Parts {
				modelParts = append(modelParts, p)
				switch {
				case p.Thought && p.Text != "":
					s.emitUpdate(st.ID, AgentThoughtChunkUpdate{Content: NewTextContentBlock(p.Text)})
				case p.Text != "":
					s.emitUpdate(st.ID, AgentMessageChunkUpdate{Content: NewTextContentBlock(p.Text)})
				}
				if p.FunctionCall != nil {
					toolCounter++
					tcID := p.FunctionCall.ID
					if tcID == "" {
						tcID = fmt.Sprintf("call_%d", toolCounter)
					}
					kind, title, locs := KindTitleAndLocations(p.FunctionCall.Name, p.FunctionCall.Args)
					s.emitUpdate(st.ID, ToolCallUpdate{
						ToolCallID: tcID,
						Name:       p.FunctionCall.Name,
						Args:       p.FunctionCall.Args,
						Status:     "in_progress",
						Kind:       kind,
						Title:      title,
						Locations:  locs,
						RawInput:   p.FunctionCall.Args,
					})
				}
			}
		}
		if u := ch.Response.UsageMetadata; u != nil {
			usage = &Usage{
				InputTokens:  int64(u.PromptTokenCount),
				OutputTokens: int64(u.CandidatesTokenCount),
				ThoughtTokens: int64(u.ThoughtsTokenCount),
				TotalTokens:  int64(u.TotalTokenCount),
			}
		}
		return nil
	})

	if len(modelParts) > 0 {
		st.AppendContent(&aicode.Content{Role: "model", Parts: modelParts})
	}

	if err != nil {
		if errors.Is(err, context.Canceled) {
			return PromptResult{StopReason: "cancelled", Usage: usage}, nil
		}
		return nil, &Error{Code: CodeInternalError, Message: err.Error()}
	}
	return PromptResult{StopReason: "end_turn", Usage: usage}, nil
}

func (s *Server) handlePromptWithTools(ctx context.Context, st *SessionState, userText string) (any, *Error) {
	cwd := st.GetCwd()
	if cwd == "" {
		cwd = s.cfg.DefaultCwd
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	mode := cortex.Mode(st.GetMode())
	if mode == "" {
		mode = cortex.ModeAgent
	}

	guard := cortex.NewPermissionGuard(cwd, mode)
	guard.DangerouslySkipPermissions = s.cfg.DangerouslySkipPermissions

	var toolCounter int
	toolIDs := make(map[*aicode.FunctionCall]string)
	resolveToolID := func(fc *aicode.FunctionCall) string {
		if fc == nil {
			toolCounter++
			return fmt.Sprintf("call_%d", toolCounter)
		}
		if id, ok := toolIDs[fc]; ok {
			return id
		}
		id := fc.ID
		if id == "" {
			toolCounter++
			id = fmt.Sprintf("call_%d", toolCounter)
		}
		toolIDs[fc] = id
		return id
	}

	engine := &cortex.Engine{
		Streamer:     s.cfg.Streamer,
		Model:        st.GetModel(),
		Registry:     s.cfg.ToolRegistry,
		Guard:        guard,
		SystemPrompt: cortex.BuildSystemPrompt(cwd, mode, ""),
		MaxSteps:     s.cfg.MaxToolSteps,
		OnEvent: func(ev cortex.Event) {
			switch ev.Type {
			case cortex.EventThought:
				if ev.Text != "" {
					s.emitUpdate(st.ID, AgentThoughtChunkUpdate{Content: NewTextContentBlock(ev.Text)})
				}
			case cortex.EventText:
				if ev.Text != "" {
					s.emitUpdate(st.ID, AgentMessageChunkUpdate{Content: NewTextContentBlock(ev.Text)})
				}
			case cortex.EventToolStart:
				if ev.ToolCall == nil {
					return
				}
				tcID := resolveToolID(ev.ToolCall)
				kind, title, locs := KindTitleAndLocations(ev.ToolCall.Name, ev.ToolCall.Args)
				s.emitUpdate(st.ID, ToolCallUpdate{
					ToolCallID: tcID,
					Name:       ev.ToolCall.Name,
					Args:       ev.ToolCall.Args,
					Status:     "in_progress",
					Kind:       kind,
					Title:      title,
					Locations:  locs,
					RawInput:   ev.ToolCall.Args,
				})
			case cortex.EventToolDone:
				if ev.ToolCall == nil {
					return
				}
				tcID := resolveToolID(ev.ToolCall)
				kind, title, locs := KindTitleAndLocations(ev.ToolCall.Name, ev.ToolCall.Args)
				status := "completed"
				var contents []ToolCallContent
				var rawOutput any
				if ev.Err != nil {
					status = "failed"
					errText := ev.Err.Error()
					if ev.ToolResult != nil && ev.ToolResult.Text != "" {
						errText += "\n" + ev.ToolResult.Text
					}
					contents = []ToolCallContent{ContentToolCallContent(errText)}
					rawOutput = map[string]any{"error": ev.Err.Error()}
				} else if ev.ToolResult != nil {
					if ev.ToolResult.IsDiff {
						contents = []ToolCallContent{DiffToolCallContent(ev.ToolResult.Path, ev.ToolResult.OldText, ev.ToolResult.NewText)}
					} else if ev.ToolResult.Text != "" {
						contents = []ToolCallContent{ContentToolCallContent(ev.ToolResult.Text)}
					}
					if len(ev.ToolResult.Output) > 0 {
						rawOutput = ev.ToolResult.Output
					} else if ev.ToolResult.Text != "" {
						rawOutput = map[string]any{"output": ev.ToolResult.Text}
					}
				}
				s.emitUpdate(st.ID, ToolCallStatusUpdate{
					ToolCallID: tcID,
					Name:       ev.ToolCall.Name,
					Args:       ev.ToolCall.Args,
					Status:     status,
					Kind:       kind,
					Title:      title,
					Locations:  locs,
					RawOutput:  rawOutput,
					Content:    contents,
				})
			}
		},
	}

	newHistory, summary, err := engine.RunTurn(ctx, st.SnapshotHistory(), userText)
	st.ReplaceHistory(newHistory)

	var usage *Usage
	if summary != nil && summary.Usage != nil {
		u := summary.Usage
		usage = &Usage{
			InputTokens:   int64(u.PromptTokenCount),
			OutputTokens:  int64(u.CandidatesTokenCount),
			ThoughtTokens: int64(u.ThoughtsTokenCount),
			TotalTokens:   int64(u.TotalTokenCount),
		}
	}

	if err != nil {
		if errors.Is(err, context.Canceled) {
			return PromptResult{StopReason: "cancelled", Usage: usage}, nil
		}
		return nil, &Error{Code: CodeInternalError, Message: err.Error()}
	}
	return PromptResult{StopReason: "end_turn", Usage: usage}, nil
}

func (s *Server) sessionResult(st *SessionState) NewSessionResult {
	return NewSessionResult{
		SessionID: st.ID,
		Models: &SessionModelState{
			CurrentModelID:  st.Model,
			AvailableModels: s.cfg.Models,
		},
		Modes: &SessionModeState{
			CurrentModeID: st.Mode,
			AvailableModes: []ModeInfo{
				{ID: "agent", Name: "Agent", Description: "Autonomous coding and tool use"},
				{ID: "plan", Name: "Plan", Description: "Planning before edits"},
				{ID: "ask", Name: "Ask", Description: "Read-only question answering"},
			},
		},
		ConfigOptions: GroupModels(s.cfg.Models, st.Model),
	}
}

func (s *Server) getSession(id string) (*SessionState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.sessions[id]
	return st, ok
}

func (s *Server) emitUpdate(sessionID string, update any) {
	s.mu.RLock()
	conn := s.conn
	s.mu.RUnlock()
	if conn != nil {
		_ = conn.Notify("session/update", SessionUpdateNotification{
			SessionID: sessionID,
			Update:    update,
		})
	}
}

func newSessionID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "sess-" + hex.EncodeToString(b[:])
}
