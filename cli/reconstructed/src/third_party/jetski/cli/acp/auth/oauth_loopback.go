// Reconstructed from agy v1.5.10 binary evidence (disassembly + Go type table).
// Binary VA: 0x10233e6c0 | Size: 16 funcs (first func consumerOAuthScopes, 672 B) | Source: third_party/jetski/cli/acp/auth/oauth_loopback.go:L36
// Reconstructed from agy v1.5.10 (arm64 Mach-O)
// Package auth: third_party/jetski/cli/acp/auth/oauth_loopback.go

package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"google3/base/go/log"
	"google3/third_party/golang/oauth2/google/google"
	"google3/third_party/golang/oauth2/oauth2"
	"google3/third_party/golang/osappopen/open/open"
	"google3/third_party/jetski/language_server/code_assist_client/codeassistclient"
)

// baseOAuthScopes holds the 5 default Google OAuth scopes. /* inferred: 0x10d3d8c70 */
var baseOAuthScopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/userinfo.email",
	"https://www.googleapis.com/auth/userinfo.profile",
	"https://www.googleapis.com/auth/cclog",
	"https://www.googleapis.com/auth/experimentsandconfigs",
}

// Binary VA: 0x10233e6c0 | Size: 672 B | Source: third_party/jetski/cli/acp/auth/oauth_loopback.go:L36
func consumerOAuthScopes() []string {
	scopes := slices.Clone(baseOAuthScopes)
	if !slices.Contains(scopes, "https://www.googleapis.com/auth/aicode") {
		scopes = append(scopes, "https://www.googleapis.com/auth/aicode")
	}
	if !slices.Contains(scopes, "openid") {
		scopes = append(scopes, "openid")
	}
	return scopes
}

// businessOAuthScopes was inlined into newBusinessBrowserLogin at L51-59. /* inferred */
func businessOAuthScopes() []string {
	scopes := make([]string, 0, len(baseOAuthScopes)+1)
	for _, s := range baseOAuthScopes {
		if s == "https://www.googleapis.com/auth/aicode" {
			continue
		}
		scopes = append(scopes, s)
	}
	if !slices.Contains(scopes, "openid") {
		scopes = append(scopes, "openid")
	}
	return scopes
}

// Binary VA: 0x10233e960 | Size: 720 B | Source: third_party/jetski/cli/acp/auth/oauth_loopback.go:L67
func NewBackendTokenStore(productName string, isJetski bool, appDataDir string) (codeassistclient.TokenStorage, error) {
	if productName == "" || !filepath.IsAbs(appDataDir) {
		return nil, fmt.Errorf("invalid token store config (product=%q, appDataDir=%q)", productName, appDataDir)
	}
	serviceName := "antigravity"
	if isJetski {
		serviceName = "jetski-cli"
	}
	return codeassistclient.NewCompositeTokenStorage(
		&codeassistclient.KeyringTokenStorage{ServiceName: serviceName},
		&codeassistclient.fileTokenStorage{Path: filepath.Join(appDataDir, productName+"-oauth-token")}, /* inferred */
		filepath.Join(appDataDir, "cache", productName+"-keyring-unavailable"),
	), nil
}

// loopbackLogin implements local HTTP loopback OAuth2 PKCE login.
// Binary Type VA: 0x10a8e4ed0
type loopbackLogin struct {
	productName    string
	isJetski       bool
	appDataDir     string
	authMethod     string
	clientID       string
	clientSecret   string
	scopes         []string
	endpoint       oauth2.Endpoint
	openURL        func(string) error
	browserBlocker func() string
	persist        func(func() error) error
}

// Binary VA: 0x10233ec30 | Size: 368 B | Source: third_party/jetski/cli/acp/auth/oauth_loopback.go:L105
func newBrowserLogin(productName string, isJetski bool, appDataDir string) *loopbackLogin {
	return &loopbackLogin{
		productName:    productName,
		isJetski:       isJetski,
		appDataDir:     appDataDir,
		authMethod:     "consumer",
		clientID:       "1071006060591-<redacted-consumer-client-id>",
		clientSecret:   "<redacted-oauth-client-secret>",
		scopes:         consumerOAuthScopes(),
		endpoint:       google.Endpoint,
		openURL:        open.Start,
		browserBlocker: browserUnavailableReason,
	}
}

// Binary VA: 0x10233eda0 | Size: 848 B | Source: third_party/jetski/cli/acp/auth/oauth_loopback.go:L122
func newBusinessBrowserLogin(productName string, isJetski bool, appDataDir string) *loopbackLogin {
	return &loopbackLogin{
		productName:    productName,
		isJetski:       isJetski,
		appDataDir:     appDataDir,
		authMethod:     "gcp",
		clientID:       "884354919052-<redacted-gcp-client-id>",
		clientSecret:   "<redacted-oauth-client-secret>",
		scopes:         businessOAuthScopes(),
		endpoint:       google.Endpoint,
		openURL:        open.Start,
		browserBlocker: browserUnavailableReason,
	}
}

// Login was inlined into Manager (referenced via (*loopbackLogin).Login-fm). /* inferred */
func (l *loopbackLogin) Login(ctx context.Context) error {
	_, err := l.LoginToken(ctx)
	return err
}

// Binary VA: 0x10233f0f0 | Size: 656 B | Source: third_party/jetski/cli/acp/auth/oauth_loopback.go:L146
func (l *loopbackLogin) LoginToken(ctx context.Context) (*oauth2.Token, error) {
	store, err := NewBackendTokenStore(l.productName, l.isJetski, l.appDataDir)
	if err != nil {
		return nil, fmt.Errorf("browser sign-in is unavailable: %w", err)
	}
	tok, err := l.acquire(ctx)
	if err != nil {
		return nil, err
	}
	authMethod := l.authMethod
	if authMethod == "" {
		authMethod = "consumer"
	}
	save := func() error {
		// Binary VA: 0x102340960 | Size: 272 B | Source: third_party/jetski/cli/acp/auth/oauth_loopback.go:L159
		if err := store.ClearTokens(ctx); err != nil {
			return err
		}
		if err := store.SaveToken(ctx, tok, authMethod); err != nil {
			return fmt.Errorf("couldn't save your credentials: %w", err)
		}
		return nil
	}
	if l.persist != nil {
		err = l.persist(save)
	} else {
		err = save()
	}
	if err != nil {
		return nil, err
	}
	return tok, nil
}

// Binary VA: 0x10233f380 | Size: 368 B | Source: third_party/jetski/cli/acp/auth/oauth_loopback.go:L179
func (l *loopbackLogin) oauthConfig(redirectURL string) *oauth2.Config {
	clientID := l.clientID
	clientSecret := l.clientSecret
	if clientID == "" {
		clientID = "1071006060591-<redacted-consumer-client-id>"
		clientSecret = "<redacted-oauth-client-secret>"
	}
	scopes := l.scopes
	if len(scopes) == 0 {
		scopes = consumerOAuthScopes()
	}
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     l.endpoint,
		RedirectURL:  redirectURL,
		Scopes:       scopes,
	}
}

// Binary VA: 0x10233f4f0 | Size: 2144 B | Source: third_party/jetski/cli/acp/auth/oauth_loopback.go:L199
func (l *loopbackLogin) acquire(ctx context.Context) (*oauth2.Token, error) {
	if l.browserBlocker != nil {
		if reason := l.browserBlocker(); reason != "" {
			return nil, &BrowserUnavailableError{
				Reason:     reason,
				AuthMethod: l.authMethod,
			}
		}
	}
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, fmt.Errorf("couldn't start the local sign-in listener: %w", err)
	}
	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		ln.Close()
		return nil, fmt.Errorf("listener address %v is not a *net.TCPAddr", ln.Addr())
	}

	state := rand.Text()
	cfg := l.oauthConfig(fmt.Sprintf("http://localhost:%d%s", tcpAddr.Port, "/auth/callback"))
	verifier := oauth2.GenerateVerifier()
	authURL := cfg.AuthCodeURL(
		state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
	)

	resCh := make(chan callbackResult, 1)
	srv := &http.Server{
		Handler:           callbackHandler(state, resCh),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		// Binary VA: 0x102340ab0 | Size: 224 B | Source: third_party/jetski/cli/acp/auth/oauth_loopback.go:L229
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.CtxErrorf(context.Background(), "OAuth callback server failed: %v", err)
		}
	}()
	defer func() {
		// Binary VA: 0x102340a70 | Size: 64 B | Source: third_party/jetski/cli/acp/auth/oauth_loopback.go:L234
		srv.Close()
	}()

	log.CtxInfof(ctx, "Opening browser for ACP sign-in (method=%s, redirect=%s)", l.authMethod, cfg.RedirectURL)
	if err := l.openURL(authURL); err != nil {
		return nil, fmt.Errorf("couldn't open a web browser: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-resCh:
		srv.Shutdown(ctx)
		if res.err != nil {
			return nil, res.err
		}
		tok, err := cfg.Exchange(ctx, res.code, oauth2.VerifierOption(verifier))
		if err != nil {
			return nil, fmt.Errorf("couldn't complete sign-in: %w", err)
		}
		return tok, nil
	}
}

// BrowserUnavailableError indicates browser-based OAuth cannot run in this environment.
// Binary Type VA: 0x10a3071e0
type BrowserUnavailableError struct {
	Reason     string
	AuthMethod string
}

// Binary VA: 0x10233fd50 | Size: 80 B | Source: third_party/jetski/cli/acp/auth/oauth_loopback.go:L266
func (e *BrowserUnavailableError) Error() string {
	return "browser sign-in isn't available here: " + e.Reason
}

// callbackResult carries the OAuth callback result over the channel.
// Binary Type VA: 0x10a307270
type callbackResult struct {
	code string
	err  error
}

// Binary VA: 0x10233fda0 | Size: 256 B | Source: third_party/jetski/cli/acp/auth/oauth_loopback.go:L278
func callbackHandler(state string, ch chan<- callbackResult) http.Handler {
	var once sync.Once
	mux := &http.ServeMux{}
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		// Binary VA: 0x102340b90 | Size: 864 B | Source: third_party/jetski/cli/acp/auth/oauth_loopback.go:L281
		q := r.URL.Query()
		if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 {
			http.Error(w, "Invalid or missing state parameter.", http.StatusBadRequest)
			return
		}
		if errParam := q.Get("error"); errParam != "" {
			fmt.Fprint(w, "Sign-in was not completed. You can close this tab.")
			once.Do(func() {
				// Binary VA: 0x102340f40 | Size: 192 B | Source: third_party/jetski/cli/acp/auth/oauth_loopback.go:L289
				ch <- callbackResult{err: fmt.Errorf("sign-in was denied in the browser (%s)", errParam)}
			})
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "Missing authorization code.", http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, "Sign-in received. You can close this tab and return to your IDE.")
		once.Do(func() {
			// Binary VA: 0x102340ef0 | Size: 80 B | Source: third_party/jetski/cli/acp/auth/oauth_loopback.go:L298
			ch <- callbackResult{code: code}
		})
	})
	return mux
}

// Binary VA: 0x10233fea0 | Size: 192 B | Source: third_party/jetski/cli/acp/auth/oauth_loopback.go:L305
func browserUnavailableReason() string {
	for _, k := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		if os.Getenv(k) != "" {
			return "this is a remote (SSH) session, so the browser's sign-in redirect can't reach this machine"
		}
	}
	return ""
}
