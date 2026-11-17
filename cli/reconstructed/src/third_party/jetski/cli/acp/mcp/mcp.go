// Reconstructed from agy v1.5.10 binary evidence (disassembly + Go type table).
// Binary VA: 0x102c516b0 | Size: 7 funcs (first func Assign, 992 B) | Source: third_party/jetski/cli/acp/mcp/mcp.go:L34
// Reconstructed from binary: /Users/mac/.local/bin/agy (v1.5.10)
// Source: third_party/jetski/cli/acp/mcp/mcp.go

package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"sync"

	"google3/third_party/golang/protobuf/v2/proto/proto"
	"google3/third_party/jetski/cli/acp/schema/schema"
	"google3/third_party/jetski/cortex_pb/cortex_go_proto"
)

// Binary VA: 0x10a327e20 | Size: 16 B
type Names struct {
	mu           sync.Mutex
	fingerprints map[string]string
}

// Binary VA: 0x102c516b0 | Size: 992 B | Source: third_party/jetski/cli/acp/mcp/mcp.go:L34
func (n *Names) Assign(spec *cortex_go_proto.McpServerSpec) *cortex_go_proto.McpServerSpec {
	if spec == nil {
		return nil
	}
	fp := Fingerprint(spec)
	base := sanitizeMCPServerName(spec.GetName()) /* inferred */
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.fingerprints == nil {
		n.fingerprints = make(map[string]string)
	}
	candidate := base
	for i := 2; ; i++ { /* inferred */
		if prev, exists := n.fingerprints[candidate]; !exists || prev == fp {
			n.fingerprints[candidate] = fp
			out := proto.Clone(spec).(*cortex_go_proto.McpServerSpec)
			out.SetName(candidate) /* inferred */
			return out
		}
		candidate = base + "_" + strconv.Itoa(i) /* inferred */
	}
}

// Binary VA: 0x102c52260 | Size: 64 B | Source: third_party/jetski/cli/acp/mcp/mcp.go:L68
func sanitizeMCPServerName(name string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, name)
}

// Binary VA: 0x102c51a90 | Size: 368 B | Source: third_party/jetski/cli/acp/mcp/mcp.go:L80
func Fingerprint(spec *cortex_go_proto.McpServerSpec) string {
	cloned := proto.Clone(spec).(*cortex_go_proto.McpServerSpec)
	// [unrecovered] clears name or non-structural fields before deterministic marshal
	b, _ := proto.MarshalOptions{Deterministic: true}.Marshal(cloned)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Binary VA: 0x102c51c00 | Size: 336 B | Source: third_party/jetski/cli/acp/mcp/mcp.go:L94
func IDEPortSpec(port int) *cortex_go_proto.McpServerSpec {
	if port <= 0 {
		return nil
	}
	portStr := strconv.FormatInt(int64(port), 10)
	return cortex_go_proto.McpServerSpec_builder{
		Name:    proto.String("ide-tools"), /* inferred: builder field */
		Command: proto.String("nc"),        /* inferred: builder field */
		Args:    []string{"127.0.0.1", portStr},
	}.Build()
}

// Binary VA: 0x102c51d50 | Size: 512 B | Source: third_party/jetski/cli/acp/mcp/mcp.go:L109
func FromConfig(cfg schema.MCPServerConfig) *cortex_go_proto.McpServerSpec {
	// [unrecovered] switches on cfg.Type / cfg.URL / cfg.Command
	env := parseKeyValuePairs(cfg.Env)
	headers := parseKeyValuePairs(cfg.Headers)
	_ = env
	_ = headers
	return cortex_go_proto.McpServerSpec_builder{
		Name:    proto.String(cfg.Name),
		Command: proto.String(cfg.Command),
		Args:    cfg.Args,
		Env:     env,
		Url:     proto.String(cfg.URL),
		Headers: headers,
	}.Build()
}

// Binary VA: 0x102c51f50 | Size: 672 B | Source: third_party/jetski/cli/acp/mcp/mcp.go:L132
func parseKeyValuePairs(raw any) map[string]string {
	out := make(map[string]string)
	switch v := raw.(type) {
	case map[string]any:
		for k, val := range v {
			if s, ok := val.(string); ok {
				out[k] = s
			}
		}
	case []any:
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name, _ := m["name"].(string)
			val, _ := m["value"].(string)
			if name != "" { /* inferred */
				out[name] = val
			}
		}
	}
	return out
}
