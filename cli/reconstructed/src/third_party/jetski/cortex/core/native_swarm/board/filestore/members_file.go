// Reconstructed from agy v1.5.10 arm64 Mach-O binary
// Source: third_party/jetski/cortex/core/native_swarm/board/filestore/members_file.go

package filestore

import (
	jsonv2 "encoding/json/v2"

	"google3/third_party/jetski/cortex/core/native_swarm/board/board"
)

type fileRoster struct {
	Title   string                 `json:"title"`
	Members map[string]*fileMember `json:"members"`
}

type legacyMember struct {
	Killed         bool   `json:"killed"`
	Model          string `json:"model"`
	HostInstanceID string `json:"host_instance_id,omitempty"`
}

// plain mirrors board.Member without methods to avoid MarshalJSON/UnmarshalJSON recursion.
type plain board.Member

type fileMember struct {
	board.Member
	legacy legacyMember
}

// Binary VA: 0x101fc44b0 | Size: 304 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/members_file.go:L34
func (m fileMember) MarshalJSON() ([]byte, error) {
	wire := struct { /* inferred */
		plain
		legacyMember
	}{
		plain: plain(m.Member),
		legacyMember: legacyMember{
			Killed:         m.IsKilled(), // inlined members.go:183
			Model:          m.EffectiveModel(), // inlined members.go:194,197
			HostInstanceID: m.NodeID,
		},
	}
	return jsonv2.Marshal(wire)
}

// Binary VA: 0x101fc45e0 | Size: 336 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/members_file.go:L48
func (m *fileMember) UnmarshalJSON(data []byte) error {
	raw := new(struct { /* inferred */
		plain
		legacyMember
	})
	if err := jsonv2.Unmarshal(data, raw); err != nil {
		return err
	}
	m.Member = board.Member(raw.plain)
	m.legacy = raw.legacyMember
	return nil
}

// Binary VA: 0x101fc4730 | Size: 512 B | Source: third_party/jetski/cortex/core/native_swarm/board/filestore/members_file.go:L68
func (m *fileMember) member() *board.Member {
	out := new(board.Member)
	*out = m.Member
	if out.Phase == "" {
		out.Phase = "RUNNING"
	}
	if out.DesiredState == "" {
		out.DesiredState = "RUNNING"
	}
	if m.legacy.Killed {
		out.DesiredState = "KILLED"
		out.Phase = "KILLED"
	}
	if out.NodeID == "" {
		out.NodeID = m.legacy.HostInstanceID
	}
	if m.legacy.Model != "" && out.EffectiveModel() != m.legacy.Model {
		out.DesiredModel = m.legacy.Model
		out.AppliedModel = m.legacy.Model
	}
	return out
}

// toFileRoster (inlined into writeRoster at filestore.go:1094; Source: members_file.go:L91-L96)
func toFileRoster(r *board.Roster) *fileRoster { /* inferred */
	members := make(map[string]*fileMember, len(r.Members))
	out := &fileRoster{
		Title:   r.Title,
		Members: members,
	}
	for name, m := range r.Members {
		if m == nil {
			continue
		}
		members[name] = &fileMember{Member: *m}
	}
	return out
}
