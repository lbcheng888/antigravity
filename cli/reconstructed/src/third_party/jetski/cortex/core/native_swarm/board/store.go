// Binary VA: 0x101fb86b0 | Size: 96 B | Source: third_party/jetski/cortex/core/native_swarm/board/store.go:L216
// Package board defines the swarm message board model, store interfaces,
// mention/channel matching, and validation.
package board

import (
	"context"
	"time"

	"google3/third_party/jetski/fs/fs"
)

// BackendKind identifies the storage backend for a swarm board.
type BackendKind string

// DesiredState is the requested lifecycle state of a member.
type DesiredState string

// Phase is the runtime execution phase of a member.
type Phase string

// WakeMode describes how a parked member is woken.
type WakeMode string

// MessageKind classifies a message on the board.
type MessageKind string

// EventKind identifies a store subscription event.
type EventKind int

const (
	// MessagesAppended indicates new messages were appended to the board.
	MessagesAppended EventKind = 1
	// RosterChanged indicates the roster changed.
	RosterChanged EventKind = 2
)

// Known string constants recovered from binary comparisons.
const (
	DesiredKilled DesiredState = "KILLED"
	PhaseRunning  Phase        = "RUNNING"
	PhaseKilled   Phase        = "KILLED"
)

// Anchor is the persisted board pointer/metadata.
// Binary VA: 0x10a670388 | Size: 72 B
type Anchor struct {
	Version   int         `json:"version"`
	Backend   BackendKind `json:"backend"`
	SwarmID   string      `json:"swarm_id"`
	DB        string      `json:"db,omitempty"`
	CreatedAt string      `json:"created_at,omitempty"`
}

// CreateSpec specifies initial parameters when creating a board.
// Binary VA: 0x10a670460 | Size: 80 B
type CreateSpec struct {
	SwarmID                   string
	CoordinatorConversationID string
	CoordinatorModel          string
	Title                     string
	NodeID                    string
}

// Channel represents a named board channel.
// Binary VA: 0x10a5ac568 | Size: 56 B
type Channel struct {
	Name      string `json:"name"`
	Topic     string `json:"topic,omitempty"`
	CreatedBy string `json:"created_by"`
	Archived  bool   `json:"archived,omitempty"`
}

// Control holds structured metadata for channel/control messages.
// Binary VA: 0x10a7e1048 | Size: 112 B
type Control struct {
	Op       string              `json:"op"`
	Channel  string              `json:"channel"`
	Topic    string              `json:"topic,omitempty"`
	Channels []string            `json:"channels,omitempty"`
	Members  []string            `json:"members,omitempty"`
	Prev     map[string][]string `json:"prev,omitempty"`
	Next     map[string][]string `json:"next,omitempty"`
}

// Message is a single entry on the swarm message board.
// Binary VA: 0x10a85f3b8 | Size: 112 B
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

// MemberState holds the mutable wake/read cursor state of a member.
// Binary VA: 0x10a670610 | Size: 48 B
type MemberState struct {
	WakeMode           WakeMode
	WaitSince          uint64
	LastReadSeq        uint64
	LastNudgedSeq      uint64
	LastParkBlockedSeq uint64
}

// Member represents a participant in the swarm roster.
// Binary VA: 0x10a9b0368 | Size: 288 B
type Member struct {
	ConversationID     string       `json:"conversation_id"`
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

// NewMember describes a member being added to the roster.
// Binary VA: 0x10a85f4d8 | Size: 136 B
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

// Roster holds the swarm title, members, and channels.
// Binary VA: 0x10a496e50 | Size: 32 B
type Roster struct {
	Title    string              `json:"title"`
	Members  map[string]*Member  `json:"members"`
	Channels map[string]*Channel `json:"channels,omitempty"`
}

// PostRequest specifies a message to append to the board.
// Binary VA: 0x10a753ce0 | Size: 96 B
type PostRequest struct {
	Author    string
	ActorConv string
	Text      string
	Kind      MessageKind
	Channel   string
	RequestID string
}

// AddMembersRequest adds members and optionally posts a brief.
// Binary VA: 0x10a6702b0 | Size: 88 B
type AddMembersRequest struct {
	Actor     string
	Members   []NewMember
	BriefText string
	RequestID string
	Channel   string
}

// JoinRequest registers a remote/joining member.
// Binary VA: 0x10a670538 | Size: 80 B
type JoinRequest struct {
	Name           string
	ConversationID string
	NodeLabel      string
	Model          string
	RequestID      string
}

// Event is emitted to Store subscribers.
// Binary VA: 0x10a5ac628 | Size: 56 B
type Event struct {
	Kind     EventKind
	Messages []Message
	Roster   *Roster
	Locked   LockedStore
}

// LockedStore provides read/update access while holding the store lock.
// Binary VA: 0x10a263820 | Size: 16 B
type LockedStore interface {
	ChannelActive(string) bool
	Member(string) (*Member, bool)
	Names() []string
	UpdateMemberLocked(string, func(*MemberState))
}

// RemoteStore supports remote join/kill operations.
// Binary VA: 0x10a1654f0 | Size: 16 B
type RemoteStore interface {
	Join(context.Context, JoinRequest) (Member, error)
	Kill(context.Context, string, []string) error
}

// Store is the persistence and event interface for a swarm board.
// Binary VA: 0x10a832ce0 | Size: 16 B
type Store interface {
	AddMembers(context.Context, AddMembersRequest) (Message, error)
	ArchiveChannel(context.Context, string, string) (Message, error)
	Backend() BackendKind
	CalledOn(context.Context, string, uint64) (bool, error)
	Close() error
	CreateChannel(context.Context, string, string, string) (Message, error)
	Get(context.Context, []uint64) ([]Message, error)
	Head(context.Context) (uint64, error)
	KillMembers(context.Context, string, []string) error
	Latest(context.Context, int) ([]Message, error)
	LatestIn(context.Context, View, int) ([]Message, uint64, error)
	ModelApplied(context.Context, string, string) error
	Post(context.Context, PostRequest) (Message, error)
	Roster(context.Context) (*Roster, error)
	SetMemberModel(context.Context, string, string, string) error
	SetTitle(context.Context, string, string) error
	Since(context.Context, uint64, int) ([]Message, error)
	SinceIn(context.Context, View, uint64, int) ([]Message, uint64, error)
	Subscribe(func(Event)) func()
	SubscribeChannels(context.Context, string, string, []string, []string) (Message, error)
	SwarmID() string
	UpdateMember(context.Context, string, func(*MemberState) error) (MemberState, error)
	VisibleAfter(context.Context, View, uint64, uint64) (bool, error)
}

// Backend creates or opens a board Store.
// Binary VA: 0x10a1f18f0 | Size: 16 B
type Backend interface {
	Create(context.Context, fs.Path, CreateSpec) (Store, Anchor, error)
	Kind() BackendKind
	Open(context.Context, fs.Path, Anchor) (Store, error)
}

// Binary VA: 0x101fb86b0 | Size: 96 B | Source: third_party/jetski/cortex/core/native_swarm/board/store.go:L216
func (m *Member) State() MemberState {
	return MemberState{
		WakeMode:           m.WakeMode,
		WaitSince:          m.WaitSince,
		LastReadSeq:        m.LastReadSeq,
		LastNudgedSeq:      m.LastNudgedSeq,
		LastParkBlockedSeq: m.LastParkBlockedSeq,
	}
}

// Binary VA: 0x101fb8710 | Size: 160 B | Source: third_party/jetski/cortex/core/native_swarm/board/store.go:L227
func (m *Member) SetState(s MemberState) {
	m.WakeMode = s.WakeMode
	m.WaitSince = s.WaitSince
	m.LastReadSeq = s.LastReadSeq
	m.LastNudgedSeq = s.LastNudgedSeq
	m.LastParkBlockedSeq = s.LastParkBlockedSeq
}

// Binary VA: 0x101fb87b0 | Size: 64 B | Source: third_party/jetski/cortex/core/native_swarm/board/store.go:L239
func (s *MemberState) CatchUp(seq uint64) bool {
	if seq == 0 || s.LastReadSeq != seq-1 {
		return false
	}
	s.LastReadSeq = seq
	if seq > s.LastNudgedSeq {
		s.LastNudgedSeq = seq
	}
	return true
}

// Binary VA: 0x101fb87f0 | Size: 96 B | Source: third_party/jetski/cortex/core/native_swarm/board/store.go:L255
func (s *MemberState) FastForwardAuthor(seq uint64) bool {
	changed := s.CatchUp(seq)
	if seq != 0 && s.WaitSince == seq-1 {
		s.WaitSince = seq
		changed = true
	}
	return changed
}

// Binary VA: 0x101fb8850 | Size: 992 B | Source: third_party/jetski/cortex/core/native_swarm/board/store.go:L265
func (r *Roster) Clone() *Roster {
	out := &Roster{
		Members: make(map[string]*Member),
	}
	if r == nil {
		return out
	}
	out.Title = r.Title
	for name, m := range r.Members {
		if m == nil {
			continue
		}
		cp := *m
		if len(m.Channels) > 0 { // inlined channels.go:616
			cp.Channels = append([]string(nil), m.Channels...)
		}
		out.Members[name] = &cp
	}
	if r.Channels != nil {
		out.Channels = make(map[string]*Channel, len(r.Channels))
		for name, ch := range r.Channels {
			if ch == nil {
				continue
			}
			cp := *ch
			out.Channels[name] = &cp
		}
	}
	return out
}

// Binary VA: 0x101fb8c30 | Size: 64 B | Source: third_party/jetski/cortex/core/native_swarm/board/store.go:L305
func (k EventKind) String() string {
	switch k {
	case MessagesAppended:
		return "MessagesAppended"
	case RosterChanged:
		return "RosterChanged"
	default:
		return "EventKind(?)"
	}
}
