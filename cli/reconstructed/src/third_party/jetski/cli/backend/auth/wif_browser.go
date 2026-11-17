// Binary VA: 0x101d44840 | Size: 5792 B (9 funcs) | Source: third_party/jetski/cli/backend/auth/wif_browser.go
package auth

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"

	"golang.org/x/oauth2"
	"google3/base/go/log"
	"google3/third_party/jetski/cli/analytics/analytics"
	"google3/third_party/jetski/go_utils/go_utils"
	"google3/third_party/jetski/language_server/code_assist_client/codeassistclient"
)

const wifClientID = "a2a6f0e3e-3ff0-462b-869a-47277df4c806"

var wifScopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"openid",
	"email",
	"profile",
	"offline_access",
}

// Binary VA: 0x10a300280 | Size: 160 B | Type: auth.wifOAuthMethod
type wifOAuthMethod struct {
	oauthMethod
	wifProvider string
}

// Binary VA: 0x101d44840 | Size: 256 B | Source: third_party/jetski/cli/backend/auth/wif_browser.go:L36
func (w *wifOAuthMethod) SetWIFProvider(provider string) {
	w.mu.Lock()
	// Binary VA: 0x101d46f50 | Size: 112 B | Source: third_party/jetski/cli/backend/auth/wif_browser.go:L38
	defer w.mu.Unlock()
	w.wifProvider = provider
}

// Binary VA: 0x101d44940 | Size: 1344 B | Source: third_party/jetski/cli/backend/auth/wif_browser.go:L42
func (w *wifOAuthMethod) StartInteractive(ctx context.Context, onComplete func(*AuthResult, error)) string {
	log.ctxlogf(ctx, "%sOAuth: starting OAuth flow", w.name)

	w.mu.Lock()
	cfg := &oauth2.Config{
		ClientID: wifClientID,
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://auth.cloud.google/authorize",
			TokenURL: "https://sts.googleapis.com/v1/oauthtoken",
		},
		Scopes: wifScopes,
	}
	provider := w.wifProvider
	w.mu.Unlock()

	cfg.RedirectURL = oauthRedirectURL

	verifier := oauth2.GenerateVerifier()
	state := generateState()
	once := new(sync.Once)

	authURL := cfg.AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("provider_name", provider),
	)

	w.mu.Lock()
	w.oauthConfig = cfg
	w.oauthVerifier = verifier
	w.oauthState = state
	w.oauthOnce = once
	w.onComplete = onComplete
	w.mu.Unlock()

	if os.Getenv("SSH_CLIENT") == "" && os.Getenv("SSH_TTY") == "" && os.Getenv("SSH_CONNECTION") == "" {
		// Binary VA: 0x101d46fc0 | Size: 64 B | Source: third_party/jetski/cli/backend/auth/wif_browser.go:L72
		go OpenBrowser(authURL)
	}
	return authURL
}

// Binary VA: 0x101d44e80 | Size: 288 B | Source: third_party/jetski/cli/backend/auth/wif_browser.go:L78
func (w *wifOAuthMethod) SubmitAuthorizationCode(input string) {
	code := input
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		if u, err := url.Parse(input); err == nil {
			if c := u.Query().Get("code"); c != "" {
				code = c
			}
		}
	}
	w.completeOAuth(code)
}

// Binary VA: 0x101d44fa0 | Size: 576 B | Source: third_party/jetski/cli/backend/auth/wif_browser.go:L89
func (w *wifOAuthMethod) completeOAuth(code string) {
	w.mu.Lock()
	once := w.oauthOnce
	w.mu.Unlock()

	if once != nil {
		// Binary VA: 0x101d47000 | Size: 608 B | Source: third_party/jetski/cli/backend/auth/wif_browser.go:L104
		once.Do(func() {
			w.mu.Lock()
			verifier := w.oauthVerifier
			state := w.oauthState
			cfg := w.oauthConfig
			onComplete := w.onComplete
			provider := w.wifProvider
			w.cleanupStateUnlocked()
			w.mu.Unlock()

			if cfg == nil || state == "" {
				if onComplete != nil {
					onComplete(nil, fmt.Errorf("no OAuth process in progress"))
				}
				return
			}

			// Binary VA: 0x101d47260 | Size: 2224 B | Source: third_party/jetski/cli/backend/auth/wif_browser.go:L120
			go func() {
				tok, err := cfg.Exchange(w.ctx, code, oauth2.VerifierOption(verifier))
				if err != nil {
					if go_utils.IsNetworkError(err) {
						log.ctxlogf(w.ctx, "%sOAuth: token exchange failed due to network error (check firewall/network restrictions): %v", w.name, err)
					} else {
						log.ctxlogf(w.ctx, "%sOAuth: token exchange failed: %v", w.name, err)
					}
					analytics.RecordSignInError("token_exchange", "OAuth token exchange failed")
					if onComplete != nil {
						onComplete(nil, fmt.Errorf("token exchange failed: %w", err))
					}
					return
				}

				if err := w.tokenStorage.SaveToken(tok, w.name, codeassistclient.WithWIFProvider(provider)); err != nil {
					log.ctxlogf(w.ctx, "%sOAuth: failed to persist token to keyring: %v", w.name, err)
				}
				log.ctxlogf(w.ctx, "%sOAuth: token acquired, expiry=%v", w.name, tok.Expiry)

				ts := &savingTokenSource{
					base: codeassistclient.RefreshingTokenSource(w.ctx, cfg, tok),
					// Binary VA: 0x101d47b10 | Size: 320 B | Source: third_party/jetski/cli/backend/auth/wif_browser.go:L144
					saveFunc: func(t *oauth2.Token) error {
						return w.tokenStorage.SaveToken(t, w.name, codeassistclient.WithWIFProvider(provider))
					},
				}

				log.ctxlogf(w.ctx, "%sOAuth: authentication completed successfully", w.name)
				res := &AuthResult{
					TokenSource: ts,
					AuthMethod:  w.name,
					WIFProvider: provider,
				}
				claims, err := codeassistclient.ExtractOIDCClaims(tok)
				if err != nil {
					log.ctxlogf(w.ctx, "%sOAuth: failed to extract OIDC claims for email: %v", w.name, err)
				} else {
					res.Email = claims.Email
				}
				if onComplete != nil {
					onComplete(res, nil)
				}
			}()
		})
		return
	}

	w.mu.Lock()
	onComplete := w.onComplete
	w.mu.Unlock()
	if onComplete != nil {
		onComplete(nil, fmt.Errorf("no OAuth process in progress"))
	}
}
