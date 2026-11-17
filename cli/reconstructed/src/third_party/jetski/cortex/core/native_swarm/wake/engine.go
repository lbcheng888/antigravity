// Binary source reconstruction of agy v1.5.10
// Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go
// Package: wake (google3/third_party/jetski/cortex/core/native_swarm/wake/wake)

package wake

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"google3/base/go/log"
	"google3/third_party/jetski/cortex/core/native_swarm/board/board"
)

// Binary VA: 0x10a0d8338 | Size: 8 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go
type IdleProbe func(context.Context, string) bool

// Binary VA: 0x10a0d8398 | Size: 8 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go
type RingFunc func(context.Context, string, uint64)

// Binary VA: 0x10a5bd7e8 | Size: 40 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go
type StopDecision struct {
	Block  bool
	Reason string
	From   uint64
	Head   uint64
}

// Binary VA: 0x10a4a3918 | Size: 32 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go
type doorbell struct {
	convID string
	seq    uint64
	ifIdle bool
}

// Binary VA: 0x10a4a39c0 | Size: 64 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go
type snapshot struct {
	state  board.MemberState
	evHead uint64
	head   uint64
}

// Binary VA: 0x10a926068 | Size: 136 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go
type Engine struct {
	store     board.Store
	ring      RingFunc
	host      string
	idle      IdleProbe
	cancel    func()
	closed    atomic.Bool
	mu        sync.Mutex
	head      uint64
	headKnown bool
	blocks    map[string]int
	ringMu    sync.Mutex
	qmu       sync.Mutex
	queue     []doorbell
}

type Option func(*Engine) /* inferred */

var (
	errStaleSnapshot = errors.New("wake: snapshot is stale") // 0x10d3d6cd0
	errNoChange      = errors.New("wake: no change")         // 0x10d3d6ce0
)

// Binary VA: 0x101ff5cf0 | Size: 96 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L109
func WithIdleProbe(probe IdleProbe) Option { /* inferred wrapper */
	// Binary VA: 0x101ff5cf0 | Size: 96 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L109
	return func(e *Engine) {
		e.idle = probe
	}
}

// Binary VA: 0x101fca230 | Size: 672 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L116
func New(store board.Store, ring RingFunc, host string, opts ...Option) *Engine {
	e := &Engine{
		store:  store,
		ring:   ring,
		host:   host,
		blocks: make(map[string]int),
	}
	for _, opt := range opts {
		opt(e)
	}
	e.cancel = store.Subscribe(e.onEvent)
	if err := e.ensureInit(context.Background()); err != nil {
		log.Ctxlogf(context.Background(), 1 /* inferred */, "native swarm: wake engine for %s starts without a snapshot (retried lazily): %v", store.SwarmID(), err)
	}
	return e
}

// Binary VA: 0x101fca4d0 | Size: 96 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L136
func (e *Engine) Close() {
	e.closed.Store(true)
	if e.cancel != nil {
		e.cancel()
	}
}

// Binary VA: 0x101fca530 | Size: 656 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L146
func (e *Engine) ensureInit(ctx context.Context) error {
	e.mu.Lock()
	known := e.headKnown
	e.mu.Unlock()
	if known {
		return nil
	}
	head, err := e.store.Head(ctx)
	if err != nil {
		return fmt.Errorf("read board head: %w", err)
	}
	e.mu.Lock()
	if !e.headKnown || head > e.head {
		e.head = head
	}
	e.headKnown = true
	e.mu.Unlock()
	return nil
}

// Binary VA: 0x101fca7c0 | Size: 368 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L188
func concernedBy(m *board.Member, msg board.Message, active func(string) bool) bool {
	if msg.RequestID != "" /* inferred: sp+0x110 */ {
		return false
	}
	switch string(m.WakeMode) {
	case "anything":
		if m.WaitSince >= msg.Seq {
			return false
		}
		return m.Sees(msg.Agent, msg, active)
	case "mentions":
		if m.WaitSince >= msg.Seq {
			return false
		}
		chans := board.EffectiveChannels(msg.Agent, m.Role, m.Channels)
		return board.MentionsIn(msg.Text, msg.Agent, msg.Channel, chans, active)
	default:
		return false
	}
}

// Binary VA: 0x101fca930 | Size: 624 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L203
func (e *Engine) onEvent(ev board.Event) {
	if e.closed.Load() {
		return
	}
	e.mu.Lock()
	var rings []doorbell /* inferred */
	if ev.Kind == 1 /* board.EventMessages */ {
		for _, msg := range ev.Messages {
			if !e.headKnown || msg.Seq > e.head {
				rings = e.evaluateLocked(ev.Roster, ev.Locked, msg, rings)
				e.head = msg.Seq
				e.headKnown = true
			}
		}
	}
	e.mu.Unlock()
	e.enqueue(rings)
}

// Binary VA: 0x101fcaba0 | Size: 2896 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L232
func (e *Engine) evaluateLocked(r *board.Roster, locked board.LockedStore, msg board.Message, rings []doorbell) []doorbell {
	if r == nil {
		return rings
	}
	authorName, ok := r.NameOf(msg.Agent)
	author, ok2 := locked.Member(msg.Agent)
	if ok2 && author != nil && author.DesiredState != "KILLED" && (author.Phase == "RUNNING" || author.Phase == "") && author.NodeID == e.host {
		// Binary VA: 0x101fcd400 | Size: 80 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L240
		locked.UpdateMemberLocked(msg.Agent, func(st *board.MemberState) {
			st.CatchUp(msg.Seq)
			st.FastForwardAuthor(msg.Seq)
		})
	}
	names := r.SortedNames() /* inferred */
	if msg.Kind == "brief" {
		for _, name := range names {
			if name == msg.Agent {
				continue
			}
			m, ok := locked.Member(name)
			if !ok || m == nil || m.DesiredState == "KILLED" || (m.Phase != "RUNNING" && m.Phase != "") || m.NodeID != e.host {
				continue
			}
			// Binary VA: 0x101fcd3b0 | Size: 80 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L254
			locked.UpdateMemberLocked(name, func(st *board.MemberState) {
				st.CatchUp(msg.Seq)
				st.FastForwardAuthor(msg.Seq)
			})
		}
		return rings
	}
	_ = authorName
	for _, name := range names {
		if name == msg.Agent {
			continue
		}
		m, ok := locked.Member(name)
		if !ok || m == nil || m.DesiredState == "KILLED" || (m.Phase != "RUNNING" && m.Phase != "") || m.NodeID != e.host {
			continue
		}
		if !m.Sees(name, msg, locked.ChannelActive) {
			// Binary VA: 0x101fcd360 | Size: 80 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L277
			locked.UpdateMemberLocked(name, func(st *board.MemberState) {
				st.CatchUp(msg.Seq)
				st.FastForwardAuthor(msg.Seq)
			})
			continue
		}
		if concernedBy(m, msg, locked.ChannelActive) {
			// Binary VA: 0x101fced70 | Size: 112 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L281
			locked.UpdateMemberLocked(name, func(st *board.MemberState) {
				st.WakeMode = "working"
			})
			rings = append(rings, doorbell{convID: m.ConversationID, seq: msg.Seq, ifIdle: false})
			continue
		}
		chans := board.EffectiveChannels(name, m.Role, m.Channels)
		mentioned := board.MentionsIn(msg.Text, name, msg.Channel, chans, locked.ChannelActive)
		if e.idle != nil && m.WakeMode == "working" {
			if mentioned {
				rings = append(rings, doorbell{convID: m.ConversationID, seq: msg.Seq, ifIdle: true})
			}
		} else if mentioned && m.WakeMode != "working" && msg.Kind != "brief" {
			log.Ctxlogf(nil, 2 /* inferred */, "native swarm: swarm %s: #%d by %q mentions parked member %q (wake=%s, wait_since=%d, last_read=%d) but rings no doorbell",
				e.store.SwarmID(), msg.Seq, msg.Agent, name, m.WakeMode, m.WaitSince, m.LastReadSeq)
		}
	}
	return rings
}

// Binary VA: 0x101fcb6f0 | Size: 544 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L309
func (e *Engine) enqueue(rings []doorbell) {
	if len(rings) == 0 {
		return
	}
	e.qmu.Lock()
	e.queue = append(e.queue, rings...)
	e.qmu.Unlock()
	// Binary VA: 0x101fcd450 | Size: 64 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L319
	go e.Flush(context.Background())
}

// Binary VA: 0x101fcb910 | Size: 704 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L326
func (e *Engine) Flush(ctx context.Context) {
	e.ringMu.Lock()
	// Binary VA: 0x101fcd490 | Size: 112 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L328
	defer e.ringMu.Unlock()
	for {
		e.qmu.Lock()
		batch := e.queue
		e.queue = nil
		e.qmu.Unlock()
		if len(batch) == 0 {
			return
		}
		if e.ring == nil {
			continue
		}
		for _, d := range batch {
			if d.ifIdle && (e.idle == nil || !e.idle(ctx, d.convID)) {
				continue
			}
			e.ring(ctx, d.convID, d.seq)
		}
	}
}

// Binary VA: 0x101fcbbd0 | Size: 240 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L352
func (e *Engine) loadHead() uint64 {
	e.mu.Lock()
	// Binary VA: 0x101fcd500 | Size: 112 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L354
	defer e.mu.Unlock()
	return e.head
}

// Binary VA: 0x101fcbcc0 | Size: 1856 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L402
func (e *Engine) transact(
	ctx context.Context,
	name string,
	decide func(snapshot, func(uint64) (bool, error), func(uint64) (bool, error)) func(*board.MemberState), /* inferred */
) (snapshot, error) {
	if err := e.ensureInit(ctx); err != nil {
		return snapshot{}, err
	}
	snap := new(snapshot)
	// Binary VA: 0x101fcd900 | Size: 176 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L407
	readSnap := func(st *board.MemberState) {
		snap.state = *st
		snap.evHead = e.loadHead()
	}
	// Binary VA: 0x101fcd8b0 | Size: 80 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L411
	_, err := e.store.UpdateMember(ctx, name, func(st *board.MemberState) error {
		readSnap(st)
		return errNoChange
	})
	if !errors.Is(err, errNoChange) {
		if err != nil {
			return snapshot{}, err
		}
		return snapshot{}, fmt.Errorf("wake: store %s ignored a mut error (UpdateMember contract)", e.store.SwarmID())
	}
	for {
		if err := ctx.Err(); err != nil {
			return snapshot{}, err
		}
		s := *snap
		head, err := e.store.Head(ctx)
		if err != nil {
			return snapshot{}, fmt.Errorf("read board head: %w", err)
		}
		if s.evHead > head {
			head = s.evHead
		}
		s.head = head

		calledCache := make(map[uint64]bool)
		// Binary VA: 0x101fcd7a0 | Size: 272 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L431
		calledOn := func(seq uint64) (bool, error) {
			if seq >= s.head {
				return false, nil
			}
			if v, ok := calledCache[seq]; ok {
				return v, nil
			}
			v, err := e.store.CalledOn(ctx, name, seq)
			if err != nil {
				return false, err
			}
			calledCache[seq] = v
			return v, nil
		}

		visCache := make(map[uint64]bool)
		// Binary VA: 0x101fcd650 | Size: 336 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L446
		visibleAfter := func(seq uint64) (bool, error) {
			if seq >= s.head {
				return false, nil
			}
			if v, ok := visCache[seq]; ok {
				return v, nil
			}
			v, err := e.store.VisibleAfter(ctx, board.View{Member: name}, seq, s.head)
			if err != nil {
				return false, err
			}
			visCache[seq] = v
			return v, nil
		}

		mut := decide(s, calledOn, visibleAfter)
		if err != nil /* inferred: decision error */ {
			return snapshot{}, err
		}
		// Binary VA: 0x101fcd570 | Size: 224 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L464
		_, err = e.store.UpdateMember(ctx, name, func(st *board.MemberState) error {
			if *st != s.state || e.loadHead() != s.evHead {
				readSnap(st)
				return errStaleSnapshot
			}
			if mut == nil {
				return errNoChange
			}
			mut(st)
			return nil
		})
		if errors.Is(err, errStaleSnapshot) {
			continue
		}
		if err == nil || errors.Is(err, errNoChange) {
			return s, nil
		}
		return snapshot{}, err
	}
}

// Binary VA: 0x101fcc400 | Size: 112 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L488
func gone(err error) bool {
	return errors.Is(err, board.ErrNotMember) || errors.Is(err, board.ErrKilled)
}

// Binary VA: 0x101fcc470 | Size: 656 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L496
func (e *Engine) Park(ctx context.Context, name string, since uint64, mode board.WakeMode) (uint64, error) {
	effective := new(uint64)
	*effective = since
	// Binary VA: 0x101fcd9b0 | Size: 128 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L498
	_, err := e.store.UpdateMember(ctx, name, func(st *board.MemberState) error {
		*effective = since
		if st.LastReadSeq > since {
			*effective = st.LastReadSeq
		}
		st.WaitSince = *effective
		st.WakeMode = mode
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("record the wait for %q: %w", name, err)
	}
	e.mu.Lock()
	delete(e.blocks, name)
	e.mu.Unlock()
	return *effective, nil
}

// Binary VA: 0x101fcc700 | Size: 368 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L525
func (e *Engine) MarkRead(ctx context.Context, name string, seq uint64) error {
	// Binary VA: 0x101fcda30 | Size: 48 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L526
	_, err := e.store.UpdateMember(ctx, name, func(st *board.MemberState) error {
		if st.LastReadSeq >= seq {
			return errNoChange
		}
		st.LastReadSeq = seq
		return nil
	})
	if err == nil || errors.Is(err, errNoChange) {
		return nil
	}
	return fmt.Errorf("advance the read cursor of %q: %w", name, err)
}

// Binary VA: 0x101fcc870 | Size: 592 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L551
func (e *Engine) MarkSeen(ctx context.Context, name string, seqs []uint64) error {
	if len(seqs) == 0 {
		return nil
	}
	seen := make(map[uint64]bool, len(seqs))
	var maxSeen uint64
	for _, s := range seqs {
		seen[s] = true
		if s > maxSeen {
			maxSeen = s
		}
	}
	// Binary VA: 0x101fcda60 | Size: 1632 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L561
	_, err := e.transact(ctx, name, func(s snapshot, _ func(uint64) (bool, error), _ func(uint64) (bool, error)) func(*board.MemberState) {
		floor := max(s.state.LastReadSeq, s.state.WaitSince)
		if floor >= maxSeen && s.head >= maxSeen {
			return nil
		}
		msgs, err := e.store.Since(ctx, floor, int(maxSeen-floor))
		if err != nil {
			_ = fmt.Errorf("read the board after #%d: %w", floor, err)
			return nil
		}
		hasUnseenMention := false
		for _, m := range msgs {
			if m.Seq > maxSeen || seen[m.Seq] || m.Agent == name || m.RequestID != "" {
				continue
			}
			if board.Mentions(m.Text, name) {
				return nil
			}
			if len(board.ChannelMentions(m.Text)) > 0 {
				hasUnseenMention = true
			}
		}
		if hasUnseenMention {
			r, err := e.store.Roster(ctx)
			if err != nil {
				_ = fmt.Errorf("read the roster: %w", err)
				return nil
			}
			m, ok := r.Members[name]
			if !ok || m == nil || len(r.Channels) <= 0 {
				return nil /* inferred */
			}
			after, err := e.store.Since(ctx, floor, 0)
			if err != nil {
				_ = fmt.Errorf("read the board after #%d: %w", floor, err)
				return nil
			}
			chansAt := board.ChannelsAt(name, m.Channels, after)
			activeAt := board.ActiveAt(r, after)
			for i, msg := range after {
				if msg.Seq > maxSeen {
					break
				}
				if seen[msg.Seq] || msg.Agent == name || msg.RequestID != "" {
					continue
				}
				chans := board.EffectiveChannels(name, m.Role, chansAt[i])
				if board.MentionsIn(msg.Text, name, msg.Channel, chans, activeAt[i]) {
					return nil
				}
			}
		}
		// Binary VA: 0x101fce0c0 | Size: 32 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L605
		return func(st *board.MemberState) {
			if st.LastReadSeq < maxSeen {
				st.LastReadSeq = maxSeen
			}
		}
	})
	if err != nil {
		if gone(err) {
			return nil
		}
		return fmt.Errorf("advance the read cursor of %q over seen messages: %w", name, err)
	}
	return nil
}

// Binary VA: 0x101fccac0 | Size: 256 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L634
func (e *Engine) Nudge(ctx context.Context, name string, force bool) (uint64, uint64, bool, error) {
	var shouldWake bool
	// Binary VA: 0x101fce0e0 | Size: 512 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L635
	snap, err := e.transact(ctx, name, func(s snapshot, calledOn func(uint64) (bool, error), visibleAfter func(uint64) (bool, error)) func(*board.MemberState) {
		shouldWake = false
		needWakeMode := force || s.state.WakeMode != "working"
		if s.head != 0 && s.state.LastNudgedSeq >= s.head {
			if needWakeMode {
				// Binary VA: 0x101fced00 | Size: 112 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L641
				return func(st *board.MemberState) {
					st.WakeMode = "working"
				}
			}
			return nil
		}
		floor := max(s.state.LastReadSeq, s.state.WaitSince)
		if s.state.LastReadSeq < s.head {
			if force {
				ok, err := visibleAfter(s.state.LastReadSeq)
				if err != nil {
					return nil
				}
				if ok {
					shouldWake = true
				} else {
					return nil
				}
			} else {
				ok, err := calledOn(floor)
				if err != nil {
					return nil
				}
				shouldWake = ok
			}
		}
		// Binary VA: 0x101fce2e0 | Size: 128 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L665
		return func(st *board.MemberState) {
			if needWakeMode {
				st.WakeMode = "working"
			}
			if st.LastNudgedSeq < s.head {
				st.LastNudgedSeq = s.head
			}
		}
	})
	if err != nil {
		if gone(err) {
			return 0, 0, false, nil
		}
		return 0, 0, false, err
	}
	return snap.head, snap.state.WaitSince, shouldWake, nil
}

// Binary VA: 0x101fccbc0 | Size: 192 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L687
func (e *Engine) HasConcerning(ctx context.Context, name string) (bool, error) {
	var has bool
	// Binary VA: 0x101fce360 | Size: 336 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L689
	_, err := e.transact(ctx, name, func(s snapshot, calledOn func(uint64) (bool, error), visibleAfter func(uint64) (bool, error)) func(*board.MemberState) {
		has = false
		switch string(s.state.WakeMode) {
		case "anything":
			ok, err := visibleAfter(s.state.WaitSince)
			if err == nil {
				has = ok
			}
		case "mentions":
			ok, err := calledOn(s.state.WaitSince)
			if err == nil {
				has = ok
			}
		}
		if has {
			// Binary VA: 0x101fcec90 | Size: 112 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L706
			return func(st *board.MemberState) {
				st.WakeMode = "working"
			}
		}
		return nil
	})
	if err != nil {
		if gone(err) {
			return false, nil
		}
		return false, err
	}
	return has, nil
}

// Binary VA: 0x101fccc80 | Size: 848 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L743
func (e *Engine) ParkOnStop(ctx context.Context, name string) (bool, error) {
	var wake bool
	// Binary VA: 0x101fce4b0 | Size: 560 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L745
	snap, err := e.transact(ctx, name, func(s snapshot, calledOn func(uint64) (bool, error), visibleAfter func(uint64) (bool, error)) func(*board.MemberState) {
		wake = false
		mode := string(s.state.WakeMode)
		if mode == "anything" || mode == "mentions" {
			var ok bool
			var err error
			if mode == "anything" {
				ok, err = visibleAfter(s.state.WaitSince)
			} else {
				ok, err = calledOn(s.state.WaitSince)
			}
			if err != nil {
				return nil
			}
			if ok && s.state.LastParkBlockedSeq < s.head {
				wake = true
				// Binary VA: 0x101fce770 | Size: 32 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L764
				return func(st *board.MemberState) {
					if st.LastParkBlockedSeq < s.head {
						st.LastParkBlockedSeq = s.head
					}
				}
			}
			return nil
		}
		since := max(s.state.WaitSince, s.state.LastReadSeq)
		ok, err := calledOn(since)
		if err != nil {
			return nil
		}
		unblocked := ok && s.state.LastParkBlockedSeq < s.head
		wake = unblocked
		updateBlocked := ok && !unblocked
		_ = updateBlocked
		// Binary VA: 0x101fce6e0 | Size: 144 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L779
		return func(st *board.MemberState) {
			st.WakeMode = "mentions"
			st.WaitSince = since
			if unblocked && st.LastParkBlockedSeq < s.head {
				st.LastParkBlockedSeq = s.head
			}
		}
	})
	if err != nil {
		if gone(err) {
			return false, nil
		}
		return false, err
	}
	e.mu.Lock()
	delete(e.blocks, name)
	e.mu.Unlock()
	if !wake {
		return false, nil
	}
	r, err := e.store.Roster(ctx)
	if err != nil {
		return false, fmt.Errorf("read roster to ring %q: %w", name, err)
	}
	m, ok := r.Members[name]
	if !ok || m.DesiredState == "KILLED" {
		return false, nil
	}
	e.enqueue([]doorbell{{convID: m.ConversationID, seq: snap.head, ifIdle: true}})
	e.Flush(ctx)
	return true, nil
}

// Binary VA: 0x101fccfd0 | Size: 912 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L842
func (e *Engine) CheckStop(ctx context.Context, name string, maxBlocks int, peek bool) (StopDecision, error) {
	e.mu.Lock()
	canBlock := true
	if peek {
		if maxBlocks <= 0 {
			canBlock = false
		} else {
			canBlock = e.blocks[name] <= maxBlocks
		}
	}
	e.mu.Unlock()

	var dec StopDecision
	// Binary VA: 0x101fce790 | Size: 944 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L851
	_, err := e.transact(ctx, name, func(s snapshot, calledOn func(uint64) (bool, error), visibleAfter func(uint64) (bool, error)) func(*board.MemberState) {
		dec = StopDecision{Head: s.head}
		mode := string(s.state.WakeMode)
		if mode == "anything" || mode == "mentions" {
			dec.From = s.state.WaitSince
			if s.state.WaitSince >= s.head && s.state.LastReadSeq < s.head {
				return nil
			}
			var ok bool
			var err error
			if mode == "anything" {
				ok, err = visibleAfter(s.state.WaitSince)
			} else {
				ok, err = calledOn(s.state.WaitSince)
			}
			if err != nil || !ok {
				return nil
			}
			if canBlock {
				if peek {
					// Binary VA: 0x101fcec50 | Size: 32 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L885
					return func(st *board.MemberState) {
						if st.WaitSince < s.head {
							st.WaitSince = s.head
						}
					}
				}
				if s.state.WaitSince < s.state.LastReadSeq {
					// Binary VA: 0x101fcec70 | Size: 32 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L883
					return func(st *board.MemberState) {
						if st.WaitSince < st.LastReadSeq {
							st.WaitSince = st.LastReadSeq
						}
					}
				}
				return nil
			}
			dec.Block = true
			dec.Reason = "[board] new messages arrived; read_board before waiting again"
			// Binary VA: 0x101fcebd0 | Size: 128 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L891
			return func(st *board.MemberState) {
				if st.LastParkBlockedSeq < s.head {
					st.LastParkBlockedSeq = s.head
				}
				st.WakeMode = "working"
			}
		}

		from := max(s.state.WaitSince, s.state.LastReadSeq)
		dec.From = from
		ok, err := calledOn(from)
		if err != nil {
			return nil
		}
		if ok && s.state.LastParkBlockedSeq < s.head && !canBlock {
			dec.Block = true
			dec.Reason = "[board] new messages call on you; read_board before ending your turn"
			// Binary VA: 0x101fcebb0 | Size: 32 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L904
			return func(st *board.MemberState) {
				if st.LastParkBlockedSeq < s.head {
					st.LastParkBlockedSeq = s.head
				}
			}
		}
		if ok && peek {
			return nil
		}
		// Binary VA: 0x101fceb40 | Size: 112 B | Source: third_party/jetski/cortex/core/native_swarm/wake/engine.go:L912
		return func(st *board.MemberState) {
			st.WakeMode = "mentions"
			st.WaitSince = from
		}
	})
	if err != nil {
		if gone(err) {
			return StopDecision{}, nil
		}
		return StopDecision{}, err
	}
	e.mu.Lock()
	if dec.Block {
		e.blocks[name]++
	} else {
		delete(e.blocks, name)
	}
	e.mu.Unlock()
	return dec, nil
}
