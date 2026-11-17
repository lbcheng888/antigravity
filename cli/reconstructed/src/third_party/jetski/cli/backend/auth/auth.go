// Binary VA: 0x101d3f460 | Size: 3328 B (9 funcs) | Source: third_party/jetski/cli/backend/auth/auth.go
package auth

import (
	"context"
	"fmt"

	"golang.org/x/oauth2"
	"google3/base/go/log"
	"google3/third_party/jetski/language_server/code_assist_client/codeassistclient"
)

// Binary VA: 0x10a25f120 | Size: 16 B | Type: auth.AuthMethod
type AuthMethod interface {
	Logout()
	Name() string
	StartInteractive(context.Context, func(*AuthResult, error)) string
	TryAuth(context.Context) *AuthResult
}

// Binary VA: 0x10a108610 | Size: 16 B | Type: auth.supportedChecker
type supportedChecker interface {
	IsSupported() bool
}

// Binary VA: 0x10a7b6fe8 | Size: 112 B | Type: auth.AuthResult
type AuthResult struct {
	TokenSource  oauth2.TokenSource
	Email        string
	QuotaProject string
	AuthMethod   string
	UserTier     string
	WIFProvider  string
	Location     string
}

// Binary VA: 0x10a1adc40 | Size: 16 B | Type: auth.StartGCPConfig
type StartGCPConfig struct {
	WIFProvider string
}

// Binary VA: 0x10a556828 | Size: 72 B | Type: auth.ChainedAuth
type ChainedAuth struct {
	methods   []AuthMethod
	gcpMethod AuthMethod
	active    AuthMethod
	storage   codeassistclient.TokenStorage
}

// Binary VA: 0x101d3f460 | Size: 544 B | Source: third_party/jetski/cli/backend/auth/auth.go:L150
func (c *ChainedAuth) Authenticate(ctx context.Context) *AuthResult {
	for _, m := range c.methods {
		res := m.TryAuth(ctx)
		if res == nil {
			continue
		}
		c.active = m
		if res.AuthMethod == "gcp" && c.gcpMethod != nil {
			c.active = c.gcpMethod
		}
		log.ctxlogf(ctx, "ChainedAuth: authenticated via %s (effective: %s)", m.Name(), c.active.Name())
		return res
	}
	return nil
}

// Binary VA: 0x101d3f680 | Size: 368 B | Source: third_party/jetski/cli/backend/auth/auth.go:L167
func (c *ChainedAuth) StartInteractive(ctx context.Context, onComplete func(*AuthResult, error)) string {
	for _, m := range c.methods {
		done := false
		// Binary VA: 0x101d45220 | Size: 320 B | Source: third_party/jetski/cli/backend/auth/auth.go:L170
		url := m.StartInteractive(ctx, func(res *AuthResult, err error) {
			done = true
			if res != nil {
				c.active = m
				log.ctxlogf(ctx, "ChainedAuth: interactive auth completed via %s", m.Name())
			}
			onComplete(res, err)
		})
		if url != "" || done {
			c.active = m
			return url
		}
	}
	return ""
}

// Binary VA: 0x101d3f7f0 | Size: 736 B | Source: third_party/jetski/cli/backend/auth/auth.go:L195
func (c *ChainedAuth) StartGCPAuthProcess(ctx context.Context, cfg StartGCPConfig, onComplete func(*AuthResult, error)) string {
	if c.gcpMethod == nil {
		log.ctxlogf(ctx, "StartGCPAuthProcess called but gcpMethod is nil")
		onComplete(nil, fmt.Errorf("GCP authentication is not supported in this configuration"))
		return ""
	}
	if cfg.WIFProvider != "" {
		// Inlined from wif_browser.go:L24-32
		c.gcpMethod = &wifOAuthMethod{
			oauthMethod: oauthMethod{
				tokenStorage: c.storage,
				ctx:          ctx,
				name:         "gcp",
				clientID:     wifClientID,
			},
			wifProvider: cfg.WIFProvider,
		}
	} else {
		// Inlined from gcp_external.go:L13-18
		c.gcpMethod = &oauthMethod{
			tokenStorage: c.storage,
			ctx:          ctx,
			name:         "gcp",
			clientID:     gcpClientID,
			clientSecret: gcpClientSecret,
		}
	}
	c.active = c.gcpMethod
	return c.gcpMethod.StartInteractive(ctx, onComplete)
}

// Binary VA: 0x101d3fad0 | Size: 144 B | Source: third_party/jetski/cli/backend/auth/auth.go:L213
func (c *ChainedAuth) Logout() {
	for _, m := range c.methods {
		m.Logout()
	}
	c.active = nil
}

// Binary VA: 0x101d3fb60 | Size: 16 B | Source: third_party/jetski/cli/backend/auth/auth.go:L221
func (c *ChainedAuth) Active() AuthMethod {
	return c.active
}

// Binary VA: 0x101d3fb70 | Size: 224 B | Source: third_party/jetski/cli/backend/auth/auth.go:L226
func (c *ChainedAuth) Cancel() {
	for _, m := range c.methods {
		if canceler, ok := m.(interface{ Cancel() }); ok {
			canceler.Cancel()
		}
	}
}

// Binary VA: 0x101d3fc50 | Size: 480 B | Source: third_party/jetski/cli/backend/auth/auth.go:L236
func (c *ChainedAuth) SubmitAuthorizationCode(code string) {
	if c.active != nil && c.active == c.gcpMethod {
		if sub, ok := c.gcpMethod.(interface{ SubmitAuthorizationCode(string) }); ok {
			sub.SubmitAuthorizationCode(code)
			return
		}
	}
	for _, m := range c.methods {
		if sub, ok := m.(interface{ SubmitAuthorizationCode(string) }); ok {
			sub.SubmitAuthorizationCode(code)
			return
		}
	}
}

// Binary VA: 0x101d3fe30 | Size: 496 B | Source: third_party/jetski/cli/backend/auth/auth.go:L256
func (c *ChainedAuth) IsMethodSupported(name string) bool {
	if name == "gcp" {
		if c.gcpMethod == nil {
			return false
		}
		if sc, ok := c.gcpMethod.(supportedChecker); ok {
			return sc.IsSupported()
		}
		return true
	}
	for _, m := range c.methods {
		if m.Name() == name {
			if sc, ok := m.(supportedChecker); ok {
				return sc.IsSupported()
			}
			return true
		}
	}
	return false
}
