// Reconstructed from agy v1.5.10 binary evidence (disassembly + Go type table).
// Binary VA: 0x102332240 | Size: 2 funcs (first func ClientSupportFrom, 448 B) | Source: third_party/jetski/cli/acp/auth/clientsupport.go:L24
// Reconstructed from agy v1.5.10 (arm64 Mach-O)
// Package auth: third_party/jetski/cli/acp/auth/clientsupport.go

package auth

import "google3/third_party/jetski/cli/acp/schema/schema"

// ClientSupport holds client auth capabilities extracted from InitializeParams. /* inferred */
type ClientSupport struct {
	Gateway  bool
	Terminal bool
}

// Binary VA: 0x102332240 | Size: 448 B | Source: third_party/jetski/cli/acp/auth/clientsupport.go:L24
func ClientSupportFrom(params schema.InitializeParams) ClientSupport {
	caps := params.Capabilities() /* inferred: inlined protocol.go:54 */
	if caps == nil || caps.Auth == nil {
		return ClientSupport{}
	}
	var gateway bool /* inferred: inlined methods.go:184-197 */
	if caps.Auth.Meta != nil {
		if v, ok := caps.Auth.Meta["gateway"]; ok && v != nil {
			switch val := v.(type) {
			case bool:
				gateway = val
			case map[string]interface{}:
				gateway = len(val) > 0
			case string:
				gateway = len(val) > 0
			}
		}
	}
	return ClientSupport{
		Gateway:  gateway,
		Terminal: caps.Auth.Terminal,
	}
}

// Binary VA: 0x102332400 | Size: 208 B | Source: third_party/jetski/cli/acp/auth/clientsupport.go:L37
func AdvertisedMethods(isJetski bool, support ClientSupport, adminSettingsPath string, disableDefaultAdminSettings bool) []schema.AuthMethod {
	gatewayAuthEnabled := IsGatewayAuthEnabled()
	adminDisposition := resolveAdminGatewayDisposition(adminSettingsPath, disableDefaultAdminSettings)
	return advertisedMethods(advertisedMethodsOptions{ /* inferred */
		IsJetski:               isJetski,
		GatewayAuthEnabled:     gatewayAuthEnabled,
		ClientSupportsGateway:  support.Gateway,
		AdminGateway:           adminDisposition,
		ClientSupportsTerminal: support.Terminal,
	})
}
