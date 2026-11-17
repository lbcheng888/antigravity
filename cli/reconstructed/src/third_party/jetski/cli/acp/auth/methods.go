// Reconstructed from agy v1.5.10 binary evidence (disassembly + Go type table).
// Binary VA: 0x1023320d0 | Size: 11 funcs (first func 0, 368 B) | Source: third_party/jetski/cli/acp/auth/methods.go:L57
// Reconstructed from agy v1.5.10 (arm64 Mach-O)
// Package auth: third_party/jetski/cli/acp/auth/methods.go

package auth

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"google3/google/internal/cloud/code/v1internal/v1internal_go_proto"
	"google3/third_party/jetski/cli/acp/schema/schema"
	"google3/third_party/jetski/language_server/code_assist_client/codeassistclient"
)

// knownAuthMethods is initialized by map.init.0 at L57. /* inferred */
var knownAuthMethods map[string]bool

// Binary VA: 0x1023320d0 | Size: 368 B | Source: third_party/jetski/cli/acp/auth/methods.go:L57
func init() {
	knownAuthMethods = map[string]bool{
		"oauth-personal": true,
		"oauth-business": true,
		"gemini-api-key": true,
		"agent-platform": true,
		"vertex-ai":      true,
		"gateway":        true,
		"terminal-login": true,
	}
}

// Error represents an ACP authentication error with JSON-RPC error code.
// Binary Type VA: 0x10a45c098
type Error struct {
	Code    int
	Message string
	Data    interface{}
}

// Binary VA: 0x10233d680 | Size: 32 B | Source: third_party/jetski/cli/acp/auth/methods.go:L88
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// Binary VA: 0x10233d6a0 | Size: 480 B | Source: third_party/jetski/cli/acp/auth/methods.go:L110
func toError(err error) *Error {
	if err == nil {
		return nil
	}
	var authErr *Error
	if errors.As(err, &authErr) && authErr != nil {
		return authErr
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{
			Code:    -32603, // -0x7f5b
			Message: "Sign-in timed out. Try again.",
		}
	}
	if errors.Is(err, context.Canceled) {
		return &Error{
			Code:    -32800, // -0x8020
			Message: "Sign-in was cancelled.",
		}
	}
	return &Error{
		Code:    -32603, // -0x7f5b
		Message: fmt.Sprintf("Couldn't sign in: %v. %s", err, "Sign in once with the Antigravity CLI on this machine (agy), or configure GEMINI_API_KEY or Application Default Credentials, then try again."),
	}
}

// Binary VA: 0x10233d880 | Size: 528 B | Source: third_party/jetski/cli/acp/auth/methods.go:L134
func ToClientError(err error, isJetski bool, supportsTerminal bool) *Error {
	var browserErr *BrowserUnavailableError
	if !isJetski && errors.As(err, &browserErr) {
		var hint string /* inferred */
		if browserErr.AuthMethod == "gcp" {
			hint = "Use a Gemini API key or Gemini Enterprise Agent Platform instead."
		} else if supportsTerminal {
			hint = fmt.Sprintf("Choose %q instead, or use a Gemini API key.", "Log in from the terminal")
		} else {
			hint = fmt.Sprintf("Run `agy acp %s` in a terminal on this machine, then retry; or use a Gemini API key.", "--login")
		}
		return &Error{
			Code:    -32603, // -0x7f5b
			Message: fmt.Sprintf("No browser is available here (%s). %s", browserErr.Reason, hint),
		}
	}
	return toError(err)
}

// normalizeAuthMethod was inlined at methods.go:162-164 in config_resolution.go. /* inferred */
func normalizeAuthMethod(id string) string {
	if id == "vertex-ai" {
		return "agent-platform"
	}
	if id == "terminal-login" {
		return "oauth-personal"
	}
	return id
}

// Binary VA: 0x10233da90 | Size: 224 B | Source: third_party/jetski/cli/acp/auth/methods.go:L172
func IsGatewayAuthEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("AGY_ACP_ENABLE_GATEWAY_AUTH")))
	switch v {
	case "1", "on", "yes", "true":
		return true
	}
	return false
}

// advertisedMethodsOptions holds inputs to advertisedMethods. /* inferred */
type advertisedMethodsOptions struct {
	IsJetski               bool
	GatewayAuthEnabled     bool
	ClientSupportsGateway  bool
	AdminGateway           adminGatewayDisposition
	ClientSupportsTerminal bool
}

// Binary VA: 0x10233db70 | Size: 1248 B | Source: third_party/jetski/cli/acp/auth/methods.go:L238
func advertisedMethods(opts advertisedMethodsOptions) []schema.AuthMethod {
	personal := schema.AuthMethod{
		ID:          "oauth-personal",
		Name:        "Log in with Google",
		Description: "Log in with your Google account",
	}
	if opts.IsJetski {
		return []schema.AuthMethod{personal}
	}
	if opts.AdminGateway == adminGatewayRequired { /* 1 */
		return nil
	}
	methods := []schema.AuthMethod{
		personal,
		{
			ID:          "oauth-business",
			Name:        "Log in with Gemini Enterprise",
			Description: "Log in with your Gemini Enterprise account",
		},
		{
			ID:          "gemini-api-key",
			Name:        "Gemini API key",
			Description: "Use an API key with Gemini Developer API",
		},
		{
			ID:          "agent-platform",
			Name:        "Gemini Enterprise Agent Platform",
			Description: "Use Gemini Enterprise Agent Platform (formerly Vertex AI) with Application Default Credentials",
		},
	}
	if opts.ClientSupportsTerminal {
		methods = append(methods, schema.AuthMethod{
			ID:          "terminal-login",
			Name:        "Log in from the terminal",
			Description: "Log in with your Google account by pasting a code in a terminal; works over SSH",
			Type:        "terminal",
			Args:        []string{"--login"},
		})
	}
	if opts.GatewayAuthEnabled && opts.ClientSupportsGateway && opts.AdminGateway != adminGatewayDisabled { /* 2 */
		methods = append(methods, schema.AuthMethod{
			ID:          "gateway",
			Name:        "AI API Gateway",
			Description: "Use a custom AI API Gateway",
			Meta: map[string]interface{}{
				"gateway": map[string]interface{}{
					"protocol":        "google",
					"restartRequired": "false",
				},
			},
		})
	}
	return methods
}

// Binary VA: 0x10233e050 | Size: 400 B | Source: third_party/jetski/cli/acp/auth/methods.go:L311
func ExtractGeminiAPIKey(meta map[string]interface{}) string {
	topKeys := []string{"api-key", "apiKey", "api_key"}
	for _, k := range topKeys {
		v, ok := meta[k]
		if !ok || v == nil {
			continue
		}
		switch val := v.(type) {
		case map[string]interface{}:
			nestedKeys := []string{"apiKey", "api_key", "api-key", "key"}
			for _, nk := range nestedKeys {
				if s, ok := val[nk].(string); ok && s != "" {
					return s
				}
			}
		case string:
			if val != "" {
				return val
			}
		}
	}
	return os.Getenv("GEMINI_API_KEY")
}

// Binary VA: 0x10233e1e0 | Size: 880 B | Source: third_party/jetski/cli/acp/auth/methods.go:L355
func redactAuthMeta(meta map[string]interface{}) map[string]interface{} {
	if meta == nil {
		return nil
	}
	out := make(map[string]interface{}, len(meta))
	for k, v := range meta {
		out[k] = v
	}
	secretKeys := []string{"api-key", "apiKey", "api_key"}
	for _, k := range secretKeys {
		if _, ok := out[k]; ok {
			out[k] = "<redacted>"
		}
	}
	if gw, ok := out["gateway"].(map[string]interface{}); ok {
		if _, hasHeaders := gw["headers"]; hasHeaders {
			gwCopy := make(map[string]interface{}, len(gw))
			for k, v := range gw {
				gwCopy[k] = v
			}
			gwCopy["headers"] = "<redacted>"
			out["gateway"] = gwCopy
		}
	}
	return out
}

// TierInfo holds subscription tier metadata for the authenticated account.
// Binary Type VA: 0x10a5e9d40
type TierInfo struct {
	Name       string `json:"name"`
	ID         string `json:"id"`
	UpgradeURI string `json:"upgradeUri"`
}

// Binary VA: 0x10233e550 | Size: 48 B | Source: third_party/jetski/cli/acp/auth/methods.go:L389
func (t TierInfo) IsEmpty() bool {
	return t.Name == "" && t.ID == ""
}

// Binary VA: 0x10233e580 | Size: 320 B | Source: third_party/jetski/cli/acp/auth/methods.go:L395
func (t TierInfo) Meta() map[string]interface{} {
	if t.IsEmpty() {
		return nil
	}
	return map[string]interface{}{
		"antigravity": map[string]interface{}{
			"tier": t,
		},
	}
}

// newCodeAssistTierFetcher was inlined into Manager; func1 is preserved at VA 0x102341000. /* inferred */
func newCodeAssistTierFetcher(client *codeassistclient.CodeAssistClient, cliAuth *codeassistclient.CLIAuthProvider) func(context.Context, string) TierInfo {
	// Binary VA: 0x102341000 | Size: 704 B | Source: third_party/jetski/cli/acp/auth/methods.go:L410
	return func(ctx context.Context, method string) TierInfo {
		switch method {
		case "oauth-personal":
			if client == nil {
				return TierInfo{}
			}
			resp, err := client.GetLoadCodeAssistResponse(ctx, "") /* inferred */
			if err != nil || resp == nil {
				return TierInfo{}
			}
			tier := resp.GetPaidTier()
			if tier == nil {
				tier = resp.GetCurrentTier()
			}
			if tier == nil {
				return TierInfo{}
			}
			return TierInfo{
				Name:       tier.GetName(),
				ID:         tier.GetId(),
				UpgradeURI: tier.GetUpgradeSubscriptionUri(),
			}
		case "oauth-business":
			if cliAuth == nil {
				return TierInfo{}
			}
			tierID := cliAuth.GetUserTier()
			if tierID == "" {
				return TierInfo{}
			}
			return TierInfo{
				Name: cmp.Or(method, tierID),
				ID:   tierID,
			}
		default:
			return TierInfo{}
		}
		_ = v1internal_go_proto.LoadCodeAssistResponse{} /* inferred: proto import anchor */
	}
}
