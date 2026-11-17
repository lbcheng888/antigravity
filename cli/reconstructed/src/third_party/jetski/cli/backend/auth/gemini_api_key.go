// Binary VA: 0x101d42e60 | Size: 496 B (4 funcs) | Source: third_party/jetski/cli/backend/auth/gemini_api_key.go
package auth

import (
	"context"
	"fmt"

	"google3/base/go/log"
)

const geminiAPIKeyEnvVar = "GEMINI_API_KEY"

// Binary VA: 0x10a1ade98 | Size: 16 B | Type: auth.geminiAPIKeyAuth
type geminiAPIKeyAuth struct {
	apiKey string
}

// Binary VA: 0x101d42e60 | Size: 224 B | Source: third_party/jetski/cli/backend/auth/gemini_api_key.go:L30
func (g *geminiAPIKeyAuth) TryAuth(ctx context.Context) *AuthResult {
	if g.apiKey == "" {
		log.ctxlogf(ctx, "geminiAPIKeyAuth: %s is not set", geminiAPIKeyEnvVar)
		return nil
	}
	return &AuthResult{
		AuthMethod: "gemini_api_key",
	}
}

// Binary VA: 0x101d42f40 | Size: 240 B | Source: third_party/jetski/cli/backend/auth/gemini_api_key.go:L42
func (g *geminiAPIKeyAuth) StartInteractive(ctx context.Context, onComplete func(*AuthResult, error)) string {
	if res := g.TryAuth(ctx); res != nil {
		onComplete(res, nil)
		return ""
	}
	onComplete(nil, fmt.Errorf("%s is not set", geminiAPIKeyEnvVar))
	return ""
}

// Binary VA: 0x101d43030 | Size: 16 B | Source: third_party/jetski/cli/backend/auth/gemini_api_key.go:L52
func (g *geminiAPIKeyAuth) Logout() {}

// Binary VA: 0x101d43040 | Size: 16 B | Source: third_party/jetski/cli/backend/auth/gemini_api_key.go:L54
func (g *geminiAPIKeyAuth) Name() string {
	return "gemini_api_key"
}
