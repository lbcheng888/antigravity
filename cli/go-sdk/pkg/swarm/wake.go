package swarm

import (
	"sync"
	"sync/atomic"
)

// Doorbell notifies the runtime that an idle member should be woken for a board message.
type Doorbell struct {
	Member string
	Seq    uint64
	Reason WakeMode
}

// WakeEngine observes FileStore message events, maintains member WakeMode/LastReadSeq/WaitSince
// cursors, and emits Doorbells when idle members are concerned by new messages.
type WakeEngine struct {
	store     *FileStore
	hostID    string
	mu        sync.Mutex
	head      uint64
	headKnown bool
	closed    atomic.Bool
	unsub     func()
	doorbells chan Doorbell
}

// NewWakeEngine attaches a wake engine to store for members matching hostID (empty matches all).
func NewWakeEngine(store *FileStore, hostID string) *WakeEngine {
	e := &WakeEngine{
		store:     store,
		hostID:    hostID,
		doorbells: make(chan Doorbell, 64),
	}
	e.unsub = store.Subscribe(e.onEvent)
	return e
}

func (e *WakeEngine) Doorbells() <-chan Doorbell {
	return e.doorbells
}

func (e *WakeEngine) Close() {
	if e.closed.Swap(true) {
		return
	}
	if e.unsub != nil {
		e.unsub()
	}
	close(e.doorbells)
}

func (e *WakeEngine) onEvent(ev Event) {
	if e.closed.Load() || ev.Kind != EventMessages {
		return
	}
	e.mu.Lock()
	var rings []Doorbell
	for _, msg := range ev.Messages {
		if !e.headKnown || msg.Seq > e.head {
			rings = e.evaluateLocked(ev.Roster, msg, rings)
			e.head = msg.Seq
			e.headKnown = true
		}
	}
	e.mu.Unlock()

	for _, r := range rings {
		if e.closed.Load() {
			return
		}
		select {
		case e.doorbells <- r:
		default:
		}
	}
}

func (e *WakeEngine) evaluateLocked(r *Roster, msg Message, rings []Doorbell) []Doorbell {
	if r == nil {
		return rings
	}
	channelActive := func(ch string) bool {
		if r.Channels == nil {
			return false
		}
		c := r.Channels[ch]
		return c != nil && !c.Archived
	}

	// Author catches up on its own post and advances wait_since if waiting.
	if authorName, ok := r.NameOf(msg.Agent); ok {
		if m := r.Members[authorName]; m != nil && m.IsLive() && (e.hostID == "" || m.NodeID == e.hostID) {
			_ = e.store.UpdateMemberLocked(authorName, func(st *MemberState) {
				st.CatchUp(msg.Seq)
				st.FastForwardAuthor(msg.Seq)
			})
		}
	}

	// Briefs wake newly added members via spawn, not board-waiter doorbells.
	if msg.Kind == MessageKindBrief {
		for _, name := range r.SortedNames() {
			if name == msg.Agent {
				continue
			}
			m := r.Members[name]
			if m == nil || !m.IsLive() || (e.hostID != "" && m.NodeID != e.hostID) {
				continue
			}
			_ = e.store.UpdateMemberLocked(name, func(st *MemberState) {
				st.CatchUp(msg.Seq)
				st.FastForwardAuthor(msg.Seq)
			})
		}
		return rings
	}

	for _, name := range r.SortedNames() {
		if name == msg.Agent {
			continue
		}
		m := r.Members[name]
		if m == nil || !m.IsLive() || (e.hostID != "" && m.NodeID != e.hostID) {
			continue
		}
		if !m.Sees(name, msg, channelActive) {
			_ = e.store.UpdateMemberLocked(name, func(st *MemberState) {
				st.CatchUp(msg.Seq)
				st.FastForwardAuthor(msg.Seq)
			})
			continue
		}
		if concernedBy(name, m, msg, channelActive) {
			prevMode := m.WakeMode
			_ = e.store.UpdateMemberLocked(name, func(st *MemberState) {
				st.WakeMode = WakeModeWorking
				st.LastNudgedSeq = msg.Seq
			})
			rings = append(rings, Doorbell{
				Member: name,
				Seq:    msg.Seq,
				Reason: prevMode,
			})
		}
	}
	return rings
}

func concernedBy(name string, m *Member, msg Message, active func(string) bool) bool {
	if m.WaitSince >= msg.Seq {
		return false
	}
	switch m.WakeMode {
	case WakeModeAnything:
		return m.Sees(name, msg, active)
	case WakeModeMentions:
		chans := m.EffectiveChannels(name)
		return MentionsIn(msg.Text, name, msg.Channel, chans, active)
	default:
		return false
	}
}
