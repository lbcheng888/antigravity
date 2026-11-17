// Reconstructed from agy v1.5.10 binary evidence (disassembly + Go type table).
// Binary VA: 0x10232a000 | Size: 1 funcs (first func SupportsForm, 192 B) | Source: third_party/jetski/cli/acp/schema/elicitation.go:L16
// Reconstructed from binary: /Users/mac/.local/bin/agy (v1.5.10)
// Source: third_party/jetski/cli/acp/schema/elicitation.go

package schema

import (
	"bytes"
	"encoding/json/jsontext"
)

// Binary VA: 0x10a1b2218 | Size: 24 B
type ClientElicitationCapabilities struct {
	Form jsontext.Value `json:"form,omitempty"`
}

// Binary VA: 0x10232a000 | Size: 192 B | Source: third_party/jetski/cli/acp/schema/elicitation.go:L16
func (c *ClientElicitationCapabilities) SupportsForm() bool {
	if c == nil {
		return false
	}
	trimmed := bytes.TrimSpace(c.Form)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte("false")) {
		return false
	}
	return true
}

// Binary VA: 0x10a62d908 | Size: 112 B
type CreateElicitationParams struct {
	SessionID       string            `json:"sessionId"`
	ToolCallID      string            `json:"toolCallId,omitempty"`
	Mode            string            `json:"mode"`
	Message         string            `json:"message"`
	RequestedSchema ElicitationSchema `json:"requestedSchema"`
}

// Binary VA: 0x10a307c90 | Size: 24 B
type CreateElicitationResult struct {
	Action  string                    `json:"action"`
	Content map[string]jsontext.Value `json:"content,omitempty"`
}

// Binary VA: 0x10a307d20 | Size: 32 B
type ElicitationEnumOption struct {
	Const string `json:"const"`
	Title string `json:"title"`
}

// Binary VA: 0x10a1b2290 | Size: 24 B
type ElicitationMultiSelectItems struct {
	AnyOf []ElicitationEnumOption `json:"anyOf"`
}

// Binary VA: 0x10a62d9e0 | Size: 80 B
type ElicitationProperty struct {
	Type        string                       `json:"type"`
	Title       string                       `json:"title,omitempty"`
	Description string                       `json:"description,omitempty"`
	OneOf       []ElicitationEnumOption      `json:"oneOf,omitempty"`
	Items       *ElicitationMultiSelectItems `json:"items,omitempty"`
}

// Binary VA: 0x10a45c7d0 | Size: 48 B
type ElicitationSchema struct {
	Type       string                         `json:"type"`
	Properties map[string]ElicitationProperty `json:"properties"`
	Required   []string                       `json:"required,omitempty"`
}
