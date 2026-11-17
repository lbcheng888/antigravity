// Reconstructed from agy v1.5.10 binary evidence (disassembly + Go type table).
// Binary VA: 0x1023221a0 | Size: 52 funcs (first func Steer, 16 B) | Source: third_party/jetski/cli/acp/session/turn.go:L87
// Reconstructed from agy v1.5.10 arm64 binary.
// Source: third_party/jetski/cli/acp/session/turn.go

package session

import (
	"context"
	"errors"
	"sync"
	"time"

	"google3/third_party/jetski/cli/acp/messages/messages"
)

const defaultCancelGrace = 5 * time.Second

var (
	errTurnCancelling = errors.New("the running prompt is being cancelled; wait for it to finish, then send this prompt again")
	errTurnBlocked    = errors.New("the running prompt is waiting for a permission or question answer; answer it or send session/cancel, then send this prompt again")
	errSessionClosed  = errors.New("the session was closed or reopened; send this prompt again after reopening it")
)

// Turn tracks the active prompt turn's lifecycle channels and open client interactions.
// Struct size: 72 B (0x48).
type Turn struct {
	owner        *turnOwner
	stop         func() func()
	resetRun     func()
	ready        chan struct{}
	yield        chan struct{}
	released     chan struct{}
	steerDone    chan struct{}
	finishing    bool
	streamLost   bool
	interactions map[*clientInteraction]struct{}
}

// turnOwner identifies a specific acquisition of a Turn.
// Struct size: 16 B (0x10).
type turnOwner struct {
	finished   chan struct{}
	superseded chan *messages.Usage
}

// TurnEnd summarizes the outcome of a completed turn.
// Struct size: 16 B (0x10).
type TurnEnd struct {
	Usage     *messages.Usage
	Cancelled bool
}

// Lease represents an active caller's ownership of a Turn.
// Struct size: 32 B (0x20).
type Lease struct {
	turn     *Turn
	owner    *turnOwner
	steer    bool
	baseStep int
}

// clientInteraction tracks a cancellable permission or question interaction.
// Struct size: 40 B (0x28).
type clientInteraction struct {
	ToolPermission
	questionStep int
	cancel       context.CancelFunc
}

// Fence gates work against session closure.
// Struct size: 28 B (0x1c).
type Fence struct {
	mu     sync.RWMutex
	closed bool
}

func newTurnOwner() *turnOwner {
	return &turnOwner{
		finished:   make(chan struct{}, 1),
		superseded: make(chan *messages.Usage, 1),
	}
}

// Binary VA: 0x1023221a0 | Size: 16 B | Source: third_party/jetski/cli/acp/session/turn.go:L87
func (l Lease) Steer() bool {
	return l.steer
}

// Binary VA: 0x1023221b0 | Size: 16 B | Source: third_party/jetski/cli/acp/session/turn.go:L90
func (l Lease) BaseStep() int {
	return l.baseStep
}

// Binary VA: 0x1023221c0 | Size: 16 B | Source: third_party/jetski/cli/acp/session/turn.go:L93
func (l Lease) Turn() *Turn {
	return l.turn
}

// Binary VA: 0x1023221d0 | Size: 16 B | Source: third_party/jetski/cli/acp/session/turn.go:L96
func (l Lease) Finished() <-chan struct{} {
	return l.owner.finished
}

// Binary VA: 0x1023221e0 | Size: 16 B | Source: third_party/jetski/cli/acp/session/turn.go:L100
func (l Lease) Superseded() <-chan *messages.Usage {
	return l.owner.superseded
}

// Binary VA: 0x1023221f0 | Size: 16 B | Source: third_party/jetski/cli/acp/session/turn.go:L103
func (t *Turn) Released() <-chan struct{} {
	return t.released
}

// Binary VA: 0x102322200 | Size: 16 B | Source: third_party/jetski/cli/acp/session/turn.go:L107
func (t *Turn) Yielded() <-chan struct{} {
	return t.yield
}

// Binary VA: 0x102322210 | Size: 80 B | Source: third_party/jetski/cli/acp/session/turn.go:L111
func (t *Turn) Stop() func() {
	if t == nil || t.stop == nil {
		return nil
	}
	return t.stop()
}

// Binary VA: 0x102322260 | Size: 1600 B | Source: third_party/jetski/cli/acp/session/turn.go:L123
func (s *State) AcquireTurn() (Lease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for {
		if s.closed {
			return Lease{}, errSessionClosed
		}
		turn := s.turn
		if turn == nil {
			if s.cancelsInFlight > 0 {
				ch := s.cancelsDone
				s.mu.Unlock()
				<-ch
				s.mu.Lock()
				continue
			}
			owner := newTurnOwner()
			turn = &Turn{
				owner:    owner,
				ready:    make(chan struct{}),
				yield:    make(chan struct{}),
				released: make(chan struct{}),
			}
			s.turn = turn
			clear(s.pendingStepUsage)
			baseStep := s.nextStepIdx - 1
			if s.askedQuestion != nil && s.askedQuestion.Step > baseStep {
				baseStep = s.askedQuestion.Step
			}
			s.minTurnStepIdx = baseStep
			s.turnCancelled = false
			return Lease{
				turn:     turn,
				owner:    owner,
				baseStep: baseStep,
			}, nil
		}

		// Wait for the turn to become ready before attempting to steer.
		select {
		case <-turn.ready:
		default:
			ready := turn.ready
			s.mu.Unlock()
			<-ready
			s.mu.Lock()
			continue
		}

		// Wait if finishing, yielded, or a question is active, or another steer is in flight.
		var waitCh chan struct{}
		if !turn.finishing {
			select {
			case <-turn.yield:
				waitCh = turn.released
			default:
				if s.askedQuestion != nil {
					waitCh = turn.released
				} else if turn.steerDone != nil {
					waitCh = turn.steerDone
				}
			}
		} else {
			waitCh = turn.released
		}

		if waitCh != nil {
			s.mu.Unlock()
			<-waitCh
			s.mu.Lock()
			continue
		}

		if s.turnCancelled {
			return Lease{}, errTurnCancelling
		}
		if len(turn.interactions) > 0 {
			return Lease{}, errTurnBlocked
		}

		turn.steerDone = make(chan struct{})
		owner := newTurnOwner()
		return Lease{
			turn:  turn,
			owner: owner,
			steer: true,
		}, nil
	}
}

// Binary VA: 0x1023228a0 | Size: 496 B | Source: third_party/jetski/cli/acp/session/turn.go:L182
func (s *State) FinishSteer(lease Lease, ok bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	turn := lease.turn
	close(turn.steerDone)
	turn.steerDone = nil

	if s.turnCancelled {
		turn.finishing = true
	} else if ok {
		usage := s.consumeTurnUsageWithMuHeld()
		turn.owner.superseded <- usage
		turn.owner = lease.owner
		turn.finishing = turn.streamLost
		if turn.resetRun != nil {
			turn.resetRun()
		}
	} else {
		turn.finishing = true
	}

	if turn.finishing {
		select {
		case turn.owner.finished <- struct{}{}:
		default:
		}
	}
	return turn.finishing
}

// Binary VA: 0x102322a90 | Size: 320 B | Source: third_party/jetski/cli/acp/session/turn.go:L211
func (s *State) MarkTurnReady(turn *Turn, stop func() func(), resetRun func()) {
	s.mu.Lock()
	defer s.mu.Unlock()

	turn.stop = stop
	turn.resetRun = resetRun
	close(turn.ready)
}

// Binary VA: 0x102322bd0 | Size: 240 B | Source: third_party/jetski/cli/acp/session/turn.go:L220
func (s *State) SignalStreamLost(turn *Turn) {
	s.mu.Lock()
	turn.streamLost = true
	s.mu.Unlock()
	s.SignalTurnFinished(turn)
}

// Binary VA: 0x102322cc0 | Size: 256 B | Source: third_party/jetski/cli/acp/session/turn.go:L229
func (s *State) SignalTurnFinished(turn *Turn) {
	s.mu.Lock()
	defer s.mu.Unlock()

	turn.finishing = true
	if turn.steerDone == nil {
		select {
		case turn.owner.finished <- struct{}{}:
		default:
		}
	}
}

// Binary VA: 0x102322dc0 | Size: 320 B | Source: third_party/jetski/cli/acp/session/turn.go:L247
func (s *State) FinishTurnAfter(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if turn := s.turn; turn != nil {
		time.AfterFunc(d, func() {
			// Binary VA: 0x102325f70 | Size: 64 B | Source: third_party/jetski/cli/acp/session/turn.go:L251
			s.SignalTurnFinished(turn)
		})
	}
}

// Binary VA: 0x102322f00 | Size: 32 B | Source: third_party/jetski/cli/acp/session/turn.go:L262
func (s *State) CancelGrace() time.Duration {
	if s.cancelGrace != 0 {
		return s.cancelGrace
	}
	return defaultCancelGrace
}

// Binary VA: 0x102322f20 | Size: 416 B | Source: third_party/jetski/cli/acp/session/turn.go:L273
func (s *State) holdTurnWithMuHeld() func() {
	if s.cancelsInFlight == 0 {
		s.cancelsDone = make(chan struct{})
	}
	s.cancelsInFlight++
	release := sync.OnceFunc(func() {
		// Binary VA: 0x102326020 | Size: 272 B | Source: third_party/jetski/cli/acp/session/turn.go:L278
		s.mu.Lock()
		defer s.mu.Unlock()
		s.cancelsInFlight--
		if s.cancelsInFlight == 0 {
			close(s.cancelsDone)
		}
		if s.turn == nil && s.cancelsInFlight == 0 {
			s.turnCancelled = false
		}
	})
	time.AfterFunc(s.CancelGrace(), release)
	return release
}

// Binary VA: 0x1023230c0 | Size: 576 B | Source: third_party/jetski/cli/acp/session/turn.go:L295
func (s *State) HoldOwnedTurn(lease Lease) (func(), bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	turn := lease.turn
	for turn.steerDone != nil {
		ch := turn.steerDone
		s.mu.Unlock()
		<-ch
		s.mu.Lock()
	}
	if s.turn != turn || turn.owner != lease.owner {
		return nil, false
	}
	turn.finishing = true
	return s.holdTurnWithMuHeld(), true
}

// Binary VA: 0x102323300 | Size: 128 B | Source: third_party/jetski/cli/acp/session/turn.go:L316
func (s *State) EndTurn(lease Lease) TurnEnd {
	usage, _, superseded := s.FinishTurn(lease)
	if !superseded {
		return TurnEnd{}
	}
	return TurnEnd{
		Usage:     usage,
		Cancelled: true,
	}
}

// Binary VA: 0x102323380 | Size: 1136 B | Source: third_party/jetski/cli/acp/session/turn.go:L334
func (s *State) FinishTurn(lease Lease) (*messages.Usage, bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	turn := lease.turn
	for {
		if turn.steerDone != nil {
			ch := turn.steerDone
			s.mu.Unlock()
			<-ch
			s.mu.Lock()
			continue
		}
		if s.cancelsInFlight > 0 {
			ch := s.cancelsDone
			s.mu.Unlock()
			<-ch
			s.mu.Lock()
			continue
		}
		break
	}

	if turn.owner != lease.owner {
		usage := <-lease.owner.superseded
		return usage, false, true
	}

	usage := s.consumeTurnUsageWithMuHeld()
	cancelled := s.turnCancelled
	if s.turn == turn {
		s.turn = nil
	}
	keepOpen := !s.turnCancelled && !turn.streamLost && !s.closed
	if turn.streamLost && !s.turnCancelled {
		s.withdrawOpenWithMuHeld(true)
	}

	for ci := range turn.interactions {
		if keepOpen {
			delete(turn.interactions, ci)
			if s.openInteractions == nil {
				s.openInteractions = make(map[*clientInteraction]struct{})
			}
			s.openInteractions[ci] = struct{}{}
		} else {
			delete(turn.interactions, ci)
			ci.cancel()
			if !s.turnCancelled {
				if ci.questionStep >= 0 {
					delete(s.sentQuestions, ci.questionStep)
				} else {
					delete(s.sentToolPermissions, ci.ToolPermission)
				}
			}
		}
	}

	select {
	case <-turn.ready:
	default:
		close(turn.ready)
	}
	close(turn.released)
	if s.turn == nil && s.cancelsInFlight == 0 {
		s.turnCancelled = false
	}
	return usage, cancelled, false
}

// Binary VA: 0x1023237f0 | Size: 256 B | Source: third_party/jetski/cli/acp/session/turn.go:L394
func (s *State) YieldTurn() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if turn := s.turn; turn != nil {
		select {
		case <-turn.yield:
		default:
			close(turn.yield)
		}
	}
}

// Binary VA: 0x1023238f0 | Size: 416 B | Source: third_party/jetski/cli/acp/session/turn.go:L404
func (s *State) WaitForSteer() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if turn := s.turn; turn != nil && turn.steerDone != nil {
		ch := turn.steerDone
		s.mu.Unlock()
		<-ch
		s.mu.Lock()
	}
}

// Binary VA: 0x102323a90 | Size: 80 B | Source: third_party/jetski/cli/acp/session/turn.go:L418
func (s *State) BeginClientInteraction(perm ToolPermission) (context.Context, context.CancelFunc, func()) {
	return s.beginInteraction(perm, -1)
}

// Binary VA: 0x102323ae0 | Size: 80 B | Source: third_party/jetski/cli/acp/session/turn.go:L424
func (s *State) BeginQuestionForm(stepIdx int) (context.Context, context.CancelFunc, func()) {
	return s.beginInteraction(ToolPermission{}, stepIdx)
}

// Binary VA: 0x102323b30 | Size: 720 B | Source: third_party/jetski/cli/acp/session/turn.go:L428
func (s *State) beginInteraction(perm ToolPermission, questionStep int) (context.Context, context.CancelFunc, func()) {
	baseCtx, _ := s.InteractionContext()
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, cancel := context.WithCancel(baseCtx)
	ci := &clientInteraction{
		ToolPermission: perm,
		questionStep:   questionStep,
		cancel:         cancel,
	}
	turn := s.turn
	if questionStep < 0 {
		s.withdrawWithMuHeld(perm)
	}
	release := func() {
		// Binary VA: 0x102326360 | Size: 304 B | Source: third_party/jetski/cli/acp/session/turn.go:L440
		s.mu.Lock()
		defer s.mu.Unlock()
		if turn != nil {
			delete(turn.interactions, ci)
		}
		delete(s.openInteractions, ci)
		ci.cancel()
	}
	if turn != nil {
		if turn.interactions == nil {
			turn.interactions = make(map[*clientInteraction]struct{})
		}
		turn.interactions[ci] = struct{}{}
		return ctx, cancel, release
	}
	if s.openInteractions == nil {
		s.openInteractions = make(map[*clientInteraction]struct{})
	}
	s.openInteractions[ci] = struct{}{}
	return ctx, cancel, release
}

// Binary VA: 0x102323e00 | Size: 272 B | Source: third_party/jetski/cli/acp/session/turn.go:L474
func (s *State) withdrawWithMuHeld(perm ToolPermission) {
	if perm.ToolCallID == "" {
		return
	}
	if s.turn != nil {
		s.turn.withdrawWithMuHeld(perm)
	}
	for ci := range s.openInteractions {
		if ci.ToolPermission == perm {
			delete(s.openInteractions, ci)
			ci.cancel()
		}
	}
}

// Binary VA: 0x102323f10 | Size: 224 B | Source: third_party/jetski/cli/acp/session/turn.go:L491
func (s *State) WithdrawOpenInteractions() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.withdrawOpenWithMuHeld(true)
}

// Binary VA: 0x102323ff0 | Size: 272 B | Source: third_party/jetski/cli/acp/session/turn.go:L499
func (s *State) withdrawOpenWithMuHeld(resetDedup bool) {
	for ci := range s.openInteractions {
		delete(s.openInteractions, ci)
		ci.cancel()
		if resetDedup {
			if ci.questionStep >= 0 {
				delete(s.sentQuestions, ci.questionStep)
			} else {
				delete(s.sentToolPermissions, ci.ToolPermission)
			}
		}
	}
}

// Binary VA: 0x102324100 | Size: 240 B | Source: third_party/jetski/cli/acp/session/turn.go:L517
func (s *State) WithdrawInteraction(perm ToolPermission) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.withdrawWithMuHeld(perm)
}

// Binary VA: 0x1023241f0 | Size: 256 B | Source: third_party/jetski/cli/acp/session/turn.go:L525
func (t *Turn) withdrawWithMuHeld(perm ToolPermission) {
	if perm.ToolCallID == "" {
		return
	}
	for ci := range t.interactions {
		if ci.ToolPermission == perm {
			delete(t.interactions, ci)
			ci.cancel()
		}
	}
}

// Binary VA: 0x1023242f0 | Size: 512 B | Source: third_party/jetski/cli/acp/session/turn.go:L538
func (s *State) EndQuestionForm(stepIdx int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.turn != nil {
		for ci := range s.turn.interactions {
			if ci.questionStep == stepIdx {
				delete(s.turn.interactions, ci)
				ci.cancel()
			}
		}
	}
	for ci := range s.openInteractions {
		if ci.questionStep == stepIdx {
			delete(s.openInteractions, ci)
			ci.cancel()
		}
	}
}

// Binary VA: 0x1023244f0 | Size: 240 B | Source: third_party/jetski/cli/acp/session/turn.go:L580
func (f *Fence) Enter() bool {
	f.mu.RLock()
	if f.closed {
		f.mu.RUnlock()
		return false
	}
	return true
}

// Binary VA: 0x1023245e0 | Size: 128 B | Source: third_party/jetski/cli/acp/session/turn.go:L590
func (f *Fence) Exit() {
	f.mu.RUnlock()
}

// Binary VA: 0x102324660 | Size: 144 B | Source: third_party/jetski/cli/acp/session/turn.go:L593
func (f *Fence) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
}
