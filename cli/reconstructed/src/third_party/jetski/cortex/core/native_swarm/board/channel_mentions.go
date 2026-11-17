// Binary VA: 0x101fb2b70 | Size: 880 B | Source: third_party/jetski/cortex/core/native_swarm/board/channel_mentions.go:L19
package board

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Binary VA: 0x10a496da8 | Size: 24 B
type Lifecycle struct {
	r        *Roster
	created  map[string]uint64
	archived map[string]uint64
}

// Binary VA: 0x101fb2b70 | Size: 880 B | Source: third_party/jetski/cortex/core/native_swarm/board/channel_mentions.go:L19
func ChannelMentions(text string) []string {
	if !strings.Contains(text, "@#") {
		return nil
	}
	low := strings.ToLower(text)
	var out []string
	for i := 0; i < len(low); i++ {
		if low[i] != '@' || i+1 >= len(low) || low[i+1] != '#' {
			continue
		}
		if i > 0 {
			r, _ := utf8.DecodeLastRuneInString(low[:i])
			if isNameRune(r) {
				continue
			}
		}
		j := i + 2
		for j < len(low) {
			r, sz := utf8.DecodeRuneInString(low[j:])
			if !isNameRune(r) {
				break
			}
			j += sz
		}
		name := low[i+2 : j]
		if name == "main" || ValidateChannelName(name) == nil {
			if !slices.Contains(out, name) {
				out = append(out, name)
			}
		}
	}
	return out
}

// Binary VA: 0x101fb2ee0 | Size: 512 B | Source: third_party/jetski/cortex/core/native_swarm/board/channel_mentions.go:L57
func MentionsIn(text, name string, channels []string) bool {
	if Mentions(text, name) {
		return true
	}
	if len(channels) == 0 {
		return false
	}
	all := slices.Contains(channels, "*")
	for _, ch := range ChannelMentions(text) {
		if ch == "main" || all || slices.Contains(channels, ch) {
			return true
		}
	}
	return false
}

// Binary VA: 0x101fb30e0 | Size: 816 B | Source: third_party/jetski/cortex/core/native_swarm/board/channel_mentions.go:L81
func CheckChannelMentions(r *Roster, text string) []string {
	chs := ChannelMentions(text)
	if len(chs) == 0 {
		return nil
	}
	var warnings []string
	for _, ch := range chs {
		if ch == "main" {
			continue
		}
		var c *Channel
		if r != nil && r.Channels != nil {
			c = r.Channels[ch]
		}
		if c == nil {
			warnings = append(warnings, fmt.Sprintf("`@#%s` addressed nobody (unknown channel)", ch))
		} else if c.Archived {
			warnings = append(warnings, fmt.Sprintf("`@#%s` addressed nobody (archived channel)", ch))
		}
	}
	return warnings
}

// Note records channel create/archive control messages on Lifecycle.
// Inlined into ActiveAt at L118-L135.
func (l *Lifecycle) Note(msg Message) {
	if msg.Control == nil {
		return
	}
	switch msg.Control.Op {
	case "create":
		l.created[msg.Control.Channel] = msg.Seq
	case "archive":
		l.archived[msg.Control.Channel] = msg.Seq
	}
}

// Binary VA: 0x101fbc3a0 | Size: 272 B | Source: third_party/jetski/cortex/core/native_swarm/board/channel_mentions.go:L136
func (l *Lifecycle) ActiveAt(seq uint64) func(string) bool {
	return func(name string) bool {
		if l.r == nil || l.r.Channels == nil {
			return false
		}
		ch := l.r.Channels[name]
		if ch == nil {
			return false
		}
		if c, ok := l.created[name]; ok && seq < c { /* inferred: < vs <= */
			return false
		}
		if a, ok := l.archived[name]; ok && seq >= a { /* inferred */
			return false
		}
		return !ch.Archived
	}
}

// Binary VA: 0x101fb3410 | Size: 848 B | Source: third_party/jetski/cortex/core/native_swarm/board/channel_mentions.go:L155
func ActiveAt(r *Roster, msgs []Message) func(uint64) func(string) bool {
	l := &Lifecycle{
		r:        r,
		created:  make(map[string]uint64),
		archived: make(map[string]uint64),
	}
	for _, m := range msgs {
		l.Note(m)
	}
	return l.ActiveAt
}

// Binary VA: 0x101fb3760 | Size: 832 B | Source: third_party/jetski/cortex/core/native_swarm/board/channel_mentions.go:L174
func CalledOnIn(r *Roster, name string, msgs []Message, since uint64 /* inferred */) bool {
	var m *Member
	var ok bool
	if r != nil && r.Members != nil {
		m, ok = r.Members[name]
	}
	var chansAt func(uint64) []string /* inferred */
	var activeAt func(uint64) func(string) bool
	for _, msg := range msgs {
		if msg.Seq <= since || msg.Agent == name {
			continue
		}
		if Mentions(msg.Text, name) {
			return true
		}
		if ok && m != nil && strings.Contains(msg.Text, "@#") {
			if chansAt == nil {
				chansAt = ChannelsAt(r, name, msgs) /* inferred */
				activeAt = ActiveAt(r, msgs)
			}
			_ = activeAt
			eff := EffectiveChannels(m.Role, chansAt(msg.Seq) /* inferred */)
			if MentionsIn(msg.Text, name, eff) {
				return true
			}
		}
	}
	return false
}
