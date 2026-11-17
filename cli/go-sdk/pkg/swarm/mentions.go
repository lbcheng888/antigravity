package swarm

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Mentions reports whether text mentions @name, @all, or @<prefix> for numbered copies like worker-1.
func Mentions(text, name string) bool {
	low := strings.ToLower(text)
	target := strings.ToLower(name)
	if named(low, target) || named(low, "all") {
		return true
	}
	if prefix, ok := CopyPrefix(target); ok && named(low, prefix) {
		return true
	}
	return false
}

// CopyPrefix splits "foo-12" -> ("foo", true) for numbered shard workers.
func CopyPrefix(name string) (string, bool) {
	i := strings.LastIndexByte(name, '-')
	if i <= 0 || i+1 >= len(name) {
		return "", false
	}
	tail := name[i+1:]
	for j := 0; j < len(tail); j++ {
		if tail[j] < '0' || tail[j] > '9' {
			return "", false
		}
	}
	return name[:i], true
}

func named(text, name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(text); i++ {
		if text[i] != '@' {
			continue
		}
		if i > 0 {
			r, _ := utf8.DecodeLastRuneInString(text[:i])
			if isNameRune(r) {
				continue
			}
		}
		rest := text[i+1:]
		if !strings.HasPrefix(rest, name) {
			continue
		}
		after := rest[len(name):]
		if after == "" {
			return true
		}
		r, _ := utf8.DecodeRuneInString(after)
		if !isNameRune(r) {
			return true
		}
	}
	return false
}

func isNameRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_'
}

// ValidateChannelName enforces lowercase alphanumeric/dash/underscore channel names.
func ValidateChannelName(name string) error {
	if name == "" {
		return fmt.Errorf("channel name cannot be empty")
	}
	if name == "*" || name == "main" {
		return fmt.Errorf("reserved channel name %q", name)
	}
	for _, r := range name {
		if !isNameRune(r) {
			return fmt.Errorf("invalid rune %q in channel name %q", r, name)
		}
	}
	return nil
}

// ChannelMentions extracts all valid `@#channel` tokens from text in lowercase without duplicates.
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

// MentionsIn evaluates both direct @name/@all mentions and `@#channel` mentions against member channels.
func MentionsIn(text, name, msgChannel string, channels []string, active func(string) bool) bool {
	if Mentions(text, name) {
		return true
	}
	chs := ChannelMentions(text)
	if len(chs) == 0 {
		return false
	}
	all := slices.Contains(channels, "*")
	for _, ch := range chs {
		if active != nil && ch != "main" && !active(ch) {
			continue
		}
		if all || (ch == "main" && len(channels) > 0) || slices.Contains(channels, ch) {
			return true
		}
		if msgChannel != "" && ch == msgChannel && slices.Contains(channels, msgChannel) {
			return true
		}
	}
	return false
}

// CheckChannelMentions returns warnings for `@#channel` references targeting missing or archived channels.
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
