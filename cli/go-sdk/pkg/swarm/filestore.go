package swarm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	boardFileName   = "board.jsonl"
	membersFileName = "members.json"
	maxDedupWindow  = 64
)

type fileRoster struct {
	Title    string                 `json:"title"`
	Members  map[string]*fileMember `json:"members"`
	Channels map[string]*Channel    `json:"channels,omitempty"`
}

type legacyMember struct {
	Killed         bool   `json:"killed,omitempty"`
	Model          string `json:"model,omitempty"`
	HostInstanceID string `json:"host_instance_id,omitempty"`
}

type plainMember Member

type fileMember struct {
	Member
	legacy legacyMember
}

func (m fileMember) MarshalJSON() ([]byte, error) {
	wire := struct {
		plainMember
		legacyMember
	}{
		plainMember: plainMember(m.Member),
		legacyMember: legacyMember{
			Killed:         m.IsKilled(),
			Model:          m.EffectiveModel(),
			HostInstanceID: m.NodeID,
		},
	}
	return json.Marshal(wire)
}

func (m *fileMember) UnmarshalJSON(data []byte) error {
	raw := new(struct {
		plainMember
		legacyMember
	})
	if err := json.Unmarshal(data, raw); err != nil {
		return err
	}
	m.Member = Member(raw.plainMember)
	m.legacy = raw.legacyMember
	return nil
}

func (m *fileMember) toMember() *Member {
	out := new(Member)
	*out = m.Member
	if out.Phase == "" {
		out.Phase = PhaseRunning
	}
	if out.DesiredState == "" {
		out.DesiredState = DesiredStateRunning
	}
	if m.legacy.Killed {
		out.DesiredState = DesiredStateKilled
		out.Phase = PhaseKilled
	}
	if out.NodeID == "" {
		out.NodeID = m.legacy.HostInstanceID
	}
	if m.legacy.Model != "" && out.EffectiveModel() != m.legacy.Model {
		out.DesiredModel = m.legacy.Model
		out.AppliedModel = m.legacy.Model
	}
	return out
}

// FileStore manages JSONL message board and atomic members.json persistence in one swarm directory.
type FileStore struct {
	dir    string
	mu     sync.RWMutex
	roster *Roster
	msgs   []Message
	subsMu sync.Mutex
	nextID int
	subs   map[int]func(Event)
}

// Create initializes a fresh swarm directory with board.jsonl and members.json.
func Create(dir, title, coordinatorID, coordinatorModel string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create swarm dir: %w", err)
	}
	roster := &Roster{
		Title: title,
		Members: map[string]*Member{
			"coordinator": {
				ConversationID: coordinatorID,
				Role:           "coordinator",
				DesiredState:   DesiredStateRunning,
				DesiredModel:   coordinatorModel,
				AppliedModel:   coordinatorModel,
				Phase:          PhaseRunning,
				WakeMode:       WakeModeWorking,
			},
		},
		Channels: make(map[string]*Channel),
	}
	fs := &FileStore{
		dir:    dir,
		roster: roster,
		subs:   make(map[int]func(Event)),
	}
	if err := fs.writeRosterLocked(); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, boardFileName), nil, 0o644); err != nil {
		return nil, err
	}
	return fs, nil
}

// Open loads existing board.jsonl and members.json from dir into a FileStore.
func Open(dir string) (*FileStore, error) {
	msgs, err := readBoardFile(filepath.Join(dir, boardFileName))
	if err != nil {
		return nil, err
	}
	roster, err := readMembersFile(filepath.Join(dir, membersFileName))
	if err != nil {
		return nil, err
	}
	return &FileStore{
		dir:    dir,
		roster: roster,
		msgs:   msgs,
		subs:   make(map[int]func(Event)),
	}, nil
}

// InspectDir parses any swarm directory (including active Antigravity IDE/CLI swarms) into a snapshot.
func InspectDir(dir string) (*BoardSnapshot, error) {
	st, err := Open(dir)
	if err != nil {
		return nil, err
	}
	snap := &BoardSnapshot{
		Dir:      dir,
		Roster:   st.RosterSnapshot(),
		Messages: st.Tail(len(st.msgs)),
	}
	if len(snap.Messages) > 0 {
		snap.HeadSeq = snap.Messages[len(snap.Messages)-1].Seq
	}
	for _, m := range snap.Roster.Members {
		if m.IsKilled() {
			snap.KilledCount++
		} else {
			snap.LiveCount++
		}
	}
	return snap, nil
}

func (s *FileStore) Subscribe(fn func(Event)) func() {
	s.subsMu.Lock()
	s.nextID++
	id := s.nextID
	s.subs[id] = fn
	s.subsMu.Unlock()
	return func() {
		s.subsMu.Lock()
		delete(s.subs, id)
		s.subsMu.Unlock()
	}
}

func (s *FileStore) emit(ev Event) {
	s.subsMu.Lock()
	fns := make([]func(Event), 0, len(s.subs))
	for _, fn := range s.subs {
		fns = append(fns, fn)
	}
	s.subsMu.Unlock()
	for _, fn := range fns {
		fn(ev)
	}
}

func (s *FileStore) Head() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.msgs) == 0 {
		return 0
	}
	return s.msgs[len(s.msgs)-1].Seq
}

func (s *FileStore) RosterSnapshot() *Roster {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.roster.Clone()
}

func (s *FileStore) ReadSince(since uint64, limit int) []Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Message
	for _, m := range s.msgs {
		if m.Seq > since {
			out = append(out, m)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out
}

func (s *FileStore) Tail(limit int) []Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit >= len(s.msgs) {
		return append([]Message(nil), s.msgs...)
	}
	return append([]Message(nil), s.msgs[len(s.msgs)-limit:]...)
}

func (s *FileStore) Get(seqs []uint64) []Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	want := make(map[uint64]struct{}, len(seqs))
	for _, seq := range seqs {
		want[seq] = struct{}{}
	}
	var out []Message
	for _, m := range s.msgs {
		if _, ok := want[m.Seq]; ok {
			out = append(out, m)
		}
	}
	return out
}

// Post appends one message, deduplicating the most recent 64 RequestIDs.
func (s *FileStore) Post(req PostRequest) (Message, error) {
	s.mu.Lock()
	if req.RequestID != "" {
		start := len(s.msgs) - maxDedupWindow
		if start < 0 {
			start = 0
		}
		for i := len(s.msgs) - 1; i >= start; i-- {
			if s.msgs[i].RequestID == req.RequestID {
				dup := s.msgs[i]
				s.mu.Unlock()
				return dup, nil
			}
		}
	}

	var nextSeq uint64 = 1
	if len(s.msgs) > 0 {
		nextSeq = s.msgs[len(s.msgs)-1].Seq + 1
	}
	msg := Message{
		Seq:       nextSeq,
		Agent:     req.Author,
		Text:      req.Text,
		Time:      time.Now().Format("2006-01-02 15:04:05"),
		Kind:      req.Kind,
		RequestID: req.RequestID,
		Channel:   req.Channel,
		Control:   req.Control,
	}

	if err := s.appendMessageLocked(msg); err != nil {
		s.mu.Unlock()
		return Message{}, err
	}
	s.msgs = append(s.msgs, msg)
	rosterSnap := s.roster.Clone()
	s.mu.Unlock()

	s.emit(Event{
		Kind:     EventMessages,
		Messages: []Message{msg},
		Roster:   rosterSnap,
	})
	return msg, nil
}

// AddMembers registers new members in members.json and optionally posts their assignment brief.
func (s *FileStore) AddMembers(req AddMembersRequest) (Message, error) {
	s.mu.Lock()
	if s.roster.Members == nil {
		s.roster.Members = make(map[string]*Member)
	}
	for _, nm := range req.Members {
		if nm.Name == "" {
			s.mu.Unlock()
			return Message{}, errors.New("member name cannot be empty")
		}
		role := nm.Role
		if role == "" {
			role = "worker"
		}
		wsMode := nm.WorkspaceMode
		if wsMode == "" {
			wsMode = "inherit"
		}
		s.roster.Members[nm.Name] = &Member{
			ConversationID: nm.ConversationID,
			Role:           role,
			AddedBy:        req.Actor,
			NodeID:         nm.NodeID,
			DesiredState:   DesiredStateRunning,
			DesiredModel:   nm.DesiredModel,
			AppliedModel:   nm.DesiredModel,
			WorkspaceMode:  wsMode,
			Phase:          PhaseRunning,
			Workspace:      nm.Workspace,
			Channels:       append([]string(nil), nm.Channels...),
			WakeMode:       WakeModeWorking,
			UpdatedAt:      time.Now(),
		}
	}
	if err := s.writeRosterLocked(); err != nil {
		s.mu.Unlock()
		return Message{}, err
	}
	rosterSnap := s.roster.Clone()
	s.mu.Unlock()

	s.emit(Event{Kind: EventRoster, Roster: rosterSnap})

	if strings.TrimSpace(req.BriefText) == "" {
		return Message{}, nil
	}
	author := req.Actor
	if author == "" {
		author = "coordinator"
	}
	return s.Post(PostRequest{
		Author:    author,
		Text:      req.BriefText,
		Kind:      MessageKindBrief,
		Channel:   req.Channel,
		RequestID: req.RequestID,
	})
}

// UpdateMemberLocked mutates one member state atomically and persists members.json.
func (s *FileStore) UpdateMemberLocked(name string, fn func(*MemberState)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.roster.Members[name]
	if !ok || m == nil {
		return fmt.Errorf("unknown member %q", name)
	}
	st := m.State()
	fn(&st)
	m.SetState(st)
	m.UpdatedAt = time.Now()
	return s.writeRosterLocked()
}

// Kill marks the specified members KILLED and flushes members.json.
func (s *FileStore) Kill(names []string) error {
	s.mu.Lock()
	for _, name := range names {
		if m := s.roster.Members[name]; m != nil {
			m.MarkKilled()
			m.UpdatedAt = time.Now()
		}
	}
	err := s.writeRosterLocked()
	snap := s.roster.Clone()
	s.mu.Unlock()
	if err == nil {
		s.emit(Event{Kind: EventRoster, Roster: snap})
	}
	return err
}

func (s *FileStore) appendMessageLocked(msg Message) error {
	line, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	f, err := os.OpenFile(filepath.Join(s.dir, boardFileName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open board.jsonl: %w", err)
	}
	defer f.Close()
	_, err = f.Write(line)
	return err
}

func (s *FileStore) writeRosterLocked() error {
	fr := &fileRoster{
		Title:    s.roster.Title,
		Members:  make(map[string]*fileMember, len(s.roster.Members)),
		Channels: s.roster.Channels,
	}
	for k, m := range s.roster.Members {
		if m != nil {
			fr.Members[k] = &fileMember{Member: *m}
		}
	}
	data, err := json.MarshalIndent(fr, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	target := filepath.Join(s.dir, membersFileName)
	tmp, err := os.CreateTemp(s.dir, "members-*.json.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, target)
}

func readBoardFile(path string) ([]Message, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read board.jsonl: %w", err)
	}
	defer f.Close()

	var msgs []Message
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var m Message
		if err := json.Unmarshal(line, &m); err != nil {
			return nil, fmt.Errorf("parse board.jsonl line: %w", err)
		}
		msgs = append(msgs, m)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return msgs, nil
}

func readMembersFile(path string) (*Roster, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Roster{Members: make(map[string]*Member)}, nil
		}
		return nil, fmt.Errorf("read members.json: %w", err)
	}
	var fr fileRoster
	if err := json.Unmarshal(raw, &fr); err != nil {
		return nil, fmt.Errorf("parse members.json: %w", err)
	}
	out := &Roster{
		Title:    fr.Title,
		Members:  make(map[string]*Member, len(fr.Members)),
		Channels: fr.Channels,
	}
	for k, fm := range fr.Members {
		if fm != nil {
			out.Members[k] = fm.toMember()
		}
	}
	return out, nil
}
