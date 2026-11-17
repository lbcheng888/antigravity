package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

// AuthKind identifies the resolved credential mechanism in ChainedAuth.
type AuthKind string

const (
	AuthKindUnspecified  AuthKind = ""
	AuthKindGeminiAPIKey AuthKind = "gemini-api-key"
	AuthKindGateway      AuthKind = "gateway"
	AuthKindADC          AuthKind = "adc"
	AuthKindConsumerOAuth AuthKind = "consumer"
	AuthKindBusinessOAuth AuthKind = "business"
)

// Credentials holds the active resolved credentials without mutating system state.
type Credentials struct {
	Kind           AuthKind
	AccessToken    string
	APIKey         string
	GatewayURL     string
	Email          string
	ProjectID      string
	Region         string
	UserTier       string
	StorageBackend string
	BypassReason   BypassReason
	TokenFilePath  string
}

// ADCCredentials represents standard Google Cloud Application Default Credentials JSON.
type ADCCredentials struct {
	Type         string `json:"type"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
	QuotaProject string `json:"quota_project_id"`
}

// ChainedAuth mirrors Antigravity CLI's multi-stage ChainedAuth chain:
// GeminiAPIKey -> Gateway -> ADC -> StoredToken (Keychain or File via SSH/Marker bypass).
type ChainedAuth struct {
	Storage    *CompositeTokenStorage
	HTTPClient *http.Client
	ForceADC   bool

	mu          sync.Mutex
	activeCreds *Credentials
}

// NewChainedAuth initializes a ChainedAuth resolver for the given CLI appDataDir.
func NewChainedAuth(dataDir, appDataDir string, forceFile bool) *ChainedAuth {
	return &ChainedAuth{
		Storage:    NewCLITokenStorage(dataDir, appDataDir, forceFile),
		HTTPClient: &http.Client{},
		ForceADC:   os.Getenv("AGY_ADC_AUTH") == "1" || os.Getenv("AGY_ADC_AUTH") == "true",
	}
}

// Resolve inspects available auth sources in priority order and returns active credentials,
// refreshing an expired OAuth token via Google OAuth2 when a refresh_token is available.
func (ca *ChainedAuth) Resolve(ctx context.Context) (*Credentials, error) {
	backend, reason := ca.Storage.ActiveBackendName()
	filePath := ""
	if ca.Storage != nil && ca.Storage.File != nil {
		filePath = ca.Storage.File.FilePath
	}

	// 1. GEMINI_API_KEY
	if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		creds := &Credentials{
			Kind:           AuthKindGeminiAPIKey,
			APIKey:         key,
			StorageBackend: backend,
			BypassReason:   reason,
			TokenFilePath:  filePath,
		}
		ca.setActive(creds)
		return creds, nil
	}

	// 2. Enterprise / local Gateway URL
	if gw := os.Getenv("AGY_LLM_GATEWAY_URL"); gw != "" {
		creds := &Credentials{
			Kind:           AuthKindGateway,
			GatewayURL:     gw,
			APIKey:         os.Getenv("AGY_LLM_GATEWAY_API_KEY"),
			StorageBackend: backend,
			BypassReason:   reason,
			TokenFilePath:  filePath,
		}
		ca.setActive(creds)
		return creds, nil
	}

	// 3. Google Application Default Credentials when enabled
	if ca.ForceADC {
		adcPath := DefaultADCPath()
		if creds, err := ca.resolveADC(ctx, adcPath, backend, reason, filePath); err == nil {
			ca.setActive(creds)
			return creds, nil
		}
	}

	// 4. CompositeTokenStorage (Keyring primary or File when SSH/docker/marker bypassed)
	stored, err := ca.Storage.LoadStoredToken()
	if err != nil {
		return nil, err
	}
	if stored == nil || stored.Token == nil || (stored.Token.AccessToken == "" && stored.Token.RefreshToken == "") {
		return nil, errors.New("not authenticated: no stored credentials found")
	}

	tok := stored.Token
	kind := AuthKindConsumerOAuth
	oauthCfg := NewConsumerOAuthConfig()
	if stored.AuthMethod == string(AuthKindBusinessOAuth) || stored.AuthMethod == "enterprise" {
		kind = AuthKindBusinessOAuth
		oauthCfg = NewBusinessOAuthConfig()
	}

	if !tok.Valid() && tok.RefreshToken != "" {
		refreshed, rerr := oauthCfg.Refresh(ctx, ca.HTTPClient, tok.RefreshToken)
		if rerr != nil {
			return nil, fmt.Errorf("failed to refresh stored OAuth token: %w", rerr)
		}
		tok = refreshed
		_ = ca.Storage.SaveToken(tok, stored.AuthMethod)
	}

	creds := &Credentials{
		Kind:           kind,
		AccessToken:    tok.AccessToken,
		Email:          stored.IDTokenEmail,
		ProjectID:      stored.ProjectID,
		Region:         stored.Region,
		UserTier:       stored.UserTier,
		StorageBackend: backend,
		BypassReason:   reason,
		TokenFilePath:  filePath,
	}
	ca.setActive(creds)
	return creds, nil
}

// InspectStatus returns safe, redacted diagnostics without mutating tokens or keychains.
func (ca *ChainedAuth) InspectStatus() map[string]any {
	backend, reason := ca.Storage.ActiveBackendName()
	filePath := ""
	if ca.Storage != nil && ca.Storage.File != nil {
		filePath = ca.Storage.File.FilePath
	}
	info := map[string]any{
		"storage_backend":  backend,
		"bypass_reason":    string(reason),
		"token_file_path":  filePath,
		"gemini_api_key":   MaskSecret(os.Getenv("GEMINI_API_KEY")),
		"gateway_url":      os.Getenv("AGY_LLM_GATEWAY_URL"),
		"adc_enabled":      ca.ForceADC,
		"adc_file_present": false,
	}
	if _, err := os.Stat(DefaultADCPath()); err == nil {
		info["adc_file_present"] = true
	}
	if stored, err := ca.Storage.LoadStoredToken(); err == nil && stored != nil && !stored.IsEmpty() {
		info["auth_method"] = stored.AuthMethod
		info["email"] = stored.IDTokenEmail
		info["project_id"] = stored.ProjectID
		info["region"] = stored.Region
		info["user_tier"] = stored.UserTier
		info["tier_display_name"] = stored.TierDisplayName
		if stored.Token != nil {
			info["access_token_redacted"] = MaskSecret(stored.Token.AccessToken)
			info["has_refresh_token"] = stored.Token.RefreshToken != ""
			info["token_valid"] = stored.Token.Valid()
			if !stored.Token.Expiry.IsZero() {
				info["token_expiry"] = stored.Token.Expiry.Format("2006-01-02T15:04:05Z07:00")
			}
		}
	} else {
		info["stored_token_present"] = false
	}
	return info
}

func (ca *ChainedAuth) resolveADC(ctx context.Context, adcPath, backend string, reason BypassReason, filePath string) (*Credentials, error) {
	raw, err := os.ReadFile(adcPath)
	if err != nil {
		return nil, err
	}
	var adc ADCCredentials
	if err := json.Unmarshal(raw, &adc); err != nil {
		return nil, err
	}
	if adc.RefreshToken == "" || adc.ClientID == "" {
		return nil, errors.New("unsupported ADC credential format (missing refresh_token/client_id)")
	}
	cfg := OAuthConfig{
		ClientID:     adc.ClientID,
		ClientSecret: adc.ClientSecret,
		TokenURL:     GoogleTokenURL,
	}
	tok, err := cfg.Refresh(ctx, ca.HTTPClient, adc.RefreshToken)
	if err != nil {
		return nil, err
	}
	return &Credentials{
		Kind:           AuthKindADC,
		AccessToken:    tok.AccessToken,
		ProjectID:      adc.QuotaProject,
		StorageBackend: backend,
		BypassReason:   reason,
		TokenFilePath:  filePath,
	}, nil
}

// DefaultADCPath locates the standard Google Cloud ADC file.
func DefaultADCPath() string {
	if p := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); p != "" {
		return p
	}
	if cfgDir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(cfgDir, "gcloud", "application_default_credentials.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "gcloud", "application_default_credentials.json")
}

func (ca *ChainedAuth) setActive(creds *Credentials) {
	ca.mu.Lock()
	ca.activeCreds = creds
	ca.mu.Unlock()
}
