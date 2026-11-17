package acp

import (
	"context"
	"sync"
	"time"

	"antigravity.dev/agy-sdk/pkg/aicode"
)

// SessionState tracks conversation history, active model/mode, and cancellation state for one ACP session.
type SessionState struct {
	mu        sync.Mutex
	ID        string
	Cwd       string
	Title     string
	Model     string
	Mode      string
	UpdatedAt time.Time
	History   []*aicode.Content
	cancelFn  context.CancelFunc
	turnSeq   uint64
}

func NewSessionState(id, cwd, model string) *SessionState {
	if model == "" {
		model = "gemini-3.5-pro"
	}
	return &SessionState{
		ID:        id,
		Cwd:       cwd,
		Model:     model,
		Mode:      "agent",
		UpdatedAt: time.Now(),
	}
}

func (s *SessionState) SetModel(model string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if model != "" {
		s.Model = model
		s.UpdatedAt = time.Now()
	}
}

func (s *SessionState) GetModel() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Model
}

func (s *SessionState) SetMode(mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mode != "" {
		s.Mode = mode
		s.UpdatedAt = time.Now()
	}
}

func (s *SessionState) GetMode() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Mode
}

func (s *SessionState) GetCwd() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Cwd
}

func (s *SessionState) AppendContent(c *aicode.Content) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.History = append(s.History, c)
	s.UpdatedAt = time.Now()
}

func (s *SessionState) ReplaceHistory(history []*aicode.Content) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*aicode.Content, len(history))
	copy(out, history)
	s.History = out
	s.UpdatedAt = time.Now()
}

func (s *SessionState) SnapshotHistory() []*aicode.Content {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*aicode.Content, len(s.History))
	copy(out, s.History)
	return out
}

func (s *SessionState) StartTurn(parent context.Context) (context.Context, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancelFn != nil {
		s.cancelFn()
	}
	s.turnSeq++
	ctx, cancel := context.WithCancel(parent)
	s.cancelFn = cancel
	return ctx, s.turnSeq
}

func (s *SessionState) FinishTurn(seq uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.turnSeq == seq && s.cancelFn != nil {
		s.cancelFn()
		s.cancelFn = nil
	}
}

func (s *SessionState) CancelTurn() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancelFn == nil {
		return false
	}
	s.cancelFn()
	s.cancelFn = nil
	return true
}

func (s *SessionState) Info() SessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return SessionInfo{
		SessionID: s.ID,
		Cwd:       s.Cwd,
		Title:     s.Title,
		UpdatedAt: s.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
