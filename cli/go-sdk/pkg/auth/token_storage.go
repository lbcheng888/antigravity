package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultKeyringService and DefaultKeyringUser match Antigravity's native macOS keychain entry.
	DefaultKeyringService = "antigravity"
	DefaultKeyringUser    = "gemini"

	keyringTimeout       = 5 * time.Second
	keyringUnavailableTT = 1 * time.Hour
	envDetectTimeout     = 500 * time.Millisecond
)

// OAuthToken mirrors golang.org/x/oauth2.Token JSON layout using pure Go stdlib.
type OAuthToken struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Expiry       time.Time `json:"expiry,omitempty"`
	ExpiresIn    int64     `json:"expires_in,omitempty"`
	IDToken      string    `json:"id_token,omitempty"`
}

// Valid reports whether the token carries a non-empty access token that has not expired (with 10s skew).
func (t *OAuthToken) Valid() bool {
	if t == nil || t.AccessToken == "" {
		return false
	}
	if !t.Expiry.IsZero() && time.Now().Add(10*time.Second).After(t.Expiry) {
		return false
	}
	return true
}

// StoredToken matches Antigravity's persisted StoredToken schema.
type StoredToken struct {
	Token                         *OAuthToken `json:"token"`
	AuthMethod                    string      `json:"auth_method,omitempty"`
	IDTokenEmail                  string      `json:"id_token_email,omitempty"`
	WIFProvider                   string      `json:"wif_provider,omitempty"`
	SavedWIFProvider              string      `json:"saved_wif_provider,omitempty"`
	ProjectID                     string      `json:"project_id,omitempty"`
	Region                        string      `json:"region,omitempty"`
	UserTier                      string      `json:"user_tier,omitempty"`
	TierDisplayName               string      `json:"tier_display_name,omitempty"`
	WriterID                      string      `json:"writer_id,omitempty"`
	AICodeScopeSignOutAttemptedAt int64       `json:"aicode_scope_sign_out_attempted_at,omitempty"`
}

// IsEmpty returns true if no identity, token, or metadata fields are populated.
func (s *StoredToken) IsEmpty() bool {
	if s == nil {
		return true
	}
	return s.Token == nil &&
		s.IDTokenEmail == "" &&
		s.WIFProvider == "" &&
		s.SavedWIFProvider == "" &&
		s.ProjectID == "" &&
		s.Region == "" &&
		s.UserTier == "" &&
		s.TierDisplayName == "" &&
		s.WriterID == "" &&
		s.AICodeScopeSignOutAttemptedAt == 0
}

// KeptOnSignOut preserves fields allowed to survive an explicit sign-out.
func (s *StoredToken) KeptOnSignOut() (StoredToken, bool) {
	if s == nil {
		return StoredToken{}, false
	}
	savedWIF := s.SavedWIFProvider
	if savedWIF == "" {
		savedWIF = s.WIFProvider
	}
	if savedWIF == "" && s.AICodeScopeSignOutAttemptedAt == 0 {
		return StoredToken{}, false
	}
	return StoredToken{
		SavedWIFProvider:              savedWIF,
		AICodeScopeSignOutAttemptedAt: s.AICodeScopeSignOutAttemptedAt,
	}, true
}

// RememberIDToken extracts the email claim from a JWT ID token when present.
func (s *StoredToken) RememberIDToken() {
	if s == nil || s.Token == nil || s.Token.IDToken == "" {
		return
	}
	if email := parseJWTClaimEmail(s.Token.IDToken); email != "" {
		s.IDTokenEmail = email
	}
}

func parseJWTClaimEmail(jwt string) string {
	parts := strings.Split(jwt, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return claims.Email
}

// MaskSecret redacts sensitive credentials for safe logging/status display.
func MaskSecret(secret string) string {
	if secret == "" {
		return ""
	}
	if len(secret) <= 8 {
		return "***"
	}
	return secret[:4] + "..." + secret[len(secret)-4:]
}

// SaveTokenOption configures optional fields when saving a token.
type SaveTokenOption func(*SaveTokenConfig)

// SaveTokenConfig holds optional metadata for SaveToken.
type SaveTokenConfig struct {
	WIFProvider *string
	WriterID    *string
}

// WithWIFProvider associates a Workforce Identity Federation provider with the token.
func WithWIFProvider(provider string) SaveTokenOption {
	return func(c *SaveTokenConfig) { c.WIFProvider = &provider }
}

// WithWriterID records which writer stored the token.
func WithWriterID(writerID string) SaveTokenOption {
	return func(c *SaveTokenConfig) { c.WriterID = &writerID }
}

// TokenStorage abstracts Antigravity token persistence across Keychain and file storage.
type TokenStorage interface {
	LoadStoredToken() (*StoredToken, error)
	SaveToken(tok *OAuthToken, authMethod string, opts ...SaveTokenOption) error
	SaveProjectAndRegion(projectID, region string) error
	SaveUserTier(userTier, displayName string) error
	RemoveToken() error
}

// FileTokenStorage stores tokens as 0600 JSON at `<dataDir>/<appDataDir>-oauth-token`.
type FileTokenStorage struct {
	FilePath string
}

// LoadStoredToken reads and parses the stored JSON token file.
func (f *FileTokenStorage) LoadStoredToken() (*StoredToken, error) {
	raw, err := os.ReadFile(f.FilePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &StoredToken{}, nil
		}
		return nil, fmt.Errorf("failed to read token from file: %w", err)
	}
	stored := &StoredToken{}
	if err := json.Unmarshal(raw, stored); err == nil && !stored.IsEmpty() {
		if stored.SavedWIFProvider == "" && stored.WIFProvider != "" {
			stored.SavedWIFProvider = stored.WIFProvider
		}
		return stored, nil
	}
	var legacy OAuthToken
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return nil, fmt.Errorf("failed to unmarshal token: %w", err)
	}
	return &StoredToken{Token: &legacy}, nil
}

func (f *FileTokenStorage) updateStoredToken(fn func(*StoredToken)) error {
	stored, err := f.LoadStoredToken()
	if err != nil {
		stored = &StoredToken{}
	}
	fn(stored)
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(f.FilePath), 0o755); err != nil {
		return fmt.Errorf("failed to create directory for token: %w", err)
	}
	tmp := f.FilePath + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("failed to write token to file: %w", err)
	}
	return os.Rename(tmp, f.FilePath)
}

// SaveToken persists the OAuth token and auth method into the JSON token file.
func (f *FileTokenStorage) SaveToken(tok *OAuthToken, authMethod string, opts ...SaveTokenOption) error {
	cfg := &SaveTokenConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	return f.updateStoredToken(func(s *StoredToken) {
		s.Token = tok
		s.AuthMethod = authMethod
		s.RememberIDToken()
		if cfg.WIFProvider != nil {
			s.WIFProvider = *cfg.WIFProvider
			if *cfg.WIFProvider != "" {
				s.SavedWIFProvider = *cfg.WIFProvider
			}
		}
		if cfg.WriterID != nil {
			s.WriterID = *cfg.WriterID
		}
	})
}

// SaveProjectAndRegion updates project_id and region metadata.
func (f *FileTokenStorage) SaveProjectAndRegion(projectID, region string) error {
	return f.updateStoredToken(func(s *StoredToken) {
		s.ProjectID = projectID
		s.Region = region
	})
}

// SaveUserTier updates user_tier and tier_display_name metadata.
func (f *FileTokenStorage) SaveUserTier(userTier, displayName string) error {
	return f.updateStoredToken(func(s *StoredToken) {
		s.UserTier = userTier
		s.TierDisplayName = displayName
	})
}

// RemoveToken clears active credentials while retaining KeptOnSignOut fields if any.
func (f *FileTokenStorage) RemoveToken() error {
	cur, err := f.LoadStoredToken()
	if err == nil {
		if kept, ok := cur.KeptOnSignOut(); ok {
			return f.updateStoredToken(func(s *StoredToken) {
				*s = kept
			})
		}
	}
	if err := os.Remove(f.FilePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// KeyringBackend abstracts OS credential storage (defaulting to macOS `security` CLI).
type KeyringBackend interface {
	Get(service, user string) (string, error)
	Set(service, user, secret string) error
	Delete(service, user string) error
}

// MacOSKeyringBackend accesses macOS Keychain via `/usr/bin/security` without CGO.
type MacOSKeyringBackend struct{}

// Get reads the generic password secret for (service, user) from macOS Keychain.
func (MacOSKeyringBackend) Get(service, user string) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", errors.New("macOS keychain only supported on darwin")
	}
	ctx, cancel := context.WithTimeout(context.Background(), keyringTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-s", service, "-a", user, "-w")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Set writes or updates the generic password secret in macOS Keychain.
func (MacOSKeyringBackend) Set(service, user, secret string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("macOS keychain only supported on darwin")
	}
	ctx, cancel := context.WithTimeout(context.Background(), keyringTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/security", "add-generic-password", "-U", "-s", service, "-a", user, "-w", secret)
	return cmd.Run()
}

// Delete removes the generic password entry from macOS Keychain.
func (MacOSKeyringBackend) Delete(service, user string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("macOS keychain only supported on darwin")
	}
	ctx, cancel := context.WithTimeout(context.Background(), keyringTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/security", "delete-generic-password", "-s", service, "-a", user)
	return cmd.Run()
}

// KeyringTokenStorage reads/writes StoredToken JSON inside OS Keychain.
type KeyringTokenStorage struct {
	Service string
	User    string
	Backend KeyringBackend
}

// LoadStoredToken reads and decodes StoredToken from OS Keychain.
func (k *KeyringTokenStorage) LoadStoredToken() (*StoredToken, error) {
	if k == nil || k.Backend == nil {
		return &StoredToken{}, nil
	}
	raw, err := k.Backend.Get(k.Service, k.User)
	if err != nil {
		return &StoredToken{}, err
	}
	if strings.TrimSpace(raw) == "" {
		return &StoredToken{}, nil
	}
	// Native Antigravity stores either raw JSON or base64/go-keyring prefixed values.
	if strings.HasPrefix(raw, "go-keyring-base64:") {
		dec, derr := base64.StdEncoding.DecodeString(strings.TrimPrefix(raw, "go-keyring-base64:"))
		if derr == nil {
			raw = string(dec)
		}
	}
	stored := &StoredToken{}
	if err := json.Unmarshal([]byte(raw), stored); err == nil && !stored.IsEmpty() {
		return stored, nil
	}
	var legacy OAuthToken
	if err := json.Unmarshal([]byte(raw), &legacy); err == nil {
		return &StoredToken{Token: &legacy}, nil
	}
	return &StoredToken{}, fmt.Errorf("failed to parse keychain token JSON")
}

// SaveToken persists StoredToken JSON into OS Keychain.
func (k *KeyringTokenStorage) SaveToken(tok *OAuthToken, authMethod string, opts ...SaveTokenOption) error {
	if k == nil || k.Backend == nil {
		return errors.New("keyring backend unavailable")
	}
	cur, err := k.LoadStoredToken()
	if err != nil {
		cur = &StoredToken{}
	}
	cfg := &SaveTokenConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	cur.Token = tok
	cur.AuthMethod = authMethod
	cur.RememberIDToken()
	if cfg.WIFProvider != nil {
		cur.WIFProvider = *cfg.WIFProvider
		if *cfg.WIFProvider != "" {
			cur.SavedWIFProvider = *cfg.WIFProvider
		}
	}
	if cfg.WriterID != nil {
		cur.WriterID = *cfg.WriterID
	}
	raw, err := json.Marshal(cur)
	if err != nil {
		return err
	}
	return k.Backend.Set(k.Service, k.User, string(raw))
}

// SaveProjectAndRegion updates project/region in Keychain.
func (k *KeyringTokenStorage) SaveProjectAndRegion(projectID, region string) error {
	cur, err := k.LoadStoredToken()
	if err != nil {
		return err
	}
	cur.ProjectID = projectID
	cur.Region = region
	raw, err := json.Marshal(cur)
	if err != nil {
		return err
	}
	return k.Backend.Set(k.Service, k.User, string(raw))
}

// SaveUserTier updates user tier in Keychain.
func (k *KeyringTokenStorage) SaveUserTier(userTier, displayName string) error {
	cur, err := k.LoadStoredToken()
	if err != nil {
		return err
	}
	cur.UserTier = userTier
	cur.TierDisplayName = displayName
	raw, err := json.Marshal(cur)
	if err != nil {
		return err
	}
	return k.Backend.Set(k.Service, k.User, string(raw))
}

// RemoveToken deletes the token from OS Keychain.
func (k *KeyringTokenStorage) RemoveToken() error {
	if k == nil || k.Backend == nil {
		return nil
	}
	return k.Backend.Delete(k.Service, k.User)
}

// BypassReason explains why Keychain was bypassed in favor of file storage.
type BypassReason string

// DetectEnvironmentBypass mirrors codeassistclient.shouldBypassKeyring across
// SSH, Docker/container, WSL, and Android detectors.
func DetectEnvironmentBypass() (bool, BypassReason) {
	if os.Getenv("SSH_TTY") != "" || os.Getenv("SSH_CLIENT") != "" || os.Getenv("SSH_CONNECTION") != "" {
		return true, "ssh"
	}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true, "container"
	}
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		return true, "wsl"
	}
	if runtime.GOOS == "android" {
		return true, "android"
	}
	return false, ""
}

// CompositeTokenStorage wraps Keychain primary storage and File fallback storage
// with SSH/container bypass and a 1-hour `-keyring-unavailable` failure marker.
type CompositeTokenStorage struct {
	Keyring       TokenStorage
	File          *FileTokenStorage
	MarkerPath    string
	UseFileStatic bool
	BypassReason  BypassReason
	mu            sync.RWMutex
}

// NewCLITokenStorage builds Antigravity CLI's default CompositeTokenStorage layout.
func NewCLITokenStorage(dataDir, appDataDir string, forceFile bool) *CompositeTokenStorage {
	if appDataDir == "" {
		appDataDir = "antigravity-cli"
	}
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".gemini", appDataDir)
	}
	tokenFile := &FileTokenStorage{
		FilePath: filepath.Join(dataDir, appDataDir+"-oauth-token"),
	}
	markerPath := filepath.Join(dataDir, "cache", appDataDir+"-keyring-unavailable")
	useFile := forceFile
	var reason BypassReason
	if forceFile {
		reason = "force-file"
	} else if bypassed, r := DetectEnvironmentBypass(); bypassed {
		useFile = true
		reason = r
	} else if keyringRecentlyUnavailable(markerPath, time.Now()) {
		useFile = true
		reason = "recent-keyring-timeout"
	}
	return &CompositeTokenStorage{
		Keyring: &KeyringTokenStorage{
			Service: DefaultKeyringService,
			User:    DefaultKeyringUser,
			Backend: MacOSKeyringBackend{},
		},
		File:          tokenFile,
		MarkerPath:    markerPath,
		UseFileStatic: useFile,
		BypassReason:  reason,
	}
}

func keyringRecentlyUnavailable(markerPath string, now time.Time) bool {
	if markerPath == "" {
		return false
	}
	st, err := os.Stat(markerPath)
	if err != nil {
		return false
	}
	return now.Sub(st.ModTime()) < keyringUnavailableTT
}

func (c *CompositeTokenStorage) memoizeKeyringTimeout() {
	if c.MarkerPath == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(c.MarkerPath), 0o755)
	tmp := fmt.Sprintf("%s.tmp.%d", c.MarkerPath, time.Now().UnixNano())
	_ = os.WriteFile(tmp, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600)
	_ = os.Rename(tmp, c.MarkerPath)
	c.mu.Lock()
	c.UseFileStatic = true
	c.BypassReason = "keyring-timeout"
	c.mu.Unlock()
}

func (c *CompositeTokenStorage) usingFile() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.UseFileStatic || c.Keyring == nil
}

// ActiveBackendName returns "file" or "keyring" along with any bypass reason.
func (c *CompositeTokenStorage) ActiveBackendName() (string, BypassReason) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.UseFileStatic || c.Keyring == nil {
		return "file", c.BypassReason
	}
	return "keyring", ""
}

// LoadStoredToken loads credentials from File (when bypassed) or Keychain with File fallback.
func (c *CompositeTokenStorage) LoadStoredToken() (*StoredToken, error) {
	if c.usingFile() {
		return c.File.LoadStoredToken()
	}
	fileTok, fErr := c.File.LoadStoredToken()
	if fErr == nil && fileTok != nil && !fileTok.IsEmpty() {
		// Check Keychain first; heal if file holds newer active token.
		krTok, kErr := c.Keyring.LoadStoredToken()
		if kErr == nil && krTok != nil && !krTok.IsEmpty() {
			return krTok, nil
		}
		return fileTok, nil
	}
	krTok, kErr := c.Keyring.LoadStoredToken()
	if kErr != nil {
		c.memoizeKeyringTimeout()
		return c.File.LoadStoredToken()
	}
	return krTok, nil
}

// SaveToken persists the token to the active backend, falling back to File on Keychain failure.
func (c *CompositeTokenStorage) SaveToken(tok *OAuthToken, authMethod string, opts ...SaveTokenOption) error {
	if c.usingFile() {
		return c.File.SaveToken(tok, authMethod, opts...)
	}
	if err := c.Keyring.SaveToken(tok, authMethod, opts...); err != nil {
		c.memoizeKeyringTimeout()
		return c.File.SaveToken(tok, authMethod, opts...)
	}
	return nil
}

// SaveProjectAndRegion saves project and region to the active backend.
func (c *CompositeTokenStorage) SaveProjectAndRegion(projectID, region string) error {
	if c.usingFile() {
		return c.File.SaveProjectAndRegion(projectID, region)
	}
	if err := c.Keyring.SaveProjectAndRegion(projectID, region); err != nil {
		return c.File.SaveProjectAndRegion(projectID, region)
	}
	return nil
}

// SaveUserTier saves user tier to the active backend.
func (c *CompositeTokenStorage) SaveUserTier(userTier, displayName string) error {
	if c.usingFile() {
		return c.File.SaveUserTier(userTier, displayName)
	}
	if err := c.Keyring.SaveUserTier(userTier, displayName); err != nil {
		return c.File.SaveUserTier(userTier, displayName)
	}
	return nil
}

// RemoveToken deletes active credentials from File and (unless bypassed) Keychain.
func (c *CompositeTokenStorage) RemoveToken() error {
	errFile := c.File.RemoveToken()
	if c.usingFile() {
		return errFile
	}
	_ = c.Keyring.RemoveToken()
	return errFile
}

// Sha256Base64URL computes S256 PKCE code_challenge from a verifier string.
func Sha256Base64URL(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
