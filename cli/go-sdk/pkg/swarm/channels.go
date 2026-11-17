package swarm

import (
	"fmt"
	"regexp"
	"slices"
)

var roleSuffixRE = regexp.MustCompile(`-(monitor|narrator|coordinator)(?:-\d+)?$`)

// RoleOf resolves canonical swarm role from explicit role or suffix convention.
func RoleOf(name, role string) string {
	if role != "" && role != "worker" {
		return role
	}
	if m := roleSuffixRE.FindStringSubmatch(name); len(m) > 1 {
		return m[1]
	}
	return name
}

// EffectiveChannels defaults monitor/narrator/coordinator roles to `["*"]` when unset.
func EffectiveChannels(name, role string, channels []string) []string {
	if len(channels) != 0 {
		return channels
	}
	switch RoleOf(name, role) {
	case "monitor", "narrator", "coordinator":
		return []string{"*"}
	default:
		return nil
	}
}

func (m *Member) EffectiveChannels(name string) []string {
	return EffectiveChannels(name, m.Role, m.Channels)
}

func (m *Member) HomeChannelIn(r *Roster) string {
	for _, ch := range m.Channels {
		if ch == "*" {
			return ""
		}
		if r != nil && r.Channels != nil {
			if c := r.Channels[ch]; c != nil && !c.Archived {
				return ch
			}
		}
	}
	return ""
}

// SeesIn determines if a member subscribed to channels can observe msg (unchanneled main board is visible to all).
func SeesIn(name string, msg Message, channels []string, active func(string) bool) bool {
	if msg.Channel == "" || msg.Channel == name {
		return true
	}
	for _, ch := range channels {
		if ch == "*" || ch == msg.Channel {
			return true
		}
	}
	return MentionsIn(msg.Text, name, msg.Channel, channels, active)
}

func (m *Member) Sees(name string, msg Message, active func(string) bool) bool {
	return SeesIn(name, msg, m.EffectiveChannels(name), active)
}

// ChannelsAt reconstructs historical channel subscriptions across control messages for one member.
func ChannelsAt(name string, current []string, msgs []Message) [][]string {
	cur := current
	for _, msg := range msgs {
		if msg.Control == nil {
			continue
		}
		if msg.Control.Op != "subscribe" && msg.Control.Op != "unsubscribe" {
			continue
		}
		if slices.Contains(msg.Control.Members, name) {
			cur = msg.Control.Prev[name]
			break
		}
	}
	out := make([][]string, len(msgs))
	for i, msg := range msgs {
		if msg.Control != nil && (msg.Control.Op == "subscribe" || msg.Control.Op == "unsubscribe") {
			if slices.Contains(msg.Control.Members, name) {
				cur = msg.Control.Next[name]
			}
		}
		out[i] = cur
	}
	return out
}

// ApplyControl applies channel create/archive/subscribe/unsubscribe control operations to a Roster.
func ApplyControl(r *Roster, ctl *Control) error {
	if r == nil || ctl == nil {
		return nil
	}
	if r.Channels == nil {
		r.Channels = make(map[string]*Channel)
	}
	switch ctl.Op {
	case "create":
		if err := ValidateChannelName(ctl.Channel); err != nil {
			return err
		}
		r.Channels[ctl.Channel] = &Channel{Name: ctl.Channel, Topic: ctl.Topic}
	case "archive":
		if ch := r.Channels[ctl.Channel]; ch != nil {
			ch.Archived = true
		}
	case "subscribe":
		for _, memberName := range ctl.Members {
			if m := r.Members[memberName]; m != nil {
				for _, ch := range ctl.Channels {
					if !slices.Contains(m.Channels, ch) {
						m.Channels = append(m.Channels, ch)
					}
				}
			}
		}
	case "unsubscribe":
		for _, memberName := range ctl.Members {
			if m := r.Members[memberName]; m != nil {
				var next []string
				for _, ch := range m.Channels {
					if !slices.Contains(ctl.Channels, ch) {
						next = append(next, ch)
					}
				}
				m.Channels = next
			}
		}
	default:
		return fmt.Errorf("unknown control op %q", ctl.Op)
	}
	return nil
}
