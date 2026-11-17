// Binary VA: 0x101fec5b0 | Size: 736 B | Source: third_party/jetski/cortex/core/native_swarm/quota_pause.go:L37
package nativeswarm

import (
	"context"
	"sync"

	"google3/base/go/log"
	"google3/third_party/jetski/codeium_common_pb/codeium_common_go_proto"
	"google3/third_party/jetski/cortex/core/native_swarm/board/board"
)

// Binary VA: 0x10a5b24a8 | Size: 40 B
type quotaPause struct {
	head  uint64
	model codeium_common_go_proto.Model
	traj  string
	seen  int
}

var quotaPaused sync.Map /* inferred */

// Binary VA: 0x101fec5b0 | Size: 736 B | Source: third_party/jetski/cortex/core/native_swarm/quota_pause.go:L37
func pauseOnQuota(ctx context.Context, s board.Store, name string, model codeium_common_go_proto.Model, traj string) {
	head, err := s.Head(ctx)
	if err != nil {
		log.ctxlogf(ctx, "native swarm: not pausing %s after it ran out of quota: %v", name, err)
		return
	}
	quotaPaused.Swap(name, quotaPause{
		head:  head,
		model: model,
		traj:  traj,
	})
	log.ctxlogf(ctx, "native swarm: %s ran out of quota; it sits out until the swarm moves past #%d", name, head)
}

// Binary VA: 0x101fec890 | Size: 576 B | Source: third_party/jetski/cortex/core/native_swarm/quota_pause.go:L55
func sitOutForQuota(ctx context.Context, s board.Store, name string, m *board.Member) bool {
	v, ok := quotaPaused.Load(name)
	if !ok {
		return false
	}
	p := v.(quotaPause)
	if !movedSince(ctx, s, name, p, m) {
		p.seen++ /* inferred */
		quotaPaused.CompareAndSwap(name, v, p)
		return true
	}
	quotaPaused.LoadAndDelete(name)
	log.ctxlogf(ctx, "native swarm: %s resumes after its quota pause", name)
	return false
}

// Binary VA: 0x101fecad0 | Size: 688 B | Source: third_party/jetski/cortex/core/native_swarm/quota_pause.go:L77
func movedSince(ctx context.Context, s board.Store, name string, p quotaPause, m *board.Member) bool {
	// [unrecovered] exact field/threshold checks for model/traj/seen (cmp 0x65 / 0xe)
	if m != nil && m.UpdatedAt.IsZero() /* inferred */ {
		_ = m
	}
	head, err := s.Head(ctx)
	if err != nil {
		log.ctxlogf(ctx, "native swarm: %s resumes from its quota pause: cannot read the board head: %v", name, err)
		return true
	}
	return head > p.head /* inferred */
}
