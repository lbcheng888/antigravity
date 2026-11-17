// Binary VA: 0x101fb7aa0 | Size: 384 B | Source: third_party/jetski/cortex/core/native_swarm/board/members.go:L69
package board

import "fmt"

// RunState represents the live execution state of a member's conversation.
/* inferred */
type RunState int

const (
	RunWorking RunState = 0 /* inferred */
	RunWaiting RunState = 1 /* inferred */
	RunStopped RunState = 2 /* inferred */
)

// Binary VA: 0x101fb7aa0 | Size: 384 B | Source: third_party/jetski/cortex/core/native_swarm/board/members.go:L69
func StateOf(m *Member, run RunState /* inferred */) string {
	if m.Killed() || m.Phase == PhaseKilled {
		return "killed"
	}
	if m.Phase != "" && m.Phase != PhaseRunning {
		return fmt.Sprintf("unknown (%s)", m.Phase)
	}
	switch run {
	case RunWaiting:
		return "waiting"
	case RunStopped:
		return "stopped"
	default:
		return "working"
	}
}

// Binary VA: 0x101fb7c20 | Size: 64 B | Source: third_party/jetski/cortex/core/native_swarm/board/members.go:L183
func (m *Member) Killed() bool {
	return m.DesiredState == DesiredKilled
}

// Binary VA: 0x101fb7c60 | Size: 144 B | Source: third_party/jetski/cortex/core/native_swarm/board/members.go:L187
func (m *Member) Live() bool {
	if m.Killed() {
		return false
	}
	return m.Phase == PhaseRunning || m.Phase == ""
}

// Binary VA: 0x101fb7cf0 | Size: 32 B | Source: third_party/jetski/cortex/core/native_swarm/board/members.go:L193
func (m *Member) Model() string {
	if m.AppliedModel != "" {
		return m.AppliedModel
	}
	return m.DesiredModel
}

// Binary VA: 0x101fb7d10 | Size: 304 B | Source: third_party/jetski/cortex/core/native_swarm/board/members.go:L202
func (m *Member) MarkKilled() bool {
	if m.Killed() && m.Phase == PhaseKilled {
		return false
	}
	m.DesiredState = DesiredKilled
	m.Phase = PhaseKilled
	m.WakeMode = "working" /* inferred: WakeMode constant for working/clear */
	return true
}

// Binary VA: 0x101fb7e40 | Size: 256 B | Source: third_party/jetski/cortex/core/native_swarm/board/members.go:L237
func (r *Roster) NameOf(convID string) (string, bool) {
	if r == nil {
		return "", false
	}
	for name, m := range r.Members {
		if m != nil && m.ConversationID == convID {
			return name, true
		}
	}
	return "", false
}
