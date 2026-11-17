// Binary VA: 0x101d42c70 | Size: 496 B (4 funcs) | Source: third_party/jetski/cli/backend/auth/gateway.go
package auth

import (
	"context"
	"fmt"

	"google3/base/go/log"
)

const gatewayURLEnvVar = "AGY_LLM_GATEWAY_URL"

// Binary VA: 0x10a1ade20 | Size: 16 B | Type: auth.gatewayAuth
type gatewayAuth struct {
	gatewayURL string
}

// Binary VA: 0x101d42c70 | Size: 224 B | Source: third_party/jetski/cli/backend/auth/gateway.go:L28
func (g *gatewayAuth) TryAuth(ctx context.Context) *AuthResult {
	if g.gatewayURL == "" {
		log.ctxlogf(ctx, "gatewayAuth: %s is not set", gatewayURLEnvVar)
		return nil
	}
	return &AuthResult{
		AuthMethod: "gateway",
	}
}

// Binary VA: 0x101d42d50 | Size: 240 B | Source: third_party/jetski/cli/backend/auth/gateway.go:L39
func (g *gatewayAuth) StartInteractive(ctx context.Context, onComplete func(*AuthResult, error)) string {
	if res := g.TryAuth(ctx); res != nil {
		onComplete(res, nil)
		return ""
	}
	onComplete(nil, fmt.Errorf("%s is not set", gatewayURLEnvVar))
	return ""
}

// Binary VA: 0x101d42e40 | Size: 16 B | Source: third_party/jetski/cli/backend/auth/gateway.go:L49
func (g *gatewayAuth) Logout() {}

// Binary VA: 0x101d42e50 | Size: 16 B | Source: third_party/jetski/cli/backend/auth/gateway.go:L51
func (g *gatewayAuth) Name() string {
	return "gateway"
}
