// Binary VA: 0x101d43050 | Size: 3200 B (6 funcs) | Source: third_party/jetski/cli/backend/auth/keyring.go
package auth

import (
	"context"
	"time"

	"golang.org/x/oauth2"
	"google3/base/go/log"
	"google3/third_party/jetski/go_utils/go_utils"
	"google3/third_party/jetski/language_server/code_assist_client/codeassistclient"
)

const keyringAuthTimeout = 10 * time.Second // 0x2540be400 ns

// Binary VA: 0x10a45a1c0 | Size: 48 B | Type: auth.keyringAuth
type keyringAuth struct {
	storage     codeassistclient.TokenStorage
	ctx         context.Context
	tokenSource oauth2.TokenSource
}

// Binary VA: 0x10a3001f0 | Size: 24 B | Type: auth.tryResult
type tryResult struct {
	result *AuthResult
	ts     oauth2.TokenSource
}

// Binary VA: 0x101d43050 | Size: 560 B | Source: third_party/jetski/cli/backend/auth/keyring.go:L30
func (k *keyringAuth) TryAuth(ctx context.Context) *AuthResult {
	ch := make(chan tryResult, 1)
	// Binary VA: 0x101d46420 | Size: 2032 B | Source: third_party/jetski/cli/backend/auth/keyring.go:L36
	go func() {
		stored, err := k.storage.LoadStoredToken()
		if err != nil {
			log.ctxlogf(ctx, "keyringAuth: failed to load stored token: %v", err)
			ch <- tryResult{}
			return
		}
		if stored == nil || stored.Token == nil {
			ch <- tryResult{}
			return
		}
		if stored.AuthMethod == "gcp" && stored.UserTier == "" && stored.ProjectID == "" {
			log.ctxlogf(ctx, "keyringAuth: incomplete GCP onboarding (no userTier or projectID), treating as unauthenticated")
			ch <- tryResult{}
			return
		}
		cfg, err := oauthConfigFor(stored.AuthMethod, stored.WIFProvider)
		if err != nil {
			log.ctxlogf(ctx, "keyringAuth: failed to get oauth params: %v", err)
			ch <- tryResult{}
			return
		}
		log.ctxlogf(ctx, "keyringAuth: loaded token, expiry=%v expired=%v", stored.Token.Expiry, stored.Token.Expiry.Before(time.Now()))
		baseTS := codeassistclient.RefreshingTokenSource(ctx, cfg, stored.Token)
		ts := &savingTokenSource{
			base: baseTS,
			// Binary VA: 0x101d46c10 | Size: 320 B | Source: third_party/jetski/cli/backend/auth/keyring.go:L67
			saveFunc: func(tok *oauth2.Token) error {
				return k.storage.SaveToken(tok, stored.AuthMethod, codeassistclient.WithWIFProvider(stored.WIFProvider))
			},
		}
		if _, err := ts.Token(); err != nil {
			if go_utils.IsNetworkError(err) {
				log.ctxlogf(ctx, "keyringAuth: token validation failed due to network error (token may still be valid): %v", err)
			} else {
				log.ctxlogf(ctx, "keyringAuth: saved token invalid: %v", err)
			}
			ch <- tryResult{}
			return
		}
		res := &AuthResult{
			TokenSource:  ts,
			AuthMethod:   stored.AuthMethod,
			QuotaProject: stored.ProjectID,
			UserTier:     stored.UserTier,
			WIFProvider:  stored.WIFProvider,
			Location:     stored.Region,
			Email:        stored.IDTokenEmail,
		}
		if res.Email == "" {
			if stored.WIFProvider == "" {
				email, err := fetchUserInfo(ctx, ts)
				if err == nil {
					res.Email = email
				} else {
					log.ctxlogf(ctx, "keyringAuth: failed to fetch user info: %v", err)
				}
			} else {
				log.WarningDepth(1, "keyringAuth: no email claim stored for WIF credential")
			}
		}
		ch <- tryResult{result: res, ts: ts}
	}()

	timer := time.NewTimer(keyringAuthTimeout)
	select {
	case <-ctx.Done():
		log.ctxlogf(ctx, "keyringAuth: context cancelled, skipping keyring auth")
		return nil
	case <-timer.C:
		log.ctxlogf(ctx, "keyringAuth: timed out after %v, skipping keyring auth", keyringAuthTimeout)
		return nil
	case res := <-ch:
		if res.result != nil {
			k.tokenSource = res.ts
		}
		return res.result
	}
}

// Binary VA: 0x101d43280 | Size: 16 B | Source: third_party/jetski/cli/backend/auth/keyring.go:L119
func (k *keyringAuth) StartInteractive(context.Context, func(*AuthResult, error)) string {
	return ""
}

// Binary VA: 0x101d43290 | Size: 256 B | Source: third_party/jetski/cli/backend/auth/keyring.go:L123
func (k *keyringAuth) Logout() {
	if err := k.storage.RemoveToken(); err != nil {
		log.ctxlogf(k.ctx, "keyringAuth: failed to remove token: %v", err)
	}
	k.tokenSource = nil
}

// Binary VA: 0x101d43390 | Size: 16 B | Source: third_party/jetski/cli/backend/auth/keyring.go:L130
func (k *keyringAuth) Name() string {
	return "keyring"
}
