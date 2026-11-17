// Reconstructed from agy v1.5.10 binary evidence (disassembly + Go type table).
// Binary VA: 0x102329850 | Size: 1 funcs (first func UnmarshalJSON, 1168 B) | Source: third_party/jetski/cli/acp/schema/auth.go:L34
// Reconstructed from binary: /Users/mac/.local/bin/agy (v1.5.10)
// Source: third_party/jetski/cli/acp/schema/auth.go

package schema

import "encoding/json/jsontext"
import jsonv2 "encoding/json/v2"

// Binary VA: 0x10a720980 | Size: 96 B
type AuthMethod struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Meta        map[string]any `json:"_meta,omitempty"`
	Type        string         `json:"type,omitempty"`
	Args        []string       `json:"args,omitempty"`
}

// Binary VA: 0x10a307ae0 | Size: 24 B
type AuthenticateParams struct {
	MethodID string         `json:"methodId"`
	Meta     map[string]any `json:"_meta,omitempty"`
}

// Binary VA: 0x102329850 | Size: 1168 B | Source: third_party/jetski/cli/acp/schema/auth.go:L34
func (p *AuthenticateParams) UnmarshalJSON(data []byte) error {
	var raw map[string]jsontext.Value /* inferred */
	if err := jsonv2.Unmarshal(data, &raw); err != nil {
		return err
	}
	var out AuthenticateParams /* inferred */
	if v, ok := raw["methodId"]; ok {
		if err := jsonv2.Unmarshal(v, &out.MethodID); err != nil {
			return err
		}
	} else if v, ok := raw["method_id"]; ok {
		if err := jsonv2.Unmarshal(v, &out.MethodID); err != nil {
			return err
		}
	}
	if v, ok := raw["_meta"]; ok && len(v) > 0 && string(v) != "null" {
		if err := jsonv2.Unmarshal(v, &out.Meta); err != nil {
			return err
		}
	}
	for k, v := range raw {
		if k == "methodId" || k == "method_id" || k == "_meta" { /* inferred */
			continue
		}
		var val any /* inferred */
		if err := jsonv2.Unmarshal(v, &val); err != nil {
			return err
		}
		if out.Meta == nil {
			out.Meta = make(map[string]any)
		}
		if _, exists := out.Meta[k]; !exists {
			out.Meta[k] = val
		}
	}
	*p = out
	return nil
}

// Binary VA: 0x10a1b21a0 | Size: 8 B
type AuthenticateResult struct {
	Meta map[string]any `json:"_meta,omitempty"`
}
