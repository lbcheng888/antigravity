// Binary VA: 0x101d433a0 | Size: 4432 B (12 funcs) | Source: third_party/jetski/cli/backend/auth/token_storage.go
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google3/third_party/jetski/cli/analytics/analytics"
	"google3/third_party/jetski/language_server/code_assist_client/codeassistclient"
)

// Binary VA: 0x10a1adda8 | Size: 16 B | Type: auth.cliFileTokenStorage
type cliFileTokenStorage struct {
	filePath string
}

// Binary VA: 0x10a0bf978 | Size: 0 B | Type: auth.adcTokenStorage
type adcTokenStorage struct{}

// Binary VA: 0x101d433a0 | Size: 496 B | Source: third_party/jetski/cli/backend/auth/token_storage.go:L60
func NewCLITokenStorage(user, dataDir, appDataDir string, forceFile bool) codeassistclient.TokenStorage {
	keyringStorage := &codeassistclient.KeyringTokenStorage{
		/* inferred: unexported field user set */
	}
	_ = user
	fileStorage := &cliFileTokenStorage{
		filePath: filepath.Join(dataDir, appDataDir+"-oauth-token"),
	}
	markerPath := filepath.Join(dataDir, "cache", appDataDir+"-keyring-unavailable")
	return codeassistclient.NewCompositeTokenStorage(
		keyringStorage,
		fileStorage,
		forceFile,
		markerPath,
		analytics.RecordKeyringError,
	)
}

// Binary VA: 0x101d43590 | Size: 656 B | Source: third_party/jetski/cli/backend/auth/token_storage.go:L83
func (c *cliFileTokenStorage) updateStoredToken(fn func(*codeassistclient.StoredToken)) error {
	raw, err := os.ReadFile(c.filePath)
	stored := &codeassistclient.StoredToken{}
	if err == nil {
		_ = json.Unmarshal(raw, stored)
	}
	fn(stored)
	data, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(c.filePath), 0o755); err != nil {
		return fmt.Errorf("failed to create directory for token: %w", err)
	}
	if err := os.WriteFile(c.filePath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write token to file: %w", err)
	}
	return nil
}

// Binary VA: 0x101d43820 | Size: 208 B | Source: third_party/jetski/cli/backend/auth/token_storage.go:L104
func (c *cliFileTokenStorage) SaveToken(tok *oauth2.Token, authMethod string, opts ...codeassistclient.SaveTokenOption) error {
	cfg := &codeassistclient.SaveTokenConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	// Binary VA: 0x101d46d50 | Size: 224 B | Source: third_party/jetski/cli/backend/auth/token_storage.go:L109
	return c.updateStoredToken(func(s *codeassistclient.StoredToken) {
		s.Token = tok
		s.AuthMethod = authMethod
		s.RememberIDToken()
		if cfg.WIFProvider != nil {
			s.WIFProvider = *cfg.WIFProvider
			if *cfg.WIFProvider != "" {
				s.SavedWIFProvider = *cfg.WIFProvider
			}
		}
	})
}

// Binary VA: 0x101d438f0 | Size: 544 B | Source: third_party/jetski/cli/backend/auth/token_storage.go:L125
func (*adcTokenStorage) LoadCredentials() (*google.Credentials, error) {
	var scopes []string
	for _, s := range defaultScopes /* inferred: global slice at 0x10d3d8c70 */ {
		if s == "https://www.googleapis.com/auth/aicode" {
			continue
		}
		scopes = append(scopes, s)
	}
	return google.FindDefaultCredentialsWithParams(context.Background(), google.CredentialsParams{
		Scopes: scopes,
	})
}

// Binary VA: 0x101d43b10 | Size: 784 B | Source: third_party/jetski/cli/backend/auth/token_storage.go:L129
func (c *cliFileTokenStorage) LoadStoredToken() (*codeassistclient.StoredToken, error) {
	raw, err := os.ReadFile(c.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &codeassistclient.StoredToken{}, nil
		}
		return nil, fmt.Errorf("failed to read token from file: %w", err)
	}
	stored := &codeassistclient.StoredToken{}
	if err := json.Unmarshal(raw, stored); err == nil && !stored.IsEmpty() {
		if stored.SavedWIFProvider == "" && stored.WIFProvider != "" {
			stored.SavedWIFProvider = stored.WIFProvider
		}
		return stored, nil
	}
	tok := &oauth2.Token{}
	if err := json.Unmarshal(raw, tok); err != nil {
		return nil, fmt.Errorf("failed to unmarshal token: %w", err)
	}
	return &codeassistclient.StoredToken{Token: tok}, nil
}

// Binary VA: 0x101d43e20 | Size: 160 B | Source: third_party/jetski/cli/backend/auth/token_storage.go:L152
func (c *cliFileTokenStorage) LoadToken() (*oauth2.Token, string, string, string, string, error) {
	stored, err := c.LoadStoredToken()
	if err != nil {
		return nil, "", "", "", "", err
	}
	return stored.Token, stored.ProjectID, stored.Region, stored.UserTier, stored.TierDisplayName, nil
}

// Binary VA: 0x101d43ec0 | Size: 752 B | Source: third_party/jetski/cli/backend/auth/token_storage.go:L160
func (c *cliFileTokenStorage) SaveUserTier(userTier, displayName string) error {
	raw, _ := os.ReadFile(c.filePath)
	stored := &codeassistclient.StoredToken{}
	_ = json.Unmarshal(raw, stored)
	stored.UserTier = userTier
	stored.TierDisplayName = displayName
	data, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(c.filePath), 0o755); err != nil {
		return fmt.Errorf("failed to create directory for token: %w", err)
	}
	if err := os.WriteFile(c.filePath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write token to file: %w", err)
	}
	return nil
}

// Binary VA: 0x101d441b0 | Size: 208 B | Source: third_party/jetski/cli/backend/auth/token_storage.go:L181
func (c *cliFileTokenStorage) RemoveToken() error {
	if stored, err := c.LoadStoredToken(); err == nil && stored != nil {
		savedWIF := stored.SavedWIFProvider
		if savedWIF == "" {
			savedWIF = stored.WIFProvider
		}
		if savedWIF != "" || stored.AICodeScopeSignOutAttemptedAt != 0 {
			// Binary VA: 0x101d46e30 | Size: 176 B | Source: third_party/jetski/cli/backend/auth/token_storage.go:L187
			return c.updateStoredToken(func(s *codeassistclient.StoredToken) {
				*s = codeassistclient.StoredToken{
					SavedWIFProvider:             savedWIF,
					AICodeScopeSignOutAttemptedAt: stored.AICodeScopeSignOutAttemptedAt,
				}
			})
		}
	}
	err := os.Remove(c.filePath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Binary VA: 0x101d44280 | Size: 112 B | Source: third_party/jetski/cli/backend/auth/token_storage.go:L198
func (c *cliFileTokenStorage) SaveProjectAndRegion(projectID, region string) error {
	// Binary VA: 0x101d46ee0 | Size: 112 B | Source: third_party/jetski/cli/backend/auth/token_storage.go:L199
	return c.updateStoredToken(func(s *codeassistclient.StoredToken) {
		s.ProjectID = projectID
		s.Region = region
	})
}
