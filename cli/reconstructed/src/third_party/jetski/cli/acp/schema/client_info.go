// Reconstructed from agy v1.5.10 binary evidence (disassembly + Go type table).
// Binary VA: 0x102329ce0 | Size: 4 funcs (first func parseClientInfo, 368 B) | Source: third_party/jetski/cli/acp/schema/client_info.go:L24
// Reconstructed from binary: /Users/mac/.local/bin/agy (v1.5.10)
// Source: third_party/jetski/cli/acp/schema/client_info.go

package schema

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"regexp"
	"strings"
)

// Binary VA: 0x10a4f5c40 | Size: 32 B
type ClientInfo struct {
	Name    string
	Version string
}

var surfaceSegmentSanitizer *regexp.Regexp /* inferred: BSS 0x10d4e5328 / regexp replace */

// Binary VA: 0x102329ce0 | Size: 368 B | Source: third_party/jetski/cli/acp/schema/client_info.go:L24
func parseClientInfo(raw jsontext.Value) ClientInfo {
	if len(raw) == 0 {
		return ClientInfo{}
	}
	var m map[string]any /* inferred */
	if err := jsonv2.Unmarshal(raw, &m); err != nil || len(m) == 0 {
		return ClientInfo{}
	}
	name, _ := m["name"].(string)
	version, _ := m["version"].(string)
	return ClientInfo{Name: name, Version: version}
}

// Binary VA: 0x102329e50 | Size: 176 B | Source: third_party/jetski/cli/acp/schema/client_info.go:L37
func sanitizeSurfaceSegment(s string) string {
	s = strings.TrimSpace(s) /* inferred */
	if s == "" {
		return "unknown"
	}
	s = strings.ToLower(s)
	if surfaceSegmentSanitizer != nil { /* inferred */
		s = surfaceSegmentSanitizer.ReplaceAllString(s, "-")
	}
	s = strings.Trim(s, "-") /* inferred */
	if s == "" {
		return "unknown"
	}
	return s
}

// Binary VA: 0x102329f00 | Size: 144 B | Source: third_party/jetski/cli/acp/schema/client_info.go:L53
func (c ClientInfo) SurfaceSegments() (string, string) {
	return sanitizeSurfaceSegment(c.Name), sanitizeSurfaceSegment(c.Version)
}

// Binary VA: 0x102329f90 | Size: 112 B | Source: third_party/jetski/cli/acp/schema/client_info.go:L59
func (c ClientInfo) HostPath() string {
	name, version := c.SurfaceSegments()
	return name + "/" + version
}
