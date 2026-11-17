// Binary VA: 0x101fb7f40 | Size: 240 B | Source: third_party/jetski/cortex/core/native_swarm/board/mentions.go:L20
package board

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Binary VA: 0x101fb7f40 | Size: 240 B | Source: third_party/jetski/cortex/core/native_swarm/board/mentions.go:L20
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

// Binary VA: 0x101fb8030 | Size: 352 B | Source: third_party/jetski/cortex/core/native_swarm/board/mentions.go:L35
func CopyPrefix(name string) (string, bool) {
	i := strings.LastIndexByte(name, '-')
	if i <= 0 || i+1 >= len(name) {
		return "", false
	}
	tail := name[i+1:]
	n := 0 /* inferred */
	for j := 0; j < len(tail); j++ {
		c := tail[j]
		if c < '0' || c > '9' {
			return "", false
		}
		n = n*10 + int(c-'0') /* inferred */
	}
	_ = n
	return name[:i], true
}

// Binary VA: 0x101fb8190 | Size: 400 B | Source: third_party/jetski/cortex/core/native_swarm/board/mentions.go:L56
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

// Binary VA: 0x101fb8320 | Size: 240 B | Source: third_party/jetski/cortex/core/native_swarm/board/mentions.go:L93
func isNameRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_'
}
