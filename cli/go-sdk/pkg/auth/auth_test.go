package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type memKeyring struct {
	data map[string]string
	err  error
}

func (m *memKeyring) Get(service, user string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	v, ok := m.data[service+":"+user]
	if !ok {
		return "", errors.New("secret not found in keyring")
	}
	return v, nil
}

func (m *memKeyring) Set(service, user, secret string) error {
	if m.err != nil {
		return m.err
	}
	if m.data == nil {
		m.data = map[string]string{}
	}
	m.data[service+":"+user] = secret
	return nil
}

func (m *memKeyring) Delete(service, user string) error {
	if m.err != nil {
		return m.err
	}
	delete(m.data, service+":"+user)
	return nil
}

func makeFakeIDToken(email string) string {
	hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload, _ := json.Marshal(map[string]string{"email": email})
	return hdr + "." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func TestFileTokenStorageRoundTripAndKeepOnSignOut(t *testing.T) {
	dir := t.TempDir()
	fs := &FileTokenStorage{FilePath: filepath.Join(dir, "cli-oauth-token")}
	tok := &OAuthToken{
		AccessToken:  "ya29.test-access-12345",
		RefreshToken: "1//refresh-test",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(time.Hour),
		IDToken:      makeFakeIDToken("dev@example.com"),
	}
	if err := fs.SaveToken(tok, "consumer", WithWIFProvider("pools/p1")); err != nil {
		t.Fatalf("SaveToken failed: %v", err)
	}
	if err := fs.SaveProjectAndRegion("proj-42", "us-central1"); err != nil {
		t.Fatalf("SaveProjectAndRegion failed: %v", err)
	}
	if err := fs.SaveUserTier("ultra", "Google AI Ultra"); err != nil {
		t.Fatalf("SaveUserTier failed: %v", err)
	}

	got, err := fs.LoadStoredToken()
	if err != nil {
		t.Fatalf("LoadStoredToken failed: %v", err)
	}
	if got.IDTokenEmail != "dev@example.com" || got.ProjectID != "proj-42" || got.UserTier != "ultra" {
		t.Fatalf("unexpected loaded metadata: %+v", got)
	}
	if got.Token == nil || got.Token.AccessToken != "ya29.test-access-12345" {
		t.Fatalf("unexpected token: %+v", got.Token)
	}

	if err := fs.RemoveToken(); err != nil {
		t.Fatalf("RemoveToken failed: %v", err)
	}
	after, err := fs.LoadStoredToken()
	if err != nil {
		t.Fatalf("LoadStoredToken after RemoveToken failed: %v", err)
	}
	if after.Token != nil || after.SavedWIFProvider != "pools/p1" {
		t.Fatalf("expected active token cleared and SavedWIFProvider kept, got %+v", after)
	}
}

func TestCompositeStorageSSHBypassAndTimeoutMarker(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "10.0.0.1 51234 10.0.0.2 22")
	dir := t.TempDir()
	cs := NewCLITokenStorage(dir, "test-cli", false)
	backend, reason := cs.ActiveBackendName()
	if backend != "file" || reason != "ssh" {
		t.Fatalf("expected file backend with ssh bypass, got (%s, %s)", backend, reason)
	}

	// Unset SSH envs to test 1h keyring timeout fallback marker
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_CLIENT", "")
	t.Setenv("SSH_TTY", "")
	marker := filepath.Join(dir, "cache", "marker-keyring-unavailable")
	cs2 := &CompositeTokenStorage{
		Keyring:    &KeyringTokenStorage{Service: "s", User: "u", Backend: &memKeyring{err: errors.New("timeout")}},
		File:       &FileTokenStorage{FilePath: filepath.Join(dir, "tok.json")},
		MarkerPath: marker,
	}
	if err := cs2.SaveToken(&OAuthToken{AccessToken: "fallback-tok"}, "consumer"); err != nil {
		t.Fatalf("SaveToken fallback failed: %v", err)
	}
	if !cs2.usingFile() {
		t.Fatalf("expected UseFileStatic=true after keyring error")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected timeout marker written at %s: %v", marker, err)
	}
	if !keyringRecentlyUnavailable(marker, time.Now()) {
		t.Fatalf("expected marker to be fresh within 1h")
	}
}

func TestChainedAuthPriorityAndRefresh(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SSH_CONNECTION", "1")
	t.Setenv("GEMINI_API_KEY", "AIzaTestKey9999")
	ca := NewChainedAuth(dir, "unit-cli", true)
	creds, err := ca.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if creds.Kind != AuthKindGeminiAPIKey || creds.APIKey != "AIzaTestKey9999" {
		t.Fatalf("unexpected creds: %+v", creds)
	}
	if masked := MaskSecret(creds.APIKey); masked != "AIza...9999" {
		t.Fatalf("unexpected MaskSecret output: %q", masked)
	}
}

func TestPKCEAndLoopbackExchange(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("code") != "auth-code-1" || r.Form.Get("code_verifier") == "" {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "ya29.fresh",
			"refresh_token": "1//ref",
			"expires_in":    3600,
			"token_type":    "Bearer",
		})
	}))
	defer ts.Close()

	cfg := NewConsumerOAuthConfig()
	cfg.TokenURL = ts.URL
	verifier := "test-verifier-value"
	if got := Sha256Base64URL(verifier); len(got) != 43 {
		t.Fatalf("expected 43-char S256 challenge, got %d (%q)", len(got), got)
	}
	authURL, err := cfg.BuildPKCEAuthURL("http://127.0.0.1:9999/auth/callback", "st-1", verifier)
	if err != nil || authURL == "" {
		t.Fatalf("BuildPKCEAuthURL failed: %v", err)
	}
	tok, err := cfg.ExchangeCode(context.Background(), ts.Client(), "auth-code-1", "http://127.0.0.1:9999/auth/callback", verifier)
	if err != nil || tok.AccessToken != "ya29.fresh" {
		t.Fatalf("ExchangeCode failed: %v (%+v)", err, tok)
	}
}
