package swarm

import (
	"sort"
	"strings"
	"time"
)

type MessageKind string

const (
	MessageKindChat    MessageKind = ""
	MessageKindBrief   MessageKind = "brief"
	MessageKindControl MessageKind = "control"
)

type DesiredState string

const (
	DesiredStateRunning DesiredState = "RUNNING"
	DesiredStateKilled  DesiredState = "KILLED"
)

type Phase string

const (
	PhaseRunning Phase = "RUNNING"
	PhaseKilled  Phase = "KILLED"
)

type WakeMode string

const (
	WakeModeWorking  WakeMode = "working"
	WakeModeMentions WakeMode = "mentions"
	WakeModeAnything WakeMode = "anything"
)

type Channel struct {
	Name      string `json:"name"`
	Topic     string `json:"topic,omitempty"`
	CreatedBy string `json:"created_by,omitempty"`
	Archived  bool   `json:"archived,omitempty"`
}

type Control struct {
	Op       string              `json:"op"`
	Channel  string              `json:"channel,omitempty"`
	Topic    string              `json:"topic,omitempty"`
	Channels []string            `json:"channels,omitempty"`
	Members  []string            `json:"members,omitempty"`
	Prev     map[string][]string `json:"prev,omitempty"`
	Next     map[string][]string `json:"next,omitempty"`
}

type Message struct {
	Seq       uint64      `json:"seq"`
	Agent     string      `json:"agent"`
	Text      string      `json:"text"`
	Time      string      `json:"time"`
	Kind      MessageKind `json:"kind,omitempty"`
	RequestID string      `json:"request_id,omitempty"`
	Channel   string      `json:"channel,omitempty"`
	Control   *Control    `json:"control,omitempty"`
}

type MemberState struct {
	WakeMode           WakeMode
	WaitSince          uint64
	LastReadSeq        uint64
	LastNudgedSeq      uint64
	LastParkBlockedSeq uint64
}

func (s *MemberState) CatchUp(seq uint64) bool {
	if seq > s.LastReadSeq {
		s.LastReadSeq = seq
		return true
	}
	return false
}

func (s *MemberState) FastForwardAuthor(seq uint64) bool {
	if s.WakeMode != WakeModeWorking && seq > s.WaitSince {
		s.WaitSince = seq
		return true
	}
	return false
}

type Member struct {
	ConversationID     string       `json:"conversation_id,omitempty"`
	Role               string       `json:"role"`
	AddedBy            string       `json:"added_by,omitempty"`
	NodeID             string       `json:"node_id,omitempty"`
	OwnerKind          string       `json:"owner_kind,omitempty"`
	OwnerLabel         string       `json:"owner_label,omitempty"`
	DesiredState       DesiredState `json:"desired_state,omitempty"`
	DesiredModel       string       `json:"desired_model,omitempty"`
	AppliedModel       string       `json:"applied_model,omitempty"`
	WorkspaceMode      string       `json:"workspace_mode,omitempty"`
	Phase              Phase        `json:"phase,omitempty"`
	Workspace          string       `json:"workspace,omitempty"`
	Channels           []string     `json:"channels,omitempty"`
	WakeMode           WakeMode     `json:"wake_mode"`
	WaitSince          uint64       `json:"wait_since"`
	LastReadSeq        uint64       `json:"last_read_seq"`
	LastNudgedSeq      uint64       `json:"last_nudged_seq"`
	LastParkBlockedSeq uint64       `json:"last_park_blocked_seq"`
	UpdatedAt          time.Time    `json:"-"`
}

func (m *Member) IsKilled() bool {
	return m == nil || m.DesiredState == DesiredStateKilled || m.Phase == PhaseKilled
}

func (m *Member) IsLive() bool {
	if m == nil || m.DesiredState == DesiredStateKilled {
		return false
	}
	return m.Phase == "" || m.Phase == PhaseRunning
}

func (m *Member) MarkKilled() bool {
	if m.IsKilled() {
		return false
	}
	m.DesiredState = DesiredStateKilled
	m.Phase = PhaseKilled
	return true
}

func (m *Member) EffectiveModel() string {
	if m == nil {
		return ""
	}
	if m.DesiredModel != "" {
		return m.DesiredModel
	}
	return m.AppliedModel
}

func (m *Member) State() MemberState {
	return MemberState{
		WakeMode:           m.WakeMode,
		WaitSince:          m.WaitSince,
		LastReadSeq:        m.LastReadSeq,
		LastNudgedSeq:      m.LastNudgedSeq,
		LastParkBlockedSeq: m.LastParkBlockedSeq,
	}
}

func (m *Member) SetState(st MemberState) {
	m.WakeMode = st.WakeMode
	m.WaitSince = st.WaitSince
	m.LastReadSeq = st.LastReadSeq
	m.LastNudgedSeq = st.LastNudgedSeq
	m.LastParkBlockedSeq = st.LastParkBlockedSeq
}

type Roster struct {
	Title    string              `json:"title"`
	Members  map[string]*Member  `json:"members"`
	Channels map[string]*Channel `json:"channels,omitempty"`
}

func (r *Roster) SortedNames() []string {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.Members))
	for name := range r.Members {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (r *Roster) NameOf(id string) (string, bool) {
	if r == nil || id == "" {
		return "", false
	}
	if _, ok := r.Members[id]; ok {
		return id, true
	}
	for name, m := range r.Members {
		if m != nil && strings.EqualFold(m.ConversationID, id) {
			return name, true
		}
	}
	return "", false
}

func (r *Roster) Clone() *Roster {
	if r == nil {
		return nil
	}
	out := &Roster{
		Title:    r.Title,
		Members:  make(map[string]*Member, len(r.Members)),
		Channels: make(map[string]*Channel, len(r.Channels)),
	}
	for k, m := range r.Members {
		if m == nil {
			continue
		}
		cp := *m
		if len(m.Channels) > 0 {
			cp.Channels = append([]string(nil), m.Channels...)
		}
		out.Members[k] = &cp
	}
	for k, ch := range r.Channels {
		if ch == nil {
			continue
		}
		cp := *ch
		out.Channels[k] = &cp
	}
	return out
}

type NewMember struct {
	Name           string
	Role           string
	NodeID         string
	DesiredModel   string
	WorkspaceMode  string
	ConversationID string
	Workspace      string
	Channels       []string
}

type AddMembersRequest struct {
	Actor     string
	Members   []NewMember
	BriefText string
	RequestID string
	Channel   string
}

type PostRequest struct {
	Author    string
	Text      string
	Kind      MessageKind
	Channel   string
	RequestID string
	Control   *Control
}

type EventKind int

const (
	EventMessages EventKind = 1
	EventRoster   EventKind = 2
)

type Event struct {
	Kind     EventKind
	Messages []Message
	Roster   *Roster
}

type BoardSnapshot struct {
	Dir         string
	Roster      *Roster
	Messages    []Message
	HeadSeq     uint64
	LiveCount   int
	KilledCount int
}
