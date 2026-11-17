// Binary VA: 0x102f7b230 | Size: 208 B | Source: third_party/jetski/cli/acp/stepview/relpath.go

package stepview

import (
	"path/filepath"
	"strings"
)

// Binary VA: 0x102f7b230 | Size: 208 B | Source: third_party/jetski/cli/acp/stepview/relpath.go:L11
func relPath(cwd /* inferred */, p /* inferred */ string, dotAsSlash /* inferred */ bool) string {
	if p == "" {
		return ""
	}
	rel, err := filepath.Rel(cwd, p)
	if err != nil {
		rel = p
	}
	if dotAsSlash && rel != "." && !strings.HasSuffix(rel, "/") {
		rel = rel + "/"
	}
	return rel
}
