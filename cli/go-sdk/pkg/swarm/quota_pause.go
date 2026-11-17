package swarm

import "sync"

type quotaPauseEntry struct {
	head  uint64
	model string
	traj  string
}

// QuotaPauseTracker lets a worker that hits a quota limit sit out until the board moves past head.
type QuotaPauseTracker struct {
	mu     sync.Mutex
	paused map[string]quotaPauseEntry
}

func NewQuotaPauseTracker() *QuotaPauseTracker {
	return &QuotaPauseTracker{
		paused: make(map[string]quotaPauseEntry),
	}
}

// PauseOnQuota records that member ran out of quota at store.Head().
func (q *QuotaPauseTracker) PauseOnQuota(store *FileStore, name, model, traj string) uint64 {
	head := store.Head()
	q.mu.Lock()
	q.paused[name] = quotaPauseEntry{
		head:  head,
		model: model,
		traj:  traj,
	}
	q.mu.Unlock()
	return head
}

// SitOutForQuota returns true while member remains paused and no new message has passed p.head.
// When the board head advances past p.head, the pause clears and returns false.
func (q *QuotaPauseTracker) SitOutForQuota(store *FileStore, name string) bool {
	q.mu.Lock()
	entry, ok := q.paused[name]
	q.mu.Unlock()
	if !ok {
		return false
	}
	if store.Head() > entry.head {
		q.mu.Lock()
		delete(q.paused, name)
		q.mu.Unlock()
		return false
	}
	return true
}

func (q *QuotaPauseTracker) Clear(name string) {
	q.mu.Lock()
	delete(q.paused, name)
	q.mu.Unlock()
}
