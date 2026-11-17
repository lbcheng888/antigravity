package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	GoogleAuthURL  = "https://accounts.google.com/o/oauth2/auth"
	GoogleTokenURL = "https://oauth2.googleapis.com/token"
	WIFAuthURL     = "https://auth.cloud.google/authorize"
	WIFTokenURL    = "https://sts.googleapis.com/v1/oauthtoken"

	// Non-secret OAuth client fallbacks; supply AGY_OAUTH_CLIENT_ID / AGY_OAUTH_CLIENT_SECRET (or AGY_ENTERPRISE_OAUTH_CLIENT_*) at runtime.
	ConsumerClientID     = "antigravity-consumer-client"
	ConsumerClientSecret = ""
	BusinessClientID     = "antigravity-enterprise-client"
	BusinessClientSecret = ""

	DefaultRedirectURI = "https://antigravity.google/oauth-callback"
)

// DefaultConsumerScopes matches consumer OAuth scopes in agy CLI.
var DefaultConsumerScopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/userinfo.email",
	"https://www.googleapis.com/auth/userinfo.profile",
	"https://www.googleapis.com/auth/cclog",
	"https://www.googleapis.com/auth/experimentsandconfigs",
	"https://www.googleapis.com/auth/aicode",
}

// DefaultBusinessScopes omits the consumer-only aicode scope.
var DefaultBusinessScopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/userinfo.email",
	"https://www.googleapis.com/auth/userinfo.profile",
	"https://www.googleapis.com/auth/cclog",
	"https://www.googleapis.com/auth/experimentsandconfigs",
}

// OAuthConfig defines OAuth 2.0 endpoints, client credentials, and scopes.
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	AuthURL      string
	TokenURL     string
	RedirectURL  string
	Scopes       []string
}

func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// NewConsumerOAuthConfig returns the consumer Google AI OAuth configuration.
func NewConsumerOAuthConfig() OAuthConfig {
	return OAuthConfig{
		ClientID:     envOrDefault("AGY_OAUTH_CLIENT_ID", ConsumerClientID),
		ClientSecret: envOrDefault("AGY_OAUTH_CLIENT_SECRET", ConsumerClientSecret),
		AuthURL:      GoogleAuthURL,
		TokenURL:     GoogleTokenURL,
		RedirectURL:  DefaultRedirectURI,
		Scopes:       append([]string(nil), DefaultConsumerScopes...),
	}
}

// NewBusinessOAuthConfig returns the enterprise Google Cloud OAuth configuration.
func NewBusinessOAuthConfig() OAuthConfig {
	return OAuthConfig{
		ClientID:     envOrDefault("AGY_ENTERPRISE_OAUTH_CLIENT_ID", BusinessClientID),
		ClientSecret: envOrDefault("AGY_ENTERPRISE_OAUTH_CLIENT_SECRET", BusinessClientSecret),
		AuthURL:      GoogleAuthURL,
		TokenURL:     GoogleTokenURL,
		RedirectURL:  DefaultRedirectURI,
		Scopes:       append([]string(nil), DefaultBusinessScopes...),
	}
}

// GenerateRandomBase64URL produces a cryptographically random URL-safe string.
func GenerateRandomBase64URL(numBytes int) (string, error) {
	if numBytes <= 0 {
		numBytes = 32
	}
	b := make([]byte, numBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// BuildPKCEAuthURL constructs an OAuth 2.0 authorization URL with S256 PKCE challenge.
func (cfg OAuthConfig) BuildPKCEAuthURL(redirectURI, state, codeVerifier string) (string, error) {
	u, err := url.Parse(cfg.AuthURL)
	if err != nil {
		return "", err
	}
	if redirectURI == "" {
		redirectURI = cfg.RedirectURL
	}
	q := u.Query()
	q.Set("client_id", cfg.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("scope", strings.Join(cfg.Scopes, " "))
	q.Set("access_type", "offline")
	q.Set("prompt", "consent")
	q.Set("state", state)
	q.Set("code_challenge", Sha256Base64URL(codeVerifier))
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// ExchangeCode exchanges an authorization code and PKCE verifier for an OAuthToken.
func (cfg OAuthConfig) ExchangeCode(ctx context.Context, httpClient *http.Client, code, redirectURI, codeVerifier string) (*OAuthToken, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	if redirectURI == "" {
		redirectURI = cfg.RedirectURL
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {cfg.ClientID},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {codeVerifier},
	}
	if cfg.ClientSecret != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}
	return postTokenForm(ctx, httpClient, cfg.TokenURL, form)
}

// Refresh refreshes an expired OAuthToken using its refresh_token.
func (cfg OAuthConfig) Refresh(ctx context.Context, httpClient *http.Client, refreshToken string) (*OAuthToken, error) {
	if refreshToken == "" {
		return nil, errors.New("no refresh token available")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {cfg.ClientID},
		"refresh_token": {refreshToken},
	}
	if cfg.ClientSecret != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}
	tok, err := postTokenForm(ctx, httpClient, cfg.TokenURL, form)
	if err != nil {
		return nil, err
	}
	if tok.RefreshToken == "" {
		tok.RefreshToken = refreshToken
	}
	return tok, nil
}

func postTokenForm(ctx context.Context, httpClient *http.Client, tokenURL string, form url.Values) (*OAuthToken, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &TokenHTTPStatusError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	var tok OAuthToken
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, err
	}
	if tok.Expiry.IsZero() && tok.ExpiresIn > 0 {
		tok.Expiry = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	}
	return &tok, nil
}

// TokenHTTPStatusError captures HTTP errors from OAuth token endpoint.
type TokenHTTPStatusError struct {
	StatusCode int
	Body       string
}

func (e *TokenHTTPStatusError) Error() string {
	return fmt.Sprintf("oauth2 token endpoint returned HTTP %d: %s", e.StatusCode, e.Body)
}

// LoopbackCallbackServer runs an ephemeral `127.0.0.1:0` `/auth/callback` HTTP listener
// verifying OAuth state with subtle.ConstantTimeCompare.
type LoopbackCallbackServer struct {
	RedirectURL string
	State       string
	Verifier    string
	ResultCh    chan LoopbackResult
	srv         *http.Server
	once        sync.Once
}

// LoopbackResult carries the authorization code or error from browser redirect.
type LoopbackResult struct {
	Code string
	Err  error
}

// StartLoopbackCallback starts a local loopback callback listener for OAuth redirect.
func StartLoopbackCallback() (*LoopbackCallbackServer, error) {
	state, err := GenerateRandomBase64URL(24)
	if err != nil {
		return nil, err
	}
	verifier, err := GenerateRandomBase64URL(48)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	addr := ln.Addr().(*net.TCPAddr)
	cb := &LoopbackCallbackServer{
		RedirectURL: fmt.Sprintf("http://127.0.0.1:%d/auth/callback", addr.Port),
		State:       state,
		Verifier:    verifier,
		ResultCh:    make(chan LoopbackResult, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		gotState := q.Get("state")
		if subtle.ConstantTimeCompare([]byte(gotState), []byte(cb.State)) != 1 {
			http.Error(w, "invalid OAuth state", http.StatusBadRequest)
			cb.deliver(LoopbackResult{Err: errors.New("OAuth state mismatch")})
			return
		}
		if oauthErr := q.Get("error"); oauthErr != "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprintf(w, "<html><body><h3>Authentication Failed</h3><p>%s</p></body></html>", html.EscapeString(oauthErr))
			cb.deliver(LoopbackResult{Err: fmt.Errorf("oauth error: %s", oauthErr)})
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "missing authorization code", http.StatusBadRequest)
			cb.deliver(LoopbackResult{Err: errors.New("missing authorization code")})
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<html><body><h3>Antigravity SDK authentication complete</h3><p>You may return to the terminal.</p></body></html>")
		cb.deliver(LoopbackResult{Code: code})
	})
	cb.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		_ = cb.srv.Serve(ln)
	}()
	return cb, nil
}

func (cb *LoopbackCallbackServer) deliver(res LoopbackResult) {
	cb.once.Do(func() {
		cb.ResultCh <- res
	})
}

// Close shuts down the loopback callback server.
func (cb *LoopbackCallbackServer) Close(ctx context.Context) error {
	if cb == nil || cb.srv == nil {
		return nil
	}
	return cb.srv.Shutdown(ctx)
}
