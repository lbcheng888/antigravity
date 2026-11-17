// Reconstructed from agy v1.5.10 arm64 Mach-O binary
// Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go

package filestore

import (
	"bytes"
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"google3/third_party/jetski/base/go/log"
	"google3/third_party/jetski/cortex/core/native_swarm/board/board"
	"google3/third_party/jetski/fs/fs"
)

const (
	boardFile   = "board.jsonl"
	membersFile = "members.json"
)

var (
	sharedMu  sync.Mutex                 /* inferred */
	sharedMap = map[sharedKey]*shared{}  /* inferred */
	limiter   *board.RateLimiter         /* inferred; BSS 0x10d4eae50 */
)

// Backend is the local file-backed board.Backend.
var Backend board.Backend = backend{} /* inferred */

type backend struct{}

type Store struct {
	dir     fs.Path
	swarmID string
	sh      *shared
	mu      sync.Mutex
	closed  bool
	cancels []func()
}

type sharedKey struct {
	fs   any
	path string
}

type shared struct {
	mu   sync.RWMutex
	feed board.Feed
	ctl  controlCache
}

type controlCache struct {
	mu       sync.Mutex
	scanned  int
	controls []board.Message
}

type lockedStore struct {
	r     *board.Roster
	dirty bool
	done  bool
}

// Binary VA: 0x101fbceb0 | Size: 240 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L46
func Exists(ctx context.Context, dir fs.Path) (bool, error) {
	_, err := dir.Join(boardFile).Stat(ctx)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Binary VA: 0x101fbcfa0 | Size: 256 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L103
func keyFor(dir fs.Path) sharedKey {
	p := dir.DisplayString()
	t := reflect.TypeOf(dir.GetFS()) // inlined type.go:2988 / type.go:1649
	if t != nil && t.Comparable() {
		return sharedKey{fs: dir.GetFS(), path: p}
	}
	return sharedKey{path: p}
}

// Binary VA: 0x101fbd0a0 | Size: 464 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L121
func sharedFor(dir fs.Path) *shared {
	sharedMu.Lock()
	defer sharedMu.Unlock() // deferwrap1 VA: 0x101fc4930 | Size: 112 B | L123
	k := keyFor(dir)
	if sh, ok := sharedMap[k]; ok {
		return sh
	}
	sh := &shared{}
	sharedMap[k] = sh
	return sh
}

// Binary VA: 0x101fbd270 | Size: 16 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L139
func (backend) Kind() board.BackendKind {
	return "local"
}

// Binary VA: 0x101fbd280 | Size: 528 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L145
func (backend) Open(ctx context.Context, dir fs.Path, anchor board.Anchor) (board.Store, error) {
	ok, err := Exists(ctx, dir)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("open file board in %s: %w", dir, os.ErrNotExist)
	}
	return &Store{ // inlined L221
		dir:     dir,
		swarmID: anchor.SwarmID,
		sh:      sharedFor(dir),
	}, nil
}

// Binary VA: 0x101fbd490 | Size: 2080 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L160
func (backend) Create(ctx context.Context, dir fs.Path, spec board.CreateSpec) (board.Store, board.Anchor, error) {
	coord := &board.Member{ // inlined validate.go:408-417
		ConversationID: spec.CoordinatorConversationID,
		Role:           "coordinator",
		NodeID:         spec.NodeID,
		DesiredState:   "RUNNING",
		DesiredModel:   spec.CoordinatorModel,
		AppliedModel:   spec.CoordinatorModel,
		Phase:          "RUNNING",
		WorkspaceMode:  "inherit",
		WakeMode:       "working",
	}

	sh := sharedFor(dir)
	sh.mu.Lock()
	defer sh.mu.Unlock() // deferwrap1 VA: 0x101fc4ba0 | Size: 64 B | L164

	if err := dir.MkdirAll(ctx, 0o755); err != nil {
		return nil, board.Anchor{}, fmt.Errorf("create swarm directory: %w", err)
	}
	p := dir.Join(boardFile)
	if _, err := p.Stat(ctx); errors.Is(err, os.ErrNotExist) {
		if err := p.WriteFile(ctx, nil, 0o644); err != nil {
			return nil, board.Anchor{}, fmt.Errorf("create %s: %w", boardFile, err)
		}
	} else if err != nil {
		return nil, board.Anchor{}, err
	}

	// Binary VA: 0x101fc49a0 | Size: 512 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L178
	err := updateRoster(ctx, dir, func(r *board.Roster) (bool, error) {
		if existing, ok := r.Members["coordinator"]; ok && existing != nil {
			if existing.ConversationID == coord.ConversationID {
				return false, nil
			}
			return false, fmt.Errorf("%w: swarm in %s already has coordinator %q", board.ErrNameUsed /* inferred: 0x10d3d16f0 */, dir, existing.ConversationID)
		}
		r.Members["coordinator"] = coord
		if r.Title == "" {
			r.Title = spec.Title
		}
		return true, nil
	})
	if err != nil {
		return nil, board.Anchor{}, err
	}

	anchor := board.Anchor{
		Version:   1,
		Backend:   "local",
		SwarmID:   spec.SwarmID,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	st := &Store{ // inlined L221
		dir:     dir,
		swarmID: spec.SwarmID,
		sh:      sharedFor(dir),
	}
	return st, anchor, nil
}

// Binary VA: 0x101fbdcb0 | Size: 16 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L225
func (s *Store) Dir() fs.Path {
	return s.dir
}

// Binary VA: 0x101fbdcc0 | Size: 16 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L229
func (s *Store) SwarmID() string {
	return s.swarmID
}

// Binary VA: 0x101fbdcd0 | Size: 16 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L232
func (s *Store) Backend() board.BackendKind {
	return "local"
}

// Binary VA: 0x101fbdce0 | Size: 304 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L234
func (s *Store) checkOpen() error {
	s.mu.Lock()
	defer s.mu.Unlock() // deferwrap1 VA: 0x101fc4be0 | Size: 112 B | L236
	if s.closed {
		return board.ErrClosed /* inferred: 0x10d3d1740 "board is closed" */
	}
	return nil
}

// Binary VA: 0x101fbde10 | Size: 352 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L245
func (s *Store) Close() error {
	s.mu.Lock()
	cancels := s.cancels
	s.cancels = nil
	s.closed = true
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	return nil
}

// Binary VA: 0x101fbdf70 | Size: 1984 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L264
func (s *Store) Post(ctx context.Context, req board.PostRequest) (board.Message, error) {
	if err := s.checkOpen(); err != nil {
		return board.Message{}, err
	}
	s.sh.mu.Lock()
	defer s.sh.mu.Unlock() // deferwrap1 VA: 0x101fc4ca0 | Size: 64 B | L269

	raw, err := readFeed(ctx, s.dir)
	if err != nil {
		return board.Message{}, err
	}
	if prev, ok := findRequest(raw, req.RequestID); ok {
		return prev, nil
	}
	r, err := loadRosterOrEmpty(ctx, s.dir)
	if err != nil {
		return board.Message{}, err
	}
	head := countLines(raw)
	if err := board.ValidatePost(r, req, head); err != nil {
		return board.Message{}, err
	}
	if err := limiter.Allow(req.Author); err != nil {
		return board.Message{}, err
	}
	ch := req.Channel
	if ch == "main" { // inlined channels.go:102
		ch = ""
	}
	msg := board.Message{
		Agent:     req.Author,
		Text:      req.Text,
		Kind:      req.Kind,
		RequestID: req.RequestID,
		Channel:   ch,
	}
	msg, err = s.appendLocked(ctx, head, msg)
	if err != nil {
		return board.Message{}, err
	}

	ls := &lockedStore{r: r}
	// Binary VA: 0x101fc4c50 | Size: 80 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L301
	ls.UpdateMemberLocked(req.Author, func(st *board.MemberState) {
		st.FastForwardAuthor(msg.Seq) // inlined store.go:240-245,256-258
	})
	s.sh.feed.Emit(board.Event{
		Kind:     board.EventMessagesAppended, /* inferred: 1 */
		Messages: []board.Message{msg},
		Locked:   ls,
	})
	ls.done = true
	if ls.dirty {
		s.writeCursors(ctx, r)
	}
	return msg, nil
}

// Binary VA: 0x101fbe730 | Size: 304 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L316
func (s *Store) writeCursors(ctx context.Context, r *board.Roster) {
	if err := writeRoster(ctx, s.dir, r); err != nil {
		log.CtxInfof /* inferred */ (ctx, "native_swarm: board change in %s is durable but member cursors were not saved: %v", s.dir, err)
	}
}

// Binary VA: 0x101fbe860 | Size: 944 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L324
func (s *Store) appendLocked(ctx context.Context, head uint64, msg board.Message) (board.Message, error) {
	msg.Seq = head + 1
	msg.Time = time.Now().Format("2006-01-02 15:04:05")
	b, err := jsonv2.Marshal(msg)
	if err != nil {
		return board.Message{}, fmt.Errorf("encode message: %w", err)
	}
	b = append(b, '\n')
	if err := s.dir.Join(boardFile).AppendFile(ctx, b, 0o644); err != nil {
		return board.Message{}, fmt.Errorf("append to %s: %w", boardFile, err)
	}
	return msg, nil
}

// Binary VA: 0x101fbec10 | Size: 736 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L341
func (s *Store) Since(ctx context.Context, since uint64, limit int) ([]board.Message, error) {
	if err := s.checkOpen(); err != nil {
		return nil, err
	}
	s.sh.mu.RLock()
	defer s.sh.mu.RUnlock() // deferwrap1 VA: 0x101fc4ce0 | Size: 128 B | L346
	msgs, err := readMessages(ctx, s.dir)
	if err != nil {
		return nil, err
	}
	// inlined sinceMessages L558-L565
	var out []board.Message
	for _, m := range msgs {
		if m.Seq <= since {
			continue
		}
		if limit > 0 && len(out) == limit {
			break
		}
		out = append(out, m)
	}
	return out, nil
}

// Binary VA: 0x101fbeef0 | Size: 496 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L356
func (s *Store) Latest(ctx context.Context, limit int) ([]board.Message, error) {
	if err := s.checkOpen(); err != nil {
		return nil, err
	}
	s.sh.mu.RLock()
	defer s.sh.mu.RUnlock() // deferwrap1 VA: 0x101fc4d60 | Size: 128 B | L361
	msgs, err := readMessages(ctx, s.dir)
	if err != nil {
		return nil, err
	}
	// inlined tailMessages L370-L371
	if limit > 0 && len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	return msgs, nil
}

// Binary VA: 0x101fbf0e0 | Size: 928 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L378
func (s *Store) Get(ctx context.Context, seqs []uint64) ([]board.Message, error) {
	if err := s.checkOpen(); err != nil {
		return nil, err
	}
	s.sh.mu.RLock()
	defer s.sh.mu.RUnlock() // deferwrap1 VA: 0x101fc4de0 | Size: 128 B | L383
	want := make(map[uint64]bool, len(seqs))
	for _, seq := range seqs {
		want[seq] = true
	}
	msgs, err := readMessages(ctx, s.dir)
	if err != nil {
		return nil, err
	}
	var out []board.Message
	for _, m := range msgs {
		if want[m.Seq] {
			out = append(out, m)
			delete(want, m.Seq)
		}
	}
	return out, nil
}

// Binary VA: 0x101fbf480 | Size: 384 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L405
func (s *Store) Head(ctx context.Context) (uint64, error) {
	if err := s.checkOpen(); err != nil {
		return 0, err
	}
	s.sh.mu.RLock()
	defer s.sh.mu.RUnlock() // deferwrap1 VA: 0x101fc4e60 | Size: 128 B | L410
	raw, err := readFeed(ctx, s.dir)
	if err != nil {
		return 0, err
	}
	return countLines(raw), nil
}

// Binary VA: 0x101fbf600 | Size: 864 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L421
func (s *Store) CalledOn(ctx context.Context, name string, since uint64) (bool, error) {
	if err := s.checkOpen(); err != nil {
		return false, err
	}
	s.sh.mu.RLock()
	defer s.sh.mu.RUnlock() // deferwrap1 VA: 0x101fc4f60 | Size: 128 B | L426
	controls, err := s.sh.ctl.get(ctx, s.dir)
	if err != nil {
		return false, err
	}
	if len(controls) > 0 {
		raw, err := readFeed(ctx, s.dir)
		if err != nil {
			return false, err
		}
		var hit bool
		// Binary VA: 0x101fc4ee0 | Size: 128 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L437
		if err := s.filter(ctx, raw, func(r *board.Roster, msgs []board.Message) error {
			hit = board.CalledOnIn(r, msgs, name, since)
			return nil
		}); err != nil {
			return false, err
		}
		return hit, nil
	}
	msgs, err := readMessages(ctx, s.dir)
	if err != nil {
		return false, err
	}
	for _, m := range msgs {
		if m.Seq <= since {
			continue
		}
		if m.Agent != name && board.Mentions(m.Text, name) {
			return true, nil
		}
	}
	return false, nil
}

// Binary VA: 0x101fbf960 | Size: 272 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L458
func (s *Store) SinceIn(ctx context.Context, v board.View, since uint64, limit int) ([]board.Message, uint64, error) {
	var out []board.Message
	// Binary VA: 0x101fc4fe0 | Size: 576 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L460
	head, err := s.readView(ctx, v, func(raw []byte, mainOnly bool) error {
		if mainOnly {
			msgs := parse(raw)
			for _, m := range msgs { // inlined L558-L565
				if m.Seq <= since {
					continue
				}
				if limit > 0 && len(out) == limit {
					break
				}
				out = append(out, m)
			}
			return nil
		}
		// Binary VA: 0x101fc5220 | Size: 176 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L465
		return s.filter(ctx, raw, func(r *board.Roster, msgs []board.Message) error {
			var err error
			out, err = v.Since(r, msgs, since, limit)
			return err
		})
	})
	return out, head, err
}

// Binary VA: 0x101fbfa70 | Size: 256 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L474
func (s *Store) LatestIn(ctx context.Context, v board.View, limit int) ([]board.Message, uint64, error) {
	var out []board.Message
	// Binary VA: 0x101fc52d0 | Size: 320 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L476
	head, err := s.readView(ctx, v, func(raw []byte, mainOnly bool) error {
		if mainOnly {
			msgs := parse(raw)
			if limit > 0 && len(msgs) > limit { // inlined L370-L371
				msgs = msgs[len(msgs)-limit:]
			}
			out = msgs
			return nil
		}
		// Binary VA: 0x101fc5410 | Size: 176 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L481
		return s.filter(ctx, raw, func(r *board.Roster, msgs []board.Message) error {
			var err error
			out, err = v.Latest(r, msgs, limit)
			return err
		})
	})
	return out, head, err
}

// Binary VA: 0x101fbfb70 | Size: 240 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L492
func (s *Store) VisibleAfter(ctx context.Context, v board.View, after, head uint64) (bool, error) {
	var unread bool
	// Binary VA: 0x101fc54c0 | Size: 256 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L494
	_, err := s.readView(ctx, v, func(raw []byte, mainOnly bool) error {
		if mainOnly {
			n := countLines(raw)
			if n < head {
				head = n
			}
			unread = head > after
			return nil
		}
		// Binary VA: 0x101fc55c0 | Size: 144 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L499
		return s.filter(ctx, raw, func(r *board.Roster, msgs []board.Message) error {
			var err error
			unread, err = v.AnyUnread(r, msgs, after, head)
			return err
		})
	})
	return unread, err
}

// Binary VA: 0x101fbfc60 | Size: 880 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L510
func (s *Store) readView(ctx context.Context, v board.View, fn func(raw []byte, mainOnly bool) error) (uint64, error) {
	if err := s.checkOpen(); err != nil {
		return 0, err
	}
	selectsMain, err := v.SelectsMain()
	if err != nil {
		return 0, err
	}
	s.sh.mu.RLock()
	defer s.sh.mu.RUnlock() // deferwrap1 VA: 0x101fc5650 | Size: 128 B | L519

	raw, err := readFeed(ctx, s.dir)
	if err != nil {
		return 0, err
	}
	controls, err := s.sh.ctl.get(ctx, s.dir)
	if err != nil {
		return 0, err
	}
	if len(controls) == 0 && len(v.Channels) > 0 {
		r, err := loadRosterOrEmpty(ctx, s.dir)
		if err != nil {
			return 0, err
		}
		if err := v.Validate(r); err != nil {
			return 0, err
		}
	}
	if len(controls) == 0 && selectsMain {
		return countLines(raw), nil
	}
	head := countLines(raw)
	return head, fn(raw, len(controls) == 0)
}

// Binary VA: 0x101fbffd0 | Size: 208 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L547
func (s *Store) filter(ctx context.Context, raw []byte, fn func(*board.Roster, []board.Message) error) error {
	r, err := loadRosterOrEmpty(ctx, s.dir)
	if err != nil {
		return err
	}
	return fn(r, parse(raw))
}

// Binary VA: 0x101fc00a0 | Size: 352 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L571
func (s *Store) CreateChannel(ctx context.Context, actor, name, topic string) (board.Message, error) {
	return s.channelOp(ctx, "create", actor, name, topic, nil, nil)
}

// Binary VA: 0x101fc0200 | Size: 336 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L576
func (s *Store) ArchiveChannel(ctx context.Context, actor, name string) (board.Message, error) {
	return s.channelOp(ctx, "archive", actor, name, "", nil, nil)
}

// Binary VA: 0x101fc0350 | Size: 544 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L581
func (s *Store) SubscribeChannels(ctx context.Context, actor, target string, add, remove []string) (board.Message, error) {
	op, err := board.SubscribeOp(actor, target, add, remove)
	if err != nil {
		return board.Message{}, err
	}
	return s.channelOp(ctx, op.Op, op.Actor, op.Channel, op.Topic, op.Add, op.Remove)
}

// Binary VA: 0x101fc0570 | Size: 1776 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L593
func (s *Store) channelOp(ctx context.Context, op, actor, name, topic string, add, remove []string) (board.Message, error) {
	if err := s.checkOpen(); err != nil {
		return board.Message{}, err
	}
	s.sh.mu.Lock()
	defer s.sh.mu.Unlock() // deferwrap1 VA: 0x101fc5720 | Size: 64 B | L598

	raw, err := readFeed(ctx, s.dir)
	if err != nil {
		return board.Message{}, err
	}
	r, err := loadRosterOrEmpty(ctx, s.dir)
	if err != nil {
		return board.Message{}, err
	}
	msg, err := board.PrepareChannelOp(r, op, actor, name, topic, add, remove)
	if err != nil || msg.Control == nil {
		return board.Message{}, err
	}
	msg, err = s.appendLocked(ctx, countLines(raw), msg)
	if err != nil {
		return board.Message{}, err
	}
	board.ApplyControl(r, msg)
	ls := &lockedStore{r: r}
	// Binary VA: 0x101fc56d0 | Size: 80 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L618
	ls.UpdateMemberLocked(actor, func(st *board.MemberState) {
		st.FastForwardAuthor(msg.Seq) // inlined store.go:240-245,256-258
	})
	s.sh.feed.Emit(board.Event{
		Kind:     board.EventMessagesAppended, /* inferred: 1 */
		Messages: []board.Message{msg},
		Locked:   ls,
	})
	ls.done = true
	if ls.dirty {
		s.writeCursors(ctx, r)
	}
	return msg, nil
}

// Binary VA: 0x101fc0c60 | Size: 336 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L628
func (s *Store) Roster(ctx context.Context) (*board.Roster, error) {
	if err := s.checkOpen(); err != nil {
		return nil, err
	}
	s.sh.mu.RLock()
	defer s.sh.mu.RUnlock() // deferwrap1 VA: 0x101fc5760 | Size: 128 B | L633
	return loadRosterOrEmpty(ctx, s.dir)
}

// Binary VA: 0x101fc0db0 | Size: 2880 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L642
func (s *Store) AddMembers(ctx context.Context, req board.AddMembersRequest) (board.Message, error) {
	if err := s.checkOpen(); err != nil {
		return board.Message{}, err
	}
	s.sh.mu.Lock()
	defer s.sh.mu.Unlock() // deferwrap1 VA: 0x101fc57e0 | Size: 64 B | L647

	raw, err := readFeed(ctx, s.dir)
	if err != nil {
		return board.Message{}, err
	}
	if prev, ok := findRequest(raw, req.RequestID); ok {
		if err := s.repairMembersLocked(ctx, req, prev); err != nil {
			return prev, err
		}
		return prev, nil
	}
	r, err := loadRosterOrEmpty(ctx, s.dir)
	if err != nil {
		return board.Message{}, err
	}
	head := countLines(raw)
	if err := board.ValidateAddLocal(r, req, head); err != nil {
		return board.Message{}, err
	}
	ch := board.BriefChannel(r, req)
	msg := board.Message{
		Agent:     req.Actor,
		Text:      req.BriefText,
		Kind:      "brief", /* inferred from repairMembersLocked L703 */
		RequestID: req.RequestID,
		Channel:   ch,
	}
	msg, err = s.appendLocked(ctx, head, msg)
	if err != nil {
		return board.Message{}, err
	}
	for _, nm := range req.Members {
		chs := board.InheritChannels(r, req.Actor, nm)
		r.Members[nm.Name] = board.NewMemberRow(req.Actor, msg.Seq, nm, chs)
	}
	if actor := r.Members[req.Actor]; actor != nil {
		st := actor.State()
		st.FastForwardAuthor(msg.Seq)
		actor.SetState(st)
	}
	if err := writeRoster(ctx, s.dir, r); err != nil {
		return board.Message{}, fmt.Errorf("posted brief #%d but could not add members: %w", msg.Seq, err)
	}
	ls := &lockedStore{r: r}
	s.sh.feed.Emit(board.Event{
		Kind:   board.EventRosterChanged, /* inferred: 2 */
		Roster: r.Clone(),
		Locked: ls,
	})
	s.sh.feed.Emit(board.Event{
		Kind:     board.EventMessagesAppended, /* inferred: 1 */
		Messages: []board.Message{msg},
		Locked:   ls,
	})
	ls.done = true
	if ls.dirty {
		s.writeCursors(ctx, r)
	}
	return msg, nil
}

// Binary VA: 0x101fc18f0 | Size: 1072 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L702
func (s *Store) repairMembersLocked(ctx context.Context, req board.AddMembersRequest, prev board.Message) error {
	if prev.Kind != "brief" {
		return nil
	}
	r, err := loadRosterOrEmpty(ctx, s.dir)
	if err != nil {
		return err
	}
	changed := false
	for _, nm := range req.Members {
		if existing, ok := r.Members[nm.Name]; ok && existing != nil {
			continue
		}
		chs := board.InheritChannels(r, req.Actor, nm)
		r.Members[nm.Name] = board.NewMemberRow(req.Actor, prev.Seq, nm, chs)
		changed = true
	}
	if !changed {
		return nil
	}
	if err := writeRoster(ctx, s.dir, r); err != nil {
		return fmt.Errorf("brief #%d is posted but its members could not be added: %w", prev.Seq, err)
	}
	ls := &lockedStore{r: r}
	s.sh.feed.Emit(board.Event{
		Kind:   board.EventRosterChanged, /* inferred: 2 */
		Roster: r.Clone(),
		Locked: ls,
	})
	ls.done = true
	if ls.dirty {
		s.writeCursors(ctx, r)
	}
	return nil
}

// Binary VA: 0x101fc1d20 | Size: 128 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L736
func (s *Store) KillMembers(ctx context.Context, actor string, names []string) error {
	// Binary VA: 0x101fc5820 | Size: 448 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L737
	return s.rosterOp(ctx, func(r *board.Roster) (bool, error) {
		if err := board.ValidateKill(r, actor, names); err != nil {
			return false, err
		}
		changed := false
		for _, name := range names {
			m := r.Members[name]
			if !m.IsKilled() || m.Phase != "KILLED" || m.WakeMode != "working" { // inlined members.go:183,203-208
				m.DesiredState = "KILLED"
				m.Phase = "KILLED"
				m.WakeMode = "working"
				changed = true
			}
		}
		return changed, nil
	})
}

// Binary VA: 0x101fc1da0 | Size: 128 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L752
func (s *Store) SetTitle(ctx context.Context, actor, title string) error {
	// Binary VA: 0x101fc59e0 | Size: 224 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L753
	return s.rosterOp(ctx, func(r *board.Roster) (bool, error) {
		if err := board.ValidateCoordinator(r, actor); err != nil {
			return false, err
		}
		if r.Title == title {
			return false, nil
		}
		r.Title = title
		return true, nil
	})
}

// Binary VA: 0x101fc1e20 | Size: 144 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L767
func (s *Store) SetMemberModel(ctx context.Context, actor, name, model string) error {
	// Binary VA: 0x101fc5ac0 | Size: 416 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L768
	return s.rosterOp(ctx, func(r *board.Roster) (bool, error) {
		if err := board.ValidateSetMemberModel(r, actor, name, model); err != nil {
			return false, err
		}
		m := r.Members[name]
		isSelf := name == actor
		// inlined members.go:216-219
		changed := m.DesiredModel != model
		m.DesiredModel = model
		if isSelf && m.AppliedModel != model {
			m.AppliedModel = model
			changed = true
		}
		return changed, nil
	})
}

// Binary VA: 0x101fc1eb0 | Size: 128 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L777
func (s *Store) ModelApplied(ctx context.Context, name, model string) error {
	// Binary VA: 0x101fc5c60 | Size: 80 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L778
	return s.rosterOp(ctx, func(r *board.Roster) (bool, error) {
		return board.ApplyModel(r, name, model)
	})
}

// Binary VA: 0x101fc1f30 | Size: 592 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L785
func (s *Store) rosterOp(ctx context.Context, fn func(*board.Roster) (bool, error)) error {
	if err := s.checkOpen(); err != nil {
		return err
	}
	s.sh.mu.Lock()
	defer s.sh.mu.Unlock() // deferwrap1 VA: 0x101fc5cb0 | Size: 64 B | L790

	r, err := loadRosterOrEmpty(ctx, s.dir)
	if err != nil {
		return err
	}
	changed, err := fn(r)
	if err != nil || !changed {
		return err
	}
	if err := writeRoster(ctx, s.dir, r); err != nil {
		return err
	}
	ls := &lockedStore{r: r}
	s.sh.feed.Emit(board.Event{
		Kind:   board.EventRosterChanged, /* inferred: 2 */
		Roster: r.Clone(),
		Locked: ls,
	})
	ls.done = true
	if ls.dirty {
		s.writeCursors(ctx, r)
	}
	return nil
}

// Binary VA: 0x101fc2180 | Size: 1456 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L817
func (s *Store) UpdateMember(ctx context.Context, name string, fn func(*board.MemberState) error) (board.MemberState, error) {
	if err := s.checkOpen(); err != nil {
		return board.MemberState{}, err
	}
	s.sh.mu.Lock()
	defer s.sh.mu.Unlock() // deferwrap1 VA: 0x101fc5cf0 | Size: 64 B | L822

	r, err := loadRosterOrEmpty(ctx, s.dir)
	if err != nil {
		return board.MemberState{}, err
	}
	m, ok := r.Members[name]
	if !ok || m == nil {
		return board.MemberState{}, fmt.Errorf("%w: %q", board.ErrNotMember /* inferred: 0x10d3d1710 */, name)
	}
	if m.IsKilled() { // inlined members.go:183
		return board.MemberState{}, fmt.Errorf("%w: %q", board.ErrMemberKilled /* inferred: 0x10d3d1700 */, name)
	}
	prev := m.State()
	next := new(board.MemberState)
	*next = prev
	if err := fn(next); err != nil {
		return prev, err
	}
	if *next == prev {
		return *next, nil
	}
	m.SetState(*next)
	if err := writeRoster(ctx, s.dir, r); err != nil {
		return prev, err
	}
	return *next, nil
}

// Binary VA: 0x101fc2730 | Size: 448 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L854
func (s *Store) Subscribe(fn func(board.Event)) func() {
	s.sh.mu.Lock()
	cancel := s.sh.feed.Subscribe(fn)
	s.sh.mu.Unlock()
	s.mu.Lock()
	s.cancels = append(s.cancels, cancel)
	s.mu.Unlock()
	return cancel
}

// Binary VA: 0x101fc28f0 | Size: 64 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L877
func (l *lockedStore) Names() []string {
	return l.r.SortedNames()
}

// Binary VA: 0x101fc2930 | Size: 128 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L879
func (l *lockedStore) ChannelActive(name string) bool {
	// inlined channel_mentions.go:100-101
	ch := l.r.Channels[name]
	return ch != nil && !ch.Archived
}

// Binary VA: 0x101fc29b0 | Size: 240 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L881
func (l *lockedStore) Member(name string) (*board.Member, bool) {
	m, ok := l.r.Members[name]
	if !ok || m == nil {
		return nil, false
	}
	cp := new(board.Member)
	*cp = *m
	return cp, true
}

// Binary VA: 0x101fc2aa0 | Size: 496 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L890
func (l *lockedStore) UpdateMemberLocked(name string, fn func(*board.MemberState)) {
	if l.done {
		return
	}
	m, ok := l.r.Members[name]
	if !ok || m == nil || m.IsKilled() { // inlined members.go:183
		return
	}
	prev := m.State()
	next := new(board.MemberState)
	*next = prev
	fn(next)
	if *next == prev {
		return
	}
	m.SetState(*next)
	l.dirty = true
}

// Binary VA: 0x101fc2c90 | Size: 416 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L912
func readFeed(ctx context.Context, dir fs.Path) ([]byte, error) {
	data, err := dir.Join(boardFile).ReadFile(ctx)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", boardFile, err)
	}
	return data, nil
}

// Binary VA: 0x101fc2e30 | Size: 128 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L926
func readMessages(ctx context.Context, dir fs.Path) ([]board.Message, error) {
	raw, err := readFeed(ctx, dir)
	if err != nil {
		return nil, err
	}
	return parse(raw), nil
}

// Binary VA: 0x101fc2eb0 | Size: 480 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L936
func parse(raw []byte) []board.Message {
	var out []board.Message
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		m := new(board.Message)
		if err := jsonv2.Unmarshal([]byte(line), m); err != nil {
			continue
		}
		out = append(out, *m)
	}
	return out
}

// Binary VA: 0x101fc3090 | Size: 176 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L954
func countLines(raw []byte) uint64 {
	var n uint64
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// Binary VA: 0x101fc3140 | Size: 640 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L966
func findRequest(raw []byte, requestID string) (board.Message, bool) {
	if requestID == "" {
		return board.Message{}, false
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	start := len(lines) - 64
	if start < 0 {
		start = 0
	}
	for i := len(lines) - 1; i >= start; i-- {
		line := lines[i]
		if !strings.Contains(line, requestID) {
			continue
		}
		m := new(board.Message)
		if err := jsonv2.Unmarshal([]byte(line), m); err == nil && m.RequestID == requestID {
			return *m, true
		}
	}
	return board.Message{}, false
}

// Binary VA: 0x101fc33c0 | Size: 656 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L992
func loadRoster(ctx context.Context, dir fs.Path) (*board.Roster, error) {
	raw, err := dir.Join(membersFile).ReadFile(ctx)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		return nil, fmt.Errorf("read %s: %w", membersFile, err)
	}
	r, err := decodeRoster(raw)
	if err != nil {
		return nil, err
	}
	controls, err := sharedFor(dir).ctl.get(ctx, dir)
	if err != nil {
		return nil, err
	}
	// inlined channels.go:418-420
	r.Channels = nil
	for _, msg := range controls {
		board.ApplyControl(r, msg)
	}
	return r, nil
}

// Binary VA: 0x101fc3650 | Size: 1536 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L1025
func (c *controlCache) get(ctx context.Context, dir fs.Path) ([]board.Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock() // deferwrap1 VA: 0x101fc5d30 | Size: 112 B | L1027

	info, err := dir.Join(boardFile).Stat(ctx)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.scanned = 0
			c.controls = nil
			return nil, nil
		}
		return nil, fmt.Errorf("stat %s: %w", boardFile, err)
	}
	if int(info.Size()) == c.scanned {
		return c.controls, nil
	}
	raw, err := readFeed(ctx, dir)
	if err != nil {
		return nil, err
	}
	if len(raw) < c.scanned {
		c.scanned = 0
		c.controls = nil
	}
	idx := bytes.LastIndexByte(raw, '\n')
	end := idx + 1
	if end <= c.scanned {
		return c.controls, nil
	}
	for _, line := range bytes.Split(raw[c.scanned:end], []byte{'\n'}) {
		if !bytes.Contains(line, []byte(`"control"` /* inferred: 0x10d41f4b0 len=10? */)) {
			continue
		}
		m := new(board.Message)
		if err := jsonv2.Unmarshal(line, m); err != nil || m.Control == nil {
			continue
		}
		c.controls = append(c.controls, *m)
	}
	c.scanned = end
	return c.controls, nil
}

// Binary VA: 0x101fc3c50 | Size: 240 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L1066
func loadRosterOrEmpty(ctx context.Context, dir fs.Path) (*board.Roster, error) {
	r, err := loadRoster(ctx, dir)
	if errors.Is(err, os.ErrNotExist) {
		return &board.Roster{
			Members: map[string]*board.Member{},
		}, nil
	}
	return r, err
}

// Binary VA: 0x101fc3d40 | Size: 608 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L1074
func decodeRoster(raw []byte) (*board.Roster, error) {
	fr := new(fileRoster)
	if err := jsonv2.Unmarshal(raw, fr); err != nil {
		return nil, fmt.Errorf("parse %s: %w", membersFile, err)
	}
	members := make(map[string]*board.Member, len(fr.Members))
	r := &board.Roster{
		Title:   fr.Title,
		Members: members,
	}
	for name, fm := range fr.Members {
		if fm == nil {
			continue
		}
		members[name] = fm.member()
	}
	return r, nil
}

// Binary VA: 0x101fc3fa0 | Size: 1088 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L1092
func writeRoster(ctx context.Context, dir fs.Path, r *board.Roster) error {
	if r.Members == nil {
		r.Members = map[string]*board.Member{}
	}
	fr := toFileRoster(r) // inlined members_file.go:91-96
	data, err := json.MarshalIndent(fr, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", membersFile, err)
	}
	if err := dir.MkdirAll(ctx, 0o755); err != nil {
		return fmt.Errorf("create swarm directory: %w", err)
	}
	if err := dir.Join(membersFile).AtomicWrite(ctx, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", membersFile, err)
	}
	return nil
}

// Binary VA: 0x101fc43e0 | Size: 208 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/filestore.go:L1112
func updateRoster(ctx context.Context, dir fs.Path, fn func(*board.Roster) (bool, error)) error {
	r, err := loadRosterOrEmpty(ctx, dir)
	if err != nil {
		return err
	}
	changed, err := fn(r)
	if err != nil || !changed {
		return err
	}
	return writeRoster(ctx, dir, r)
}
