// Binary VA: 0x101c3a9b0 | Size: 7376 B (32 funcs) | Source: third_party/jetski/language_server/code_assist_client/token_storage.go
package codeassistclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring" /* inferred: keyring package */
	"golang.org/x/oauth2"
)

// Binary VA: 0x10a2fc0b0 | Size: 16 B | Type: codeassistclient.TokenStorage
type TokenStorage interface {
	LoadStoredToken() (*StoredToken, error)
	LoadToken() (*oauth2.Token, string, string, string, string, error)
	RemoveToken() error
	SaveProjectAndRegion(projectID, region string) error
	SaveToken(tok *oauth2.Token, authMethod string, opts ...SaveTokenOption) error
	SaveUserTier(userTier, displayName string) error
}

// Binary VA: 0x10a107978 | Size: 16 B | Type: codeassistclient.StoredTokenUpdater
type StoredTokenUpdater interface {
	UpdateStoredToken(fn func(*StoredToken)) error
}

// Binary VA: 0x10a2fc020 | Size: 16 B | Type: codeassistclient.SaveTokenConfig
type SaveTokenConfig struct {
	WIFProvider *string
	WriterID    *string
}

// Binary VA: 0x10a052938 | Size: 8 B | Type: codeassistclient.SaveTokenOption
type SaveTokenOption func(*SaveTokenConfig)

// Binary VA: 0x101c491c0 | Size: 96 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L43
func WithWIFProvider(provider string) SaveTokenOption {
	return func(cfg *SaveTokenConfig) {
		cfg.WIFProvider = &provider
	}
}

// Binary VA: 0x101c49220 | Size: 96 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L50
func WithWriterID(writerID string) SaveTokenOption {
	return func(cfg *SaveTokenConfig) {
		cfg.WriterID = &writerID
	}
}

// Binary VA: 0x10a8e3c88 | Size: 160 B | Type: codeassistclient.StoredToken
type StoredToken struct {
	Token                        *oauth2.Token `json:"token"`
	AuthMethod                   string        `json:"auth_method,omitempty"`
	IDTokenEmail                 string        `json:"id_token_email,omitempty"`
	WIFProvider                  string        `json:"wif_provider,omitempty"`
	SavedWIFProvider             string        `json:"saved_wif_provider,omitempty"`
	ProjectID                    string        `json:"project_id,omitempty"`
	Region                       string        `json:"region,omitempty"`
	UserTier                     string        `json:"user_tier,omitempty"`
	TierDisplayName              string        `json:"tier_display_name,omitempty"`
	WriterID                     string        `json:"writer_id,omitempty"`
	AICodeScopeSignOutAttemptedAt int64        `json:"aicode_scope_sign_out_attempted_at,omitempty"`
}

// Binary VA: 0x101c3a9b0 | Size: 96 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L99
func (s *StoredToken) IsEmpty() bool {
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

// Binary VA: 0x101c3aa10 | Size: 112 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L116
func (s *StoredToken) KeptOnSignOut() (StoredToken, bool) {
	if s == nil {
		return StoredToken{}, false
	}
	savedWIF := s.SavedWIFProvider
	if savedWIF == "" {
		savedWIF = s.WIFProvider
	}
	attemptedAt := s.AICodeScopeSignOutAttemptedAt
	if savedWIF == "" && attemptedAt == 0 {
		return StoredToken{}, false
	}
	return StoredToken{
		SavedWIFProvider:             savedWIF,
		AICodeScopeSignOutAttemptedAt: attemptedAt,
	}, true
}

// Binary VA: 0x101c3aa80 | Size: 208 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L144
func (s *StoredToken) RememberIDToken() {
	if s.Token == nil {
		return
	}
	rawIDToken, _ := s.Token.Extra("id_token").(string)
	if rawIDToken == "" {
		return
	}
	if email := parseIDToken(rawIDToken); email != "" {
		s.IDTokenEmail = email
	}
}

// Binary VA: 0x101c3ab50 | Size: 544 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L158
func (s *StoredToken) UnmarshalJSON(data []byte) error {
	type plain StoredToken
	var aux struct {
		plain
		LegacyIDToken string `json:"id_token,omitempty"`
	}
	err := json.Unmarshal(data, &aux)
	*s = StoredToken(aux.plain)
	if err != nil {
		return err
	}
	if s.IDTokenEmail == "" && aux.LegacyIDToken != "" {
		if email := parseIDToken(aux.LegacyIDToken); email != "" {
			s.IDTokenEmail = email
		}
	}
	return nil
}

// Binary VA: 0x101c3ad70 | Size: 416 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L182
func (s *StoredToken) ApplyToken(tok *oauth2.Token, authMethod string, cfg *SaveTokenConfig) {
	s.Token = tok
	s.AuthMethod = authMethod
	s.RememberIDToken()
	if authMethod == "consumer" {
		s.ProjectID = ""
		s.Region = ""
		s.UserTier = ""
		s.TierDisplayName = ""
	}
	if cfg.WIFProvider != nil {
		s.WIFProvider = *cfg.WIFProvider
		if *cfg.WIFProvider != "" {
			s.SavedWIFProvider = *cfg.WIFProvider
		}
	} else {
		s.WIFProvider = ""
	}
	if cfg.WriterID != nil {
		s.WriterID = *cfg.WriterID
	}
}

// Binary VA: 0x10a1ac020 | Size: 16 B | Type: codeassistclient.KeyringTokenStorage
type KeyringTokenStorage struct {
	user string
}

// Binary VA: 0x101c3af10 | Size: 400 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L221
func (k *KeyringTokenStorage) UpdateStoredToken(fn func(*StoredToken)) error {
	raw, err := keyring.Get("gemini", k.user)
	stored := &StoredToken{}
	if err == nil {
		_ = json.Unmarshal([]byte(raw), stored)
	}
	fn(stored)
	data, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	return keyring.Set("gemini", k.user, string(data))
}

// Binary VA: 0x101c3b0a0 | Size: 208 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L236
func (k *KeyringTokenStorage) SaveToken(tok *oauth2.Token, authMethod string, opts ...SaveTokenOption) error {
	cfg := &SaveTokenConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	// Binary VA: 0x101c4f140 | Size: 80 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L241
	return k.UpdateStoredToken(func(s *StoredToken) {
		s.ApplyToken(tok, authMethod, cfg)
	})
}

// Binary VA: 0x101c3b170 | Size: 624 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L247
func (k *KeyringTokenStorage) LoadStoredToken() (*StoredToken, error) {
	raw, err := keyring.Get("gemini", k.user)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return &StoredToken{}, nil
		}
		return nil, err
	}
	stored := &StoredToken{}
	if err := json.Unmarshal([]byte(raw), stored); err == nil && !stored.IsEmpty() {
		if stored.SavedWIFProvider == "" && stored.WIFProvider != "" {
			stored.SavedWIFProvider = stored.WIFProvider
		}
		return stored, nil
	}
	tok := &oauth2.Token{}
	if err := json.Unmarshal([]byte(raw), tok); err != nil {
		return nil, err
	}
	return &StoredToken{Token: tok}, nil
}

// Binary VA: 0x101c3b3e0 | Size: 160 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L272
func (k *KeyringTokenStorage) LoadToken() (*oauth2.Token, string, string, string, string, error) {
	stored, err := k.LoadStoredToken()
	if err != nil {
		return nil, "", "", "", "", err
	}
	return stored.Token, stored.ProjectID, stored.Region, stored.UserTier, stored.TierDisplayName, nil
}

// Binary VA: 0x101c3b480 | Size: 112 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L281
func (k *KeyringTokenStorage) SaveProjectAndRegion(projectID, region string) error {
	// Binary VA: 0x101c4f190 | Size: 112 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L282
	return k.UpdateStoredToken(func(s *StoredToken) {
		s.ProjectID = projectID
		s.Region = region
	})
}

// Binary VA: 0x101c3b4f0 | Size: 112 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L289
func (k *KeyringTokenStorage) SaveUserTier(userTier, displayName string) error {
	// Binary VA: 0x101c4f200 | Size: 112 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L290
	return k.UpdateStoredToken(func(s *StoredToken) {
		s.UserTier = userTier
		s.TierDisplayName = displayName
	})
}

// Binary VA: 0x101c3b560 | Size: 368 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L297
func (k *KeyringTokenStorage) RemoveToken() error {
	if stored, err := k.LoadStoredToken(); err == nil {
		if kept, ok := stored.KeptOnSignOut(); ok {
			// Binary VA: 0x101c4f270 | Size: 160 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L300
			return k.UpdateStoredToken(func(s *StoredToken) {
				*s = kept
			})
		}
	}
	err := keyring.Delete("gemini", k.user)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// Binary VA: 0x10a1ac200 | Size: 16 B | Type: codeassistclient.fileTokenStorage
type fileTokenStorage struct {
	filePath string
}

// Binary VA: 0x101c3b6d0 | Size: 336 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L318
func NewFileTokenStorage() (TokenStorage, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}
	return &fileTokenStorage{
		filePath: filepath.Join(home, ".gemini", "jetski-standalone-oauth-token"),
	}, nil
}

// Binary VA: 0x101c3b820 | Size: 656 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L334
func (f *fileTokenStorage) UpdateStoredToken(fn func(*StoredToken)) error {
	raw, err := os.ReadFile(f.filePath)
	stored := &StoredToken{}
	if err == nil {
		_ = json.Unmarshal(raw, stored)
	}
	fn(stored)
	data, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(f.filePath), 0o755); err != nil {
		return fmt.Errorf("failed to create token directory: %w", err)
	}
	if err := os.WriteFile(f.filePath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write token to file: %w", err)
	}
	return nil
}

// Binary VA: 0x101c3bab0 | Size: 208 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L358
func (f *fileTokenStorage) SaveToken(tok *oauth2.Token, authMethod string, opts ...SaveTokenOption) error {
	cfg := &SaveTokenConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	// Binary VA: 0x101c4f310 | Size: 80 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L363
	return f.UpdateStoredToken(func(s *StoredToken) {
		s.ApplyToken(tok, authMethod, cfg)
	})
}

// Binary VA: 0x101c3bb80 | Size: 784 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L369
func (f *fileTokenStorage) LoadStoredToken() (*StoredToken, error) {
	raw, err := os.ReadFile(f.filePath)
	if err != nil {
		if os.IsNotExist(err) {
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
	tok := &oauth2.Token{}
	if err := json.Unmarshal(raw, tok); err != nil {
		return nil, fmt.Errorf("failed to unmarshal token: %w", err)
	}
	return &StoredToken{Token: tok}, nil
}

// Binary VA: 0x101c3be90 | Size: 160 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L393
func (f *fileTokenStorage) LoadToken() (*oauth2.Token, string, string, string, string, error) {
	stored, err := f.LoadStoredToken()
	if err != nil {
		return nil, "", "", "", "", err
	}
	return stored.Token, stored.ProjectID, stored.Region, stored.UserTier, stored.TierDisplayName, nil
}

// Binary VA: 0x101c3bf30 | Size: 112 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L402
func (f *fileTokenStorage) SaveProjectAndRegion(projectID, region string) error {
	// Binary VA: 0x101c4f360 | Size: 112 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L403
	return f.UpdateStoredToken(func(s *StoredToken) {
		s.ProjectID = projectID
		s.Region = region
	})
}

// Binary VA: 0x101c3bfa0 | Size: 112 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L410
func (f *fileTokenStorage) SaveUserTier(userTier, displayName string) error {
	// Binary VA: 0x101c4f3d0 | Size: 112 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L411
	return f.UpdateStoredToken(func(s *StoredToken) {
		s.UserTier = userTier
		s.TierDisplayName = displayName
	})
}

// Binary VA: 0x101c3c010 | Size: 336 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L418
func (f *fileTokenStorage) RemoveToken() error {
	if stored, err := f.LoadStoredToken(); err == nil {
		if kept, ok := stored.KeptOnSignOut(); ok {
			// Binary VA: 0x101c4f440 | Size: 160 B | Source: third_party/jetski/language_server/code_assist_client/token_storage.go:L421
			return f.UpdateStoredToken(func(s *StoredToken) {
				*s = kept
			})
		}
	}
	err := os.Remove(f.filePath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
