// Binary source reconstruction of agy v1.5.10
// Source: third_party/jetski/cortex/core/native_swarm/board/channels.go
// Package: board (google3/third_party/jetski/cortex/core/native_swarm/board/board)

package board

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var roleSuffixRE *regexp.Regexp /* inferred: 0x10d4e9938 */

// Binary VA: 0x101fb3aa0 | Size: 208 B | Source: third_party/jetski/cortex/core/native_swarm/board/channels.go:L115
func RoleOf(name, role string) string {
	if role != "" && role != "worker" {
		return role
	}
	if m := roleSuffixRE.FindStringSubmatch(name); len(m) > 1 {
		return m[1]
	}
	return name
}

// Binary VA: 0x101fb3b70 | Size: 352 B | Source: third_party/jetski/cortex/core/native_swarm/board/channels.go:L139
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

// Binary VA: 0x101fb3cd0 | Size: 96 B | Source: third_party/jetski/cortex/core/native_swarm/board/channels.go:L148
func (m *Member) EffectiveChannels(name string) []string {
	return EffectiveChannels(name, m.Role, m.Channels)
}

// Binary VA: 0x101fb3d30 | Size: 208 B | Source: third_party/jetski/cortex/core/native_swarm/board/channels.go:L158
func (m *Member) HomeChannelIn(r *Roster) string {
	for _, ch := range m.Channels {
		if ch == "*" {
			return ""
		}
		if validateActiveChannel(r, ch) == nil {
			return ch
		}
	}
	return ""
}

// Binary VA: 0x101fb3e00 | Size: 352 B | Source: third_party/jetski/cortex/core/native_swarm/board/channels.go:L175
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

// Binary VA: 0x101fb3f60 | Size: 176 B | Source: third_party/jetski/cortex/core/native_swarm/board/channels.go:L191
func (m *Member) Sees(name string, msg Message, active func(string) bool) bool {
	chans := EffectiveChannels(name, m.Role, m.Channels)
	return SeesIn(name, msg, chans, active)
}

// Binary VA: 0x101fb4010 | Size: 1056 B | Source: third_party/jetski/cortex/core/native_swarm/board/channels.go:L215
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

// Binary VA: 0x101fb4430 | Size: 1296 B | Source: third_party/jetski/cortex/core/native_swarm/board/channels.go:L235
func (v View) Visible(r *Roster, msgs []Message) ([]bool, error) {
	vis := make([]bool, len(msgs))
	if v.All {
		for i := range vis {
			vis[i] = true
		}
		return vis, nil
	}
	if len(v.Channels) != 0 {
		set := make(map[string]bool, len(v.Channels))
		for _, ch := range v.Channels {
			if ch == "main" {
				ch = ""
			}
			set[ch] = true
		}
		for i, msg := range msgs {
			vis[i] = set[msg.Channel]
		}
		return vis, nil
	}
	if v.Member != "" {
		var m *Member
		var ok bool
		if r != nil {
			m, ok = r.Members[v.Member]
		}
		if !ok || m == nil {
			return nil, fmt.Errorf("%w: %q", ErrNotMember, v.Member)
		}
		chansAt := ChannelsAt(v.Member, m.Channels, msgs)
		activeAt := ActiveAt(r, msgs)
		for i, msg := range msgs {
			chans := EffectiveChannels(v.Member, m.Role, chansAt[i])
			vis[i] = SeesIn(v.Member, msg, chans, activeAt[i])
		}
		return vis, nil
	}
	return nil, fmt.Errorf("%w: empty view", ErrInvalid)
}

// Binary VA: 0x101fb4940 | Size: 384 B | Source: third_party/jetski/cortex/core/native_swarm/board/channels.go:L269
func (v View) Validate(r *Roster) error {
	if _, err := v.SelectsMain(); err != nil {
		return err
	}
	if !v.All && len(v.Channels) == 0 && v.Member != "" {
		var m *Member
		var ok bool
		if r != nil {
			m, ok = r.Members[v.Member]
		}
		if !ok || m == nil {
			return fmt.Errorf("%w: %q", ErrNotMember, v.Member)
		}
	}
	return nil
}

// Binary VA: 0x101fb4ac0 | Size: 384 B | Source: third_party/jetski/cortex/core/native_swarm/board/channels.go:L286
func (v View) SelectsMain() (bool, error) {
	if v.All || v.Member != "" {
		return true, nil
	}
	if len(v.Channels) == 0 {
		return false, fmt.Errorf("%w: empty view", ErrInvalid)
	}
	for _, ch := range v.Channels {
		if ch == "main" || ch == "" {
			return true, nil
		}
	}
	return false, nil
}

// Binary VA: 0x101fb4c40 | Size: 640 B | Source: third_party/jetski/cortex/core/native_swarm/board/channels.go:L304
func (v View) Since(r *Roster, msgs []Message, since uint64, limit int) ([]Message, error) {
	start := 0
	for start < len(msgs) && msgs[start].Seq <= since {
		start++
	}
	sub := msgs[start:]
	vis, err := v.Visible(r, sub)
	if err != nil {
		return nil, err
	}
	var out []Message
	for i, msg := range sub {
		if !vis[i] {
			continue
		}
		if limit > 0 && len(out) == limit {
			break
		}
		out = append(out, msg)
	}
	return out, nil
}

// Binary VA: 0x101fb4ec0 | Size: 800 B | Source: third_party/jetski/cortex/core/native_swarm/board/channels.go:L329
func (v View) Latest(r *Roster, msgs []Message, limit int) ([]Message, error) {
	vis, err := v.Visible(r, msgs)
	if err != nil {
		return nil, err
	}
	var out []Message
	for i := len(msgs) - 1; i >= 0; i-- {
		if limit > 0 && len(out) == limit {
			break
		}
		if !vis[i] {
			continue
		}
		out = append(out, msgs[i])
	}
	slices.Reverse(out)
	return out, nil
}

// Binary VA: 0x101fb51e0 | Size: 416 B | Source: third_party/jetski/cortex/core/native_swarm/board/channels.go:L349
func (v View) AnyUnread(r *Roster, msgs []Message, since, head uint64) (bool, error) {
	start := 0
	for start < len(msgs) && msgs[start].Seq <= since {
		start++
	}
	sub := msgs[start:]
	vis, err := v.Visible(r, sub)
	if err != nil {
		return false, err
	}
	for i, msg := range sub {
		if msg.Seq > head {
			break
		}
		if vis[i] && v.Unread(msg) {
			return true, nil
		}
	}
	return false, nil
}

// Binary VA: 0x101fb5380 | Size: 32 B | Source: third_party/jetski/cortex/core/native_swarm/board/channels.go:L374
func (v View) Unread(msg Message) bool {
	return msg.RequestID == ""
}

// Binary VA: 0x101fb53a0 | Size: 1264 B | Source: third_party/jetski/cortex/core/native_swarm/board/channels.go:L380
func ApplyControl(r *Roster, msg Message) {
	ctrl := msg.Control
	if ctrl == nil {
		return
	}
	switch ctrl.Op {
	case "create":
		if r.Channels == nil {
			r.Channels = make(map[string]*Channel)
		}
		if _, exists := r.Channels[ctrl.Channel]; exists {
			return
		}
		r.Channels[ctrl.Channel] = &Channel{
			Name:      ctrl.Channel,
			Topic:     ctrl.Topic,
			CreatedBy: msg.Agent,
		}
	case "archive":
		if r.Channels == nil {
			r.Channels = make(map[string]*Channel)
		}
		ch, exists := r.Channels[ctrl.Channel]
		if !exists {
			ch = &Channel{Name: ctrl.Channel}
			r.Channels[ctrl.Channel] = ch
		}
		ch.Archived = true
	case "subscribe", "unsubscribe":
		for _, name := range ctrl.Members {
			if r == nil {
				continue
			}
			m, ok := r.Members[name]
			if !ok || m == nil {
				continue
			}
			m.Channels = slices.Clone(ctrl.Next[name])
		}
	}
}

// Binary VA: 0x101fb5890 | Size: 576 B | Source: third_party/jetski/cortex/core/native_swarm/board/channels.go:L451
func SubscribeOp(subscribe, unsubscribe []string) (op string, channels []string, err error) {
	if len(subscribe) > 0 && len(unsubscribe) == 0 {
		return "subscribe", subscribe, nil
	}
	if len(unsubscribe) > 0 && len(subscribe) == 0 {
		return "unsubscribe", unsubscribe, nil
	}
	return "", nil, fmt.Errorf("%w: subscribe to or unsubscribe from channels, one at a time", ErrInvalid)
}

// Binary VA: 0x101fb5ad0 | Size: 5920 B | Source: third_party/jetski/cortex/core/native_swarm/board/channels.go:L467
func PrepareChannelOp(r *Roster, op, actor, channel, topic, target string, channels []string) (Message, error) {
	if err := ValidateChannelOp(r, op, actor, channel, topic, target, channels); err != nil {
		return Message{}, err
	}
	switch op {
	case "create":
		text := fmt.Sprintf("[channel] %s created #%s", actor, channel)
		if topic != "" {
			text = text + ": " + topic
		}
		return Message{
			Agent:   actor,
			Text:    text,
			Channel: channel,
			Control: &Control{
				Op:      "create",
				Channel: channel,
				Topic:   topic,
			},
		}, nil
	case "archive":
		if ch := r.Channels[channel]; ch.Archived {
			return Message{}, nil
		}
		text := fmt.Sprintf("[channel] %s archived #%s", actor, channel)
		return Message{
			Agent:   actor,
			Text:    text,
			Channel: channel,
			Control: &Control{
				Op:      "archive",
				Channel: channel,
			},
		}, nil
	default:
		if target == "" {
			target = actor
		}
		var m *Member
		if r != nil {
			m = r.Members[target]
		}
		var req []string
		for _, ch := range channels {
			if ch == "main" {
				ch = ""
			}
			if ch != "" && !slices.Contains(req, ch) {
				req = append(req, ch)
			}
		}
		if len(req) == 0 {
			return Message{}, nil
		}
		cur := EffectiveChannels(target, m.Role, m.Channels)
		hasStar := slices.Contains(cur, "*")
		var next []string
		var verb string
		if op == "subscribe" {
			if hasStar {
				return Message{}, nil
			}
			if slices.Contains(req, "*") {
				next = []string{"*"}
			} else {
				next = slices.Clone(cur)
				for _, ch := range req {
					if !slices.Contains(next, ch) {
						next = append(next, ch)
					}
				}
			}
			verb = "subscribed to"
		} else {
			if hasStar && !slices.Contains(req, "*") {
				return Message{}, fmt.Errorf("%w: %q is subscribed to every channel (*); unsubscribe it from * first", ErrInvalid, target)
			}
			for _, ch := range cur {
				if !slices.Contains(req, ch) {
					next = append(next, ch)
				}
			}
			verb = "unsubscribed from"
		}
		if slices.Equal(cur, next) {
			return Message{}, nil
		}
		labels := make([]string, len(req))
		for i, ch := range req {
			if ch == "*" {
				labels[i] = "every channel"
			} else {
				labels[i] = "#" + ch
			}
		}
		text := fmt.Sprintf("[channel] %s %s %s", target, verb, strings.Join(labels, ", "))
		if target != actor {
			text = text + " (by " + actor + ")"
		}
		var prevSlice []string
		if len(m.Channels) > 0 {
			prevSlice = slices.Clone(m.Channels)
		}
		prev := map[string][]string{target: prevSlice}
		nextMap := map[string][]string{target: next}
		primary := req[0]
		if primary == "*" {
			primary = ""
		}
		return Message{
			Agent:   actor,
			Text:    text,
			Channel: primary,
			Control: &Control{
				Op:       op,
				Channel:  primary,
				Channels: req,
				Members:  []string{target},
				Prev:     prev,
				Next:     nextMap,
			},
		}, nil
	}
}

// Binary VA: 0x101fb71f0 | Size: 736 B | Source: third_party/jetski/cortex/core/native_swarm/board/channels.go:L562
func InheritChannels(r *Roster, addedBy string, explicit []string) []string {
	if len(explicit) == 0 {
		if r == nil {
			return nil
		}
		m, ok := r.Members[addedBy]
		if !ok || m == nil || m.Role != "worker" || len(m.Channels) == 0 {
			return nil
		}
		return slices.Clone(m.Channels)
	}
	var out []string
	for _, ch := range explicit {
		if ch == "main" {
			ch = ""
		}
		if ch != "" && !slices.Contains(out, ch) {
			out = append(out, ch)
		}
	}
	return out
}

// Binary VA: 0x101fb74d0 | Size: 416 B | Source: third_party/jetski/cortex/core/native_swarm/board/channels.go:L575
func BriefChannel(r *Roster, addedBy string, explicit []string, requested string) string {
	if requested == "main" {
		requested = ""
	}
	if requested != "" {
		return requested
	}
	if addedBy == "" {
		return ""
	}
	chans := InheritChannels(r, addedBy, explicit)
	m := &Member{Channels: chans}
	return m.HomeChannelIn(r)
}
