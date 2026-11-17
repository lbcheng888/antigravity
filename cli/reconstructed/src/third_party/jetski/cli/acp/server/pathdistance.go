// Binary VA: 0x102f87280 | Source: third_party/jetski/cli/acp/server/pathdistance.go
package server

import (
	"path/filepath"
	"strings"
)

// Binary VA: 0x102f87280 | Size: 192 B | Source: third_party/jetski/cli/acp/server/pathdistance.go:L10
func pathDistance(base, target string) int {
	if base == "" || target == "" {
		return 9999
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return 9999
	}
	if rel == "." {
		return 0
	}
	return len(strings.Split(rel, "/"))
}
