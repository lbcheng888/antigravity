// Reconstructed from agy v1.5.10 binary evidence (disassembly + Go type table).
// Binary VA: 0x102321200 | Size: 23 funcs (first func NewStream, 880 B) | Source: third_party/jetski/cli/acp/session/stream.go:L55
// Reconstructed from agy v1.5.10 arm64 binary.
// Source: third_party/jetski/cli/acp/session/stream.go

package session

import (
	"sync"
	"sync/atomic"

	"google3/third_party/jetski/cli/backend/backend"
	cortex_go_proto "google3/third_party/jetski/cortex_pb/cortex_go_proto"
	gemini_coder_go_proto "google3/third_party/jetski/gemini_coder_pb/gemini_coder_go_proto"
)

type Streamer interface {
	StreamConversationUpdates(conversationID string, callbacks backend.StreamCallbacks) func()
}

// Stream multiplexes conversation updates between an active turn callback set
// and an out-of-turn callback set, with hold support during turn handoffs.
// Struct size: 208 B (0xd0).
type Stream struct {
	dispatch  sync.RWMutex
	mu        sync.Mutex
	turn      *backend.StreamCallbacks
	hold      chan struct{}
	closed    bool
	lost      atomic.Bool
	outOfTurn backend.StreamCallbacks
	stop      func()
}

// Binary VA: 0x102321200 | Size: 880 B | Source: third_party/jetski/cli/acp/session/stream.go:L55
func NewStream(streamer Streamer, convID string, turn *backend.StreamCallbacks, outOfTurn backend.StreamCallbacks) *Stream {
	s := &Stream{
		turn:      turn,
		outOfTurn: outOfTurn,
	}
	stop := streamer.StreamConversationUpdates(convID, backend.StreamCallbacks{
		OnSteps: func(indices []int, steps []*gemini_coder_go_proto.Step) {
			// Binary VA: 0x102325940 | Size: 128 B | Source: third_party/jetski/cli/acp/session/stream.go:L58
			s.deliver(func(cb *backend.StreamCallbacks) {
				// Binary VA: 0x1023259c0 | Size: 96 B | Source: third_party/jetski/cli/acp/session/stream.go:L59
				if cb.OnSteps != nil {
					cb.OnSteps(indices, steps)
				}
			})
		},
		OnStatus: func(status cortex_go_proto.CascadeRunStatus) {
			// Binary VA: 0x102325890 | Size: 96 B | Source: third_party/jetski/cli/acp/session/stream.go:L65
			s.deliver(func(cb *backend.StreamCallbacks) {
				// Binary VA: 0x1023258f0 | Size: 80 B | Source: third_party/jetski/cli/acp/session/stream.go:L66
				if cb.OnStatus != nil {
					cb.OnStatus(status)
				}
			})
		},
		OnUpdateEnd: func() {
			// Binary VA: 0x102325850 | Size: 64 B | Source: third_party/jetski/cli/acp/session/stream.go:L72
			s.deliver(func(cb *backend.StreamCallbacks) {
				// Binary VA: 0x102326700 | Size: 80 B | Source: third_party/jetski/cli/acp/session/stream.go:L73
				if cb.OnUpdateEnd != nil {
					cb.OnUpdateEnd()
				}
			})
		},
		OnStreamError: func(err error) {
			// Binary VA: 0x102325790 | Size: 112 B | Source: third_party/jetski/cli/acp/session/stream.go:L79
			s.lost.Store(true)
			s.deliver(func(cb *backend.StreamCallbacks) {
				// Binary VA: 0x102325800 | Size: 80 B | Source: third_party/jetski/cli/acp/session/stream.go:L81
				if cb.OnStreamError != nil {
					cb.OnStreamError(err)
				}
			})
		},
	})

	s.mu.Lock()
	closed := s.closed
	s.stop = stop
	s.mu.Unlock()

	if closed && stop != nil {
		stop()
	}
	return s
}

// Binary VA: 0x102321570 | Size: 640 B | Source: third_party/jetski/cli/acp/session/stream.go:L101
func (s *Stream) deliver(fn func(*backend.StreamCallbacks)) {
	for {
		s.dispatch.RLock()
		s.mu.Lock()
		turn := s.turn
		hold := s.hold
		closed := s.closed
		s.mu.Unlock()

		if closed {
			s.dispatch.RUnlock()
			return
		}
		if turn != nil {
			fn(turn)
			s.dispatch.RUnlock()
			return
		}
		if hold != nil {
			s.dispatch.RUnlock()
			<-hold
			continue
		}
		fn(&s.outOfTurn)
		s.dispatch.RUnlock()
		return
	}
}

// Binary VA: 0x1023217f0 | Size: 320 B | Source: third_party/jetski/cli/acp/session/stream.go:L132
func (s *Stream) attach(turn *backend.StreamCallbacks) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turn = turn
	if s.hold != nil {
		close(s.hold)
		s.hold = nil
	}
}

// Binary VA: 0x102321930 | Size: 768 B | Source: third_party/jetski/cli/acp/session/stream.go:L143
func (s *Stream) Detach(turn *backend.StreamCallbacks) func() {
	s.dispatch.Lock()
	defer s.dispatch.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.turn != turn {
		// Binary VA: 0x102326790 | Size: 16 B | Source: third_party/jetski/cli/acp/session/stream.go:L149
		return func() {}
	}
	s.turn = nil
	if s.hold != nil {
		close(s.hold)
		s.hold = nil
	}
	hold := make(chan struct{})
	s.hold = hold
	return sync.OnceFunc(func() {
		// Binary VA: 0x102325a90 | Size: 288 B | Source: third_party/jetski/cli/acp/session/stream.go:L155
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.hold == hold && s.hold != nil {
			close(s.hold)
			s.hold = nil
		}
	})
}

// Binary VA: 0x102321c30 | Size: 448 B | Source: third_party/jetski/cli/acp/session/stream.go:L174
func (s *Stream) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.turn = nil
	if s.hold != nil {
		close(s.hold)
		s.hold = nil
	}
	stop := s.stop
	s.mu.Unlock()
	if stop != nil {
		stop()
	}
}

// Binary VA: 0x102321df0 | Size: 288 B | Source: third_party/jetski/cli/acp/session/stream.go:L191
func (s *Stream) usable() bool {
	if s == nil || s.lost.Load() {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed
}

// Binary VA: 0x102321f10 | Size: 656 B | Source: third_party/jetski/cli/acp/session/stream.go:L206
func (s *State) AttachTurn(convID string, turn *backend.StreamCallbacks, getStreamer func() Streamer, outOfTurn func() backend.StreamCallbacks) *Stream {
	s.streamMu.Lock()
	defer s.streamMu.Unlock()

	if s.Closed() {
		return nil
	}
	if s.stream.usable() {
		if turn != nil {
			s.stream.attach(turn)
		}
		return s.stream
	}
	streamer := getStreamer()
	if streamer == nil {
		return nil
	}
	if s.stream != nil {
		s.stream.close()
	}
	s.stream = NewStream(streamer, convID, turn, outOfTurn())
	return s.stream
}
