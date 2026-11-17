// Binary VA: 0x102f8bc20 | Source: third_party/jetski/cli/acp/server/protocol.go
package server

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"strings"

	"google3/base/go/log"
)

// Binary VA: 0x102f8bc20 | Size: 736 B | Source: third_party/jetski/cli/acp/server/protocol.go:L14
func extractAntigravitySystemPrompt(ctx context.Context /* inferred */, raw jsontext.Value) string {
	if len(raw) == 0 {
		return ""
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return ""
	}
	spRaw, ok := meta["systemPrompt"]
	if !ok {
		return ""
	}
	spMap, ok := spRaw.(map[string]interface{})
	if !ok {
		log.WarningContextf(ctx, "Ignoring unsupported systemPrompt format in _meta.antigravity (expected object, got %T)", spRaw) /* inferred */
		return ""
	}
	appendRaw, ok := spMap["append"]
	if !ok {
		log.WarningContextf(ctx, "Ignoring invalid systemPrompt in _meta.antigravity: missing 'append' key") /* inferred */
		return ""
	}
	appendStr, ok := appendRaw.(string)
	if !ok {
		log.WarningContextf(ctx, "Ignoring invalid systemPrompt in _meta.antigravity: 'append' key must be a string, got %T", appendRaw) /* inferred */
		return ""
	}
	return strings.TrimSpace(appendStr)
}
