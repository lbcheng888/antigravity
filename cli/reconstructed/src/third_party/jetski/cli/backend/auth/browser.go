// Binary VA: 0x101d40020 | Size: 10656 B (21 funcs) | Source: third_party/jetski/cli/backend/auth/browser.go
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google3/base/go/log"
	"google3/third_party/jetski/cli/analytics/analytics"
	"google3/third_party/jetski/go_utils/go_utils"
	"google3/third_party/jetski/language_server/code_assist_client/codeassistclient"
)

const (
	oauthRedirectURL   = "https://antigravity.google/oauth-callback"
	gcpClientID        = "884354919052-<redacted-gcp-client-id>"
	gcpClientSecret    = "<redacted-oauth-client-secret>"
	consumerClientID   = "1071006060591-<redacted-consumer-client-id>"
	consumerClientSecret = "<redacted-oauth-client-secret>"
	aiCodeScope        = "https://www.googleapis.com/auth/aicode"
)

// Binary VA: 0x10a8e4660 | Size: 144 B | Type: auth.oauthMethod
type oauthMethod struct {
	mu            sync.Mutex
	oauthVerifier string
	oauthState    string
	oauthConfig   *oauth2.Config
	oauthOnce     *sync.Once
	onComplete    func(*AuthResult, error)
	tokenStorage  codeassistclient.TokenStorage
	ctx           context.Context
	name          string
	clientID      string
	clientSecret  string
}

// Binary VA: 0x10a5568e8 | Size: 40 B | Type: auth.savingTokenSource
type savingTokenSource struct {
	base      oauth2.TokenSource
	saveFunc  func(*oauth2.Token) error
	mu        sync.Mutex
	lastToken *oauth2.Token
}

// Binary VA: 0x10a1adf10 | Size: 16 B | Type: auth.userInfoResponse
type userInfoResponse struct {
	Email string `json:"email"`
}

// Binary VA: 0x101d40020 | Size: 16 B | Source: third_party/jetski/cli/backend/auth/browser.go:L51
func (o *oauthMethod) TryAuth(context.Context) *AuthResult {
	return nil
}

// Binary VA: 0x101d40030 | Size: 976 B | Source: third_party/jetski/cli/backend/auth/browser.go:L55
func (o *oauthMethod) StartInteractive(ctx context.Context, onComplete func(*AuthResult, error)) string {
	log.ctxlogf(ctx, "%sOAuth: starting OAuth flow", o.name)
	cfg := o.getOAuthConfig()
	cfg.RedirectURL = oauthRedirectURL

	verifier := oauth2.GenerateVerifier()
	state := generateState()
	once := new(sync.Once)

	authURL := cfg.AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("prompt", "consent"),
	)

	o.mu.Lock()
	o.oauthConfig = cfg
	o.oauthVerifier = verifier
	o.oauthState = state
	o.oauthOnce = once
	o.onComplete = onComplete
	o.mu.Unlock()

	if os.Getenv("SSH_CLIENT") == "" && os.Getenv("SSH_TTY") == "" && os.Getenv("SSH_CONNECTION") == "" {
		// Binary VA: 0x101d45360 | Size: 64 B | Source: third_party/jetski/cli/backend/auth/browser.go:L80
		go OpenBrowser(authURL)
	}
	return authURL
}

// Binary VA: 0x101d40400 | Size: 288 B | Source: third_party/jetski/cli/backend/auth/browser.go:L86
func (o *oauthMethod) SubmitAuthorizationCode(input string) {
	code := input
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		if u, err := url.Parse(input); err == nil {
			if c := u.Query().Get("code"); c != "" {
				code = c
			}
		}
	}
	o.completeOAuth(code)
}

// Binary VA: 0x101d40520 | Size: 576 B | Source: third_party/jetski/cli/backend/auth/browser.go:L97
func (o *oauthMethod) completeOAuth(code string) {
	o.mu.Lock()
	once := o.oauthOnce
	o.mu.Unlock()

	if once != nil {
		// Binary VA: 0x101d453a0 | Size: 560 B | Source: third_party/jetski/cli/backend/auth/browser.go:L112
		once.Do(func() {
			o.mu.Lock()
			verifier := o.oauthVerifier
			state := o.oauthState
			cfg := o.oauthConfig
			onComplete := o.onComplete
			o.cleanupStateUnlocked()
			o.mu.Unlock()

			if cfg == nil || state == "" {
				if onComplete != nil {
					onComplete(nil, fmt.Errorf("no OAuth process in progress"))
				}
				return
			}

			// Binary VA: 0x101d455d0 | Size: 2208 B | Source: third_party/jetski/cli/backend/auth/browser.go:L127
			go func() {
				tok, err := cfg.Exchange(o.ctx, code, oauth2.VerifierOption(verifier))
				if err != nil {
					if go_utils.IsNetworkError(err) {
						log.ctxlogf(o.ctx, "%sOAuth: token exchange failed due to network error (check firewall/network restrictions): %v", o.name, err)
					} else {
						log.ctxlogf(o.ctx, "%sOAuth: token exchange failed", o.name)
					}
					analytics.RecordSignInError("token_exchange", "OAuth token exchange failed")
					if onComplete != nil {
						onComplete(nil, fmt.Errorf("token exchange failed: %w", err))
					}
					return
				}

				if err := o.tokenStorage.SaveToken(tok, o.name); err != nil {
					log.ctxlogf(o.ctx, "%sOAuth: failed to persist token to keyring: %v", o.name, err)
				}
				log.ctxlogf(o.ctx, "%sOAuth: token acquired, expiry=%v", o.name, tok.Expiry)

				ts := &savingTokenSource{
					base: codeassistclient.RefreshingTokenSource(o.ctx, cfg, tok),
					// Binary VA: 0x101d45e70 | Size: 96 B | Source: third_party/jetski/cli/backend/auth/browser.go:L148
					saveFunc: func(t *oauth2.Token) error {
						return o.tokenStorage.SaveToken(t, o.name)
					},
				}

				log.ctxlogf(o.ctx, "%sOAuth: authentication completed successfully", o.name)
				res := &AuthResult{
					TokenSource: ts,
					AuthMethod:  o.name,
				}
				if claims, ok := codeassistclient.ExtractOIDCClaims(tok); ok && claims.Email != "" {
					res.Email = claims.Email
				} else {
					email, err := fetchUserInfo(o.ctx, cfg, tok)
					if err != nil {
						log.ctxlogf(o.ctx, "%sOAuth: failed to fetch user info: %v", o.name, err)
						analytics.RecordSignInError("fetch_user_info", "Failed to fetch OAuth user info")
					} else {
						res.Email = email
					}
				}
				if res.Email != "" {
					log.ctxlogf(o.ctx, "%sOAuth: authenticated successfully as %s", o.name, res.Email)
				}
				if onComplete != nil {
					onComplete(res, nil)
				}
			}()
		})
		return
	}

	o.mu.Lock()
	onComplete := o.onComplete
	o.mu.Unlock()
	if onComplete != nil {
		onComplete(nil, fmt.Errorf("no OAuth process in progress"))
	}
}

// Binary VA: 0x101d40760 | Size: 272 B | Source: third_party/jetski/cli/backend/auth/browser.go:L177
func (o *oauthMethod) Cancel() {
	o.mu.Lock()
	o.cleanupStateUnlocked()
	o.mu.Unlock()
}

// Binary VA: 0x101d40870 | Size: 288 B | Source: third_party/jetski/cli/backend/auth/browser.go:L183
func (o *oauthMethod) Logout() {
	o.Cancel()
	if err := o.tokenStorage.RemoveToken(); err != nil {
		log.ctxlogf(o.ctx, "%sOAuth: failed to remove token: %v", o.name, err)
	}
}

// Binary VA: 0x101d40990 | Size: 16 B | Source: third_party/jetski/cli/backend/auth/browser.go:L190
func (o *oauthMethod) Name() string {
	return o.name
}

// Inlined into Cancel and completeOAuth.func1 (Source: third_party/jetski/cli/backend/auth/browser.go:L193)
func (o *oauthMethod) cleanupStateUnlocked() {
	o.oauthVerifier = ""
	o.oauthState = ""
	o.oauthConfig = nil
	o.oauthOnce = nil
}

// Binary VA: 0x101d409a0 | Size: 480 B | Source: third_party/jetski/cli/backend/auth/browser.go:L199
func getOauthParams(authMethod string) (string, string, error) {
	switch authMethod {
	case "gcp":
		return gcpClientID, gcpClientSecret, nil
	case "consumer":
		return consumerClientID, consumerClientSecret, nil
	default:
		log.ctxlogf(context.Background() /* inferred */, "Unknown auth method: %s", authMethod)
		return "", "", fmt.Errorf("unknown auth method: %s", authMethod)
	}
}

// Binary VA: 0x101d40b80 | Size: 1168 B | Source: third_party/jetski/cli/backend/auth/browser.go:L215
func getScopesByAuthMethod(authMethod string) []string {
	var scopes []string
	if authMethod == "gcp" {
		for _, s := range defaultScopes /* inferred: global slice at 0x10d3d8c70 */ {
			if s == aiCodeScope {
				continue
			}
			scopes = append(scopes, s)
		}
	} else {
		hasAICode := false
		for _, s := range defaultScopes {
			if s == aiCodeScope {
				hasAICode = true
			}
			scopes = append(scopes, s)
		}
		if !hasAICode {
			scopes = append(scopes, aiCodeScope)
		}
	}
	hasOpenID := false
	for _, s := range scopes {
		if s == "openid" {
			hasOpenID = true
			break
		}
	}
	if !hasOpenID {
		scopes = append(scopes, "openid")
	}
	return scopes
}

// Binary VA: 0x101d41010 | Size: 272 B | Source: third_party/jetski/cli/backend/auth/browser.go:L235
func (o *oauthMethod) getOAuthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     o.clientID,
		ClientSecret: o.clientSecret,
		Endpoint:     google.Endpoint,
		Scopes:       getScopesByAuthMethod(o.name),
	}
}

// Binary VA: 0x101d41120 | Size: 1024 B | Source: third_party/jetski/cli/backend/auth/browser.go:L252
func (s *savingTokenSource) Token() (*oauth2.Token, error) {
	tok, err := s.base.Token()
	if err != nil {
		if go_utils.IsNetworkError(err) {
			log.ctxlogf(context.Background() /* inferred */, "token refresh failed due to network error: %v", err)
		} else {
			log.ctxlogf(context.Background() /* inferred */, "token refresh failed: %v", err)
		}
		return nil, err
	}
	s.mu.Lock()
	// Binary VA: 0x101d45ed0 | Size: 112 B | Source: third_party/jetski/cli/backend/auth/browser.go:L263
	defer s.mu.Unlock()
	if s.lastToken == nil || tok.AccessToken != s.lastToken.AccessToken {
		log.ctxlogf(context.Background() /* inferred */, "token refreshed, new expiry=%v", tok.Expiry)
		if err := s.saveFunc(tok); err != nil {
			log.ctxlogf(context.Background() /* inferred */, "Failed to save refreshed token: %v", err)
		}
		s.lastToken = tok
	}
	return tok, nil
}

// Binary VA: 0x101d41520 | Size: 112 B | Source: third_party/jetski/cli/backend/auth/browser.go:L274
func generateState() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.URLEncoding.EncodeToString(b)
}

// Binary VA: 0x101d41590 | Size: 800 B | Source: third_party/jetski/cli/backend/auth/browser.go:L292
func browserCommand(targetURL string) []string {
	switch runtime.GOOS {
	case "linux":
		return []string{"xdg-open", targetURL}
	case "darwin":
		return []string{"open", targetURL}
	case "windows":
		return []string{"rundll32", "url.dll,FileProtocolHandler", targetURL}
	case "android":
		for _, bin := range []string{"termux-open-url", "xdg-open"} {
			if _, err := exec.LookPath(bin); err == nil {
				return []string{bin, targetURL}
			}
		}
	}
	return nil
}

// Binary VA: 0x101d418b0 | Size: 416 B | Source: third_party/jetski/cli/backend/auth/browser.go:L313
func OpenBrowser(targetURL string) {
	args := browserCommand(targetURL)
	if len(args) == 0 {
		log.ctxlogf(context.Background() /* inferred */, "Failed to open browser: no launcher available on %s", runtime.GOOS)
		return
	}
	cmd := exec.Command(args[0], args[1:]...)
	if err := cmd.Start(); err != nil {
		log.ctxlogf(context.Background() /* inferred */, "Failed to open browser: %v", err)
	}
}

// Binary VA: 0x101d41a50 | Size: 848 B | Source: third_party/jetski/cli/backend/auth/browser.go:L331
func fetchUserInfo(ctx context.Context, cfg *oauth2.Config, tok *oauth2.Token) (string, error) {
	client := cfg.Client(ctx, tok)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		return "", fmt.Errorf("userinfo request failed: %w", err)
	}
	// Binary VA: 0x101d45f40 | Size: 64 B | Source: third_party/jetski/cli/backend/auth/browser.go:L337
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("userinfo returned status %d", resp.StatusCode)
	}
	var info userInfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", fmt.Errorf("failed to decode userinfo: %w", err)
	}
	return info.Email, nil
}
