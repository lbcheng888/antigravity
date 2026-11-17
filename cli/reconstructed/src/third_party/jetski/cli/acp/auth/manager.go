// Binary VA: 0x1023354f0 | Size: 33920 B (approx, 28 funcs) | Source: third_party/jetski/cli/acp/auth/manager.go
package auth

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"google3/base/go/log"
	"google3/third_party/golang/oauth2/google/google"
	"google3/third_party/golang/oauth2/oauth2"
	"google3/third_party/jetski/cli/acp/schema"
	"google3/third_party/jetski/cli/env/env"
	"google3/third_party/jetski/gateway/gateway"
	"google3/third_party/jetski/language_server/code_assist_client/codeassistclient"
	"google3/third_party/jetski/language_server_pb/language_server_go_proto"
)

// Binary VA: 0x10a1b1a98 | Size: 16 B | Source: third_party/jetski/cli/acp/auth/manager.go
type Backend interface {
	Logout() error
	RefreshExperiments(context.Context) error
	TrySilentAuth() bool
}

// Binary VA: 0x109fb2a88 | Size: 16 B | Source: third_party/jetski/cli/acp/auth/manager.go
type BackendMode string

const (
	backendModeCCPA      BackendMode = "ccpa"      /* inferred */
	backendModeGateway   BackendMode = "gateway"   /* inferred */
	backendModeADC       BackendMode = "adc"       /* inferred */
	backendModeGeminiAPI BackendMode = "gemini-api" /* inferred */
)

// Binary VA: 0x10a842678 | Size: 96 B | Source: third_party/jetski/cli/acp/auth/manager.go
type BackendSelection struct {
	Mode                BackendMode
	MethodID            string
	UseGeminiAPIKeyAuth bool
	UseSnapshotMendelFP bool
	DisableTelemetry    bool
	GeminiAPIKey        string
	Vertex              vertexConfig
	Gateway             *GatewayConfig
}

// Binary VA: 0x10a109ac8 | Size: 16 B | Source: third_party/jetski/cli/acp/auth/manager.go
type settingsReader interface {
	ReadCliSettings(context.Context) ([]byte, error)
}

// Binary VA: 0x10a1b1b10 | Size: 16 B | Source: third_party/jetski/cli/acp/auth/manager.go
type licenseBackend interface {
	FetchLicenses(context.Context) ([]*language_server_go_proto.License, error)
	SelectLicense(context.Context, string, string, string) error
	SelfAssignLicense(context.Context, string, string) (*language_server_go_proto.License, error)
}

// Binary VA: 0x10a9acfa8 | Size: 208 B | Source: third_party/jetski/cli/acp/auth/manager.go
type Config struct {
	Backend                     Backend
	IsJetski                    bool
	ProductName                 string
	AppDataDir                  string
	CLIDirName                  string
	SettingsPath                string
	AdminSettingsPath           string
	DisableDefaultAdminSettings bool
	GatewayEnabled              bool
	BackendMode                 BackendMode
	CLIAuth                     *codeassistclient.CLIAuthProvider
	CodeAssistClient            *codeassistclient.CodeAssistClient
	ConsumerBrowserLogin        func(context.Context) error
	BusinessBrowserLogin        func(context.Context) (*oauth2.Token, error)
	TokenStore                  codeassistclient.TokenStorage
	TierFetcher                 func(context.Context, string) TierInfo
	ADCLoader                   func(context.Context, ...string) (*google.Credentials, error)
	OnBackendModeChange         func(context.Context, BackendSelection) (Backend, error)
	LoginTimeout                time.Duration
}

// Binary VA: 0x10a9f9df0 | Size: 496 B | Source: third_party/jetski/cli/acp/auth/manager.go
type Manager struct {
	cfg                   Config
	loginTimeout          time.Duration
	backendMu             sync.Mutex
	be                    Backend
	backendMode           BackendMode
	cliAuth               *codeassistclient.CLIAuthProvider
	codeAssistClient      *codeassistclient.CodeAssistClient
	tokenStore            codeassistclient.TokenStorage
	consumerLogin         func(context.Context) error
	businessLogin         func(context.Context) (*oauth2.Token, error)
	tierFetcher           func(context.Context, string) TierInfo
	adcLoader             func(context.Context, ...string) (*google.Credentials, error)
	inBandGeminiKeyActive bool
	origGeminiKeyPresent  bool
	origGeminiKeyValue    string
	mu                    sync.Mutex
	authenticated         bool
	loggedOut             bool
	activeMethod          string
	activeGeminiKey       string
	activeVertex          vertexConfig
	gatewayConfig         *GatewayConfig
	tierInfo              TierInfo
	cancelLogin           context.CancelFunc
	browserLogins         int
}

// Binary VA: 0x1023354f0 | Size: 1344 B | Source: third_party/jetski/cli/acp/auth/manager.go:L131
func NewManager(cfg Config) *Manager {
	loginTimeout := cfg.LoginTimeout
	if loginTimeout <= 0 {
		loginTimeout = 5 * time.Minute // 0x45d964b800 ns
	}

	backendMode := cfg.BackendMode
	if backendMode == "" {
		backendMode = "ccpa"
	}

	tokenStore := cfg.TokenStore
	if tokenStore == nil && cfg.ProductName != "" && filepath.IsAbs(cfg.AppDataDir) {
		if ts, err := NewBackendTokenStore(cfg.ProductName, cfg.IsJetski); err == nil {
			tokenStore = ts
		}
	}

	var logins []*loopbackLogin
	consumerLogin := cfg.ConsumerBrowserLogin
	if consumerLogin == nil && cfg.ProductName != "" && filepath.IsAbs(cfg.AppDataDir) {
		login := newBrowserLogin(cfg.ProductName, cfg.IsJetski)
		logins = append(logins, login)
		consumerLogin = login.Login
	}

	businessLogin := cfg.BusinessBrowserLogin
	if businessLogin == nil && cfg.ProductName != "" && filepath.IsAbs(cfg.AppDataDir) {
		login := newBusinessBrowserLogin(cfg.ProductName, cfg.IsJetski)
		logins = append(logins, login)
		businessLogin = login.LoginToken
	}

	tierFetcher := cfg.TierFetcher
	if tierFetcher == nil && (cfg.CodeAssistClient != nil || cfg.CLIAuth != nil) {
		tierFetcher = newCodeAssistTierFetcher(cfg.CodeAssistClient, cfg.CLIAuth)
	}

	adcLoader := cfg.ADCLoader
	if adcLoader == nil && cfg.CLIAuth != nil {
		adcLoader = google.FindDefaultCredentials
	}

	m := &Manager{
		cfg:              cfg,
		loginTimeout:     loginTimeout,
		be:               cfg.Backend,
		backendMode:      backendMode,
		cliAuth:          cfg.CLIAuth,
		codeAssistClient: cfg.CodeAssistClient,
		tokenStore:       tokenStore,
		consumerLogin:    consumerLogin,
		businessLogin:    businessLogin,
		tierFetcher:      tierFetcher,
		adcLoader:        adcLoader,
	}
	for _, login := range logins {
		login.persist = m.withBackendLock
	}
	return m
}

// Binary VA: 0x102335a30 | Size: 736 B | Source: third_party/jetski/cli/acp/auth/manager.go:L197
func (m *Manager) waitForBrowser(ctx context.Context, fn func(context.Context) error) error {
	m.mu.Lock()
	m.browserLogins++
	m.mu.Unlock()

	m.backendMu.Unlock()
	err := fn(ctx)
	m.backendMu.Lock()

	m.mu.Lock()
	m.browserLogins--
	m.mu.Unlock()

	if err != nil {
		return err
	}
	return ctx.Err()
}

// Binary VA: 0x102335d10 | Size: 256 B | Source: third_party/jetski/cli/acp/auth/manager.go:L220
func (m *Manager) withBackendLock(fn func() error) error {
	m.backendMu.Lock()
	// Binary VA: 0x10233ff60 | Size: 112 B | Source: third_party/jetski/cli/acp/auth/manager.go:L222
	defer m.backendMu.Unlock()
	return fn()
}

// Binary VA: 0x102335e10 | Size: 256 B | Source: third_party/jetski/cli/acp/auth/manager.go:L229
func (m *Manager) clearActiveAuth() {
	m.mu.Lock()
	// Binary VA: 0x10233ffd0 | Size: 112 B | Source: third_party/jetski/cli/acp/auth/manager.go:L231
	defer m.mu.Unlock()
	m.authenticated = false
	m.activeMethod = ""
}

// Binary VA: 0x102335f10 | Size: 544 B | Source: third_party/jetski/cli/acp/auth/manager.go:L238
func (m *Manager) SetBackend(be Backend, mode BackendMode, cliAuth *codeassistclient.CLIAuthProvider, client *codeassistclient.CodeAssistClient) {
	m.backendMu.Lock()
	// Binary VA: 0x102340040 | Size: 112 B | Source: third_party/jetski/cli/acp/auth/manager.go:L240
	defer m.backendMu.Unlock()
	m.be = be
	if mode != "" {
		m.backendMode = mode
	}
	m.SetBackendFieldsWithBackendMuHeld(cliAuth, client)
}

// Binary VA: 0x102336130 | Size: 272 B | Source: third_party/jetski/cli/acp/auth/manager.go:L251
func (m *Manager) SetBackendFieldsWithBackendMuHeld(cliAuth *codeassistclient.CLIAuthProvider, client *codeassistclient.CodeAssistClient) {
	m.cliAuth = cliAuth
	m.codeAssistClient = client
	if m.cfg.TierFetcher == nil && (client != nil || cliAuth != nil) {
		m.tierFetcher = newCodeAssistTierFetcher(client, cliAuth)
	}
	if m.cfg.ADCLoader == nil && cliAuth != nil {
		m.adcLoader = google.FindDefaultCredentials
	}
}

// Binary VA: 0x102336240 | Size: 240 B | Source: third_party/jetski/cli/acp/auth/manager.go:L263
func (m *Manager) IsAuthenticated() bool {
	m.mu.Lock()
	// Binary VA: 0x1023400b0 | Size: 112 B | Source: third_party/jetski/cli/acp/auth/manager.go:L265
	defer m.mu.Unlock()
	return m.authenticated
}

// Binary VA: 0x102336330 | Size: 16 B | Source: third_party/jetski/cli/acp/auth/manager.go:L270
func (m *Manager) IsJetski() bool {
	return m.cfg.IsJetski
}

// Binary VA: 0x102336340 | Size: 240 B | Source: third_party/jetski/cli/acp/auth/manager.go:L275
func (m *Manager) ActiveMethod() string {
	m.mu.Lock()
	// Binary VA: 0x102340120 | Size: 112 B | Source: third_party/jetski/cli/acp/auth/manager.go:L277
	defer m.mu.Unlock()
	return m.activeMethod
}

// Binary VA: 0x102336430 | Size: 240 B | Source: third_party/jetski/cli/acp/auth/manager.go:L282
func (m *Manager) BackendMode() BackendMode {
	m.backendMu.Lock()
	// Binary VA: 0x102340190 | Size: 112 B | Source: third_party/jetski/cli/acp/auth/manager.go:L284
	defer m.backendMu.Unlock()
	return m.backendMode
}

// Binary VA: 0x102336520 | Size: 368 B | Source: third_party/jetski/cli/acp/auth/manager.go:L290
func (m *Manager) GatewayConfig() *GatewayConfig {
	m.mu.Lock()
	// Binary VA: 0x102340200 | Size: 112 B | Source: third_party/jetski/cli/acp/auth/manager.go:L292
	defer m.mu.Unlock()
	if m.gatewayConfig == nil {
		return nil
	}
	cp := *m.gatewayConfig
	return &cp
}

// Binary VA: 0x102336690 | Size: 560 B | Source: third_party/jetski/cli/acp/auth/manager.go:L302
func (m *Manager) TierInfo() TierInfo {
	m.mu.Lock()
	// Binary VA: 0x102340270 | Size: 112 B | Source: third_party/jetski/cli/acp/auth/manager.go:L304
	defer m.mu.Unlock()
	if !m.authenticated {
		return TierInfo{}
	}
	if m.activeMethod != "oauth-personal" && m.activeMethod != "oauth-business" {
		return TierInfo{}
	}
	return m.tierInfo
}

// Binary VA: 0x1023368c0 | Size: 336 B | Source: third_party/jetski/cli/acp/auth/manager.go:L318
func (m *Manager) TierMeta() map[string]any {
	return m.TierInfo().Meta()
}

// Binary VA: 0x102336a10 | Size: 304 B | Source: third_party/jetski/cli/acp/auth/manager.go:L322
func (m *Manager) isAuthenticatedWith(method string) bool {
	m.mu.Lock()
	// Binary VA: 0x1023402e0 | Size: 112 B | Source: third_party/jetski/cli/acp/auth/manager.go:L324
	defer m.mu.Unlock()
	return m.authenticated && m.activeMethod == method
}

// Binary VA: 0x102336b40 | Size: 240 B | Source: third_party/jetski/cli/acp/auth/manager.go:L335
func (m *Manager) refreshExperiments(ctx context.Context) {
	if m.be == nil {
		return
	}
	if err := m.be.RefreshExperiments(ctx); err != nil {
		log.ctxlogf(ctx, 1 /* inferred */, "Failed to refresh experiments after sign-in; customizations may be missing until the next sign-in: %v", err)
	}
}

// Binary VA: 0x102336c30 | Size: 736 B | Source: third_party/jetski/cli/acp/auth/manager.go:L344
func (m *Manager) fetchTier(ctx context.Context, method string) TierInfo {
	if method != "oauth-personal" && method != "oauth-business" {
		return TierInfo{}
	}
	if m.tierFetcher != nil {
		tier := m.tierFetcher(ctx, method)
		if !tier.IsEmpty() {
			return tier
		}
	}
	if method == "oauth-business" && m.tokenStore != nil {
		if tok, err := m.tokenStore.LoadStoredToken(); err == nil && tok != nil {
			if tok.TierID != "" || tok.TierName != "" {
				return TierInfo{
					Name: cmp.Or(tok.TierName, tok.TierID),
					ID:   tok.TierID,
				}
			}
		}
	}
	return TierInfo{}
}

// Binary VA: 0x102336f10 | Size: 288 B | Source: third_party/jetski/cli/acp/auth/manager.go:L364
func (m *Manager) readSettings(ctx context.Context) []byte {
	if r, ok := m.be.(settingsReader); ok {
		if data, err := r.ReadCliSettings(ctx); err == nil && len(data) > 0 {
			return data
		}
	}
	if m.cfg.SettingsPath != "" {
		if data, err := os.ReadFile(m.cfg.SettingsPath); err == nil {
			return data
		}
	}
	return resolveFallbackSettingsData(m.cfg.AppDataDir, m.cfg.CLIDirName, m.cfg.IsJetski)
}

// Binary VA: 0x102337030 | Size: 384 B | Source: third_party/jetski/cli/acp/auth/manager.go:L378
func (m *Manager) ensureBackendMode(ctx context.Context, sel BackendSelection) error {
	if m.backendMode == sel.Mode {
		return nil
	}
	if m.cfg.OnBackendModeChange == nil {
		m.backendMode = sel.Mode
		return nil
	}
	be, err := m.cfg.OnBackendModeChange(ctx, sel)
	if err != nil {
		return err
	}
	if be != nil {
		m.be = be
	}
	m.backendMode = sel.Mode
	return nil
}

// Binary VA: 0x1023371b0 | Size: 208 B | Source: third_party/jetski/cli/acp/auth/manager.go:L397
func (m *Manager) setInBandGeminiKey(key string) {
	if key == "" {
		m.restoreInBandGeminiKey()
		return
	}
	if !m.inBandGeminiKeyActive {
		m.origGeminiKeyValue, m.origGeminiKeyPresent = os.LookupEnv("GEMINI_API_KEY")
		m.inBandGeminiKeyActive = true
	}
	_ = os.Setenv("GEMINI_API_KEY", key)
}

// Binary VA: 0x102337280 | Size: 176 B | Source: third_party/jetski/cli/acp/auth/manager.go:L409
func (m *Manager) restoreInBandGeminiKey() {
	if !m.inBandGeminiKeyActive {
		return
	}
	if m.origGeminiKeyPresent {
		_ = os.Setenv("GEMINI_API_KEY", m.origGeminiKeyValue)
	} else {
		_ = os.Unsetenv("GEMINI_API_KEY")
	}
	m.inBandGeminiKeyActive = false
	m.origGeminiKeyPresent = false
	m.origGeminiKeyValue = ""
}

// Binary VA: 0x102337330 | Size: 192 B | Source: third_party/jetski/cli/acp/auth/manager.go:L423
func (m *Manager) applyGatewayProvider(ctx context.Context, cfg *GatewayConfig) {
	if cfg == nil || m.codeAssistClient == nil {
		return
	}
	provider := gateway.NewAuthProviderWithOptions(
		cfg.BaseURL,
		cfg.APIKey,
		cfg.WireProtocol,
		cfg.Headers,
		cfg.CACertPath,
		cfg.ProxyURL,
		cfg.RouteWebFetch,
	)
	m.codeAssistClient.AuthProvider = provider /* inferred field name at +0x0 */
	m.codeAssistClient.ResetCaches()
}

// Binary VA: 0x1023373f0 | Size: 160 B | Source: third_party/jetski/cli/acp/auth/manager.go:L439
func (m *Manager) restoreCLIAuthProvider() {
	if m.codeAssistClient == nil || m.cliAuth == nil {
		return
	}
	if m.codeAssistClient.AuthProvider != m.cliAuth /* inferred field name at +0x0 */ {
		m.codeAssistClient.AuthProvider = m.cliAuth
		m.codeAssistClient.ResetCaches()
	}
}

// Binary VA: 0x102337490 | Size: 256 B | Source: third_party/jetski/cli/acp/auth/manager.go:L446
func (m *Manager) resetVertexConfig() {
	if m.cliAuth == nil {
		return
	}
	if m.cliAuth.GetAuthMethod() != "adc" {
		return
	}
	m.cliAuth.SetTokenSource(nil)
	m.cliAuth.SetAuthMethod("")
	m.cliAuth.SetQuotaProject("")
	m.cliAuth.SetLocation("")
	m.cliAuth.SetUserTier("", "")
	if m.codeAssistClient != nil {
		m.codeAssistClient.ResetCaches()
	}
}

// Binary VA: 0x102337590 | Size: 1936 B | Source: third_party/jetski/cli/acp/auth/manager.go:L462
func (m *Manager) applyVertexConfig(ctx context.Context, cfg vertexConfig) error {
	if m.cliAuth == nil {
		if m.adcLoader != nil {
			creds, err := m.adcLoader(ctx, adcOAuthScopes()...)
			if err != nil || creds == nil {
				return fmt.Errorf("failed to load Application Default Credentials: %w", err)
			}
		}
		return nil
	}

	if m.adcLoader != nil {
		creds, err := m.adcLoader(ctx, adcOAuthScopes()...)
		if err != nil {
			return fmt.Errorf("failed to load Application Default Credentials: %w", err)
		}
		if creds == nil || creds.TokenSource == nil {
			return errors.New("no Application Default Credentials found")
		}
		if _, err := creds.TokenSource.Token(); err != nil {
			return fmt.Errorf("failed to acquire token from Application Default Credentials: %w", err)
		}
		m.cliAuth.SetTokenSource(creds.TokenSource)
	}

	m.cliAuth.SetAuthMethod("adc")
	m.cliAuth.SetQuotaProject(cfg.Project)
	m.cliAuth.SetLocation(cfg.Location)
	m.cliAuth.SetUserTier("", "")
	cliAuth := m.cliAuth
	// Binary VA: 0x1023403c0 | Size: 160 B | Source: third_party/jetski/cli/acp/auth/manager.go:L490
	cliAuth.SetIsBusinessAuthFetcher(func() bool {
		method := cliAuth.GetAuthMethod()
		return method == "gcp" || method == "adc"
	})
	// Binary VA: 0x102340350 | Size: 112 B | Source: third_party/jetski/cli/acp/auth/manager.go:L494
	cliAuth.SetIsADCFetcher(func() bool {
		return cliAuth.GetAuthMethod() == "adc"
	})
	if m.codeAssistClient != nil {
		m.codeAssistClient.ResetCaches()
	}
	return nil
}

// Binary VA: 0x102337d20 | Size: 6320 B | Source: third_party/jetski/cli/acp/auth/manager.go:L507
func (m *Manager) EnsureAuthenticated(ctx context.Context) bool {
	m.backendMu.Lock()
	// Binary VA: 0x102340460 | Size: 112 B | Source: third_party/jetski/cli/acp/auth/manager.go:L509
	defer m.backendMu.Unlock()

	m.mu.Lock()
	if m.browserLogins > 0 {
		m.mu.Unlock()
		return false
	}
	if m.authenticated {
		m.mu.Unlock()
		return true
	}
	loggedOut := m.loggedOut
	m.mu.Unlock()

	if loggedOut {
		if m.tokenStore == nil {
			return false
		}
		tok, err := m.tokenStore.LoadStoredToken()
		if err != nil || tok == nil || tok.RefreshToken == "" {
			return false
		}
	}

	if !m.cfg.IsJetski {
		settingsBytes := m.readSettings(ctx)
		gwDisp, gwCfg, _, gwErr := resolveGateway(m.cfg.AdminSettingsPath, m.cfg.DisableDefaultAdminSettings, nil, false)
		if gwDisp == 1 /* adminGatewayRequired */ {
			if gwErr != nil || gwCfg == nil {
				log.ctxlogf(ctx, 1 /* inferred */, "Enterprise policy requires gateway auth, but configuration is unusable: %v", gwErr)
				return false
			}
			_ = m.ensureBackendMode(ctx, BackendSelection{Mode: "gateway", Gateway: gwCfg})
			if m.be != nil && m.backendMode == "gateway" {
				m.be.TrySilentAuth()
			}
			m.applyGatewayProvider(ctx, gwCfg)
			m.refreshExperiments(ctx)
			m.mu.Lock()
			m.authenticated = true
			m.loggedOut = false
			m.activeMethod = "gateway"
			m.gatewayConfig = gwCfg
			m.tierInfo = TierInfo{}
			m.mu.Unlock()
			return true
		}

		method := inferSilentMethod(settingsBytes, m.cfg.IsJetski, m.cfg.AppDataDir, m.cfg.AdminSettingsPath, m.cfg.DisableDefaultAdminSettings)
		switch method {
		case "gateway":
			if gwDisp == 2 /* adminGatewayDisabled */ {
				return false
			}
			_, gwCfg, _, gwErr := resolveGateway(m.cfg.AdminSettingsPath, m.cfg.DisableDefaultAdminSettings, nil, true)
			if gwErr != nil || gwCfg == nil {
				return false
			}
			m.resetVertexConfig()
			if err := m.ensureBackendMode(ctx, BackendSelection{Mode: "gateway", Gateway: gwCfg}); err != nil {
				return false
			}
			m.applyGatewayProvider(ctx, gwCfg)
			m.refreshExperiments(ctx)
			m.mu.Lock()
			m.authenticated = true
			m.loggedOut = false
			m.activeMethod = "gateway"
			m.gatewayConfig = gwCfg
			m.tierInfo = TierInfo{}
			m.mu.Unlock()
			return true

		case "agent-platform":
			vCfg := resolveVertexConfig(settingsBytes)
			if !vCfg.IsConfigured() {
				return false
			}
			if err := m.ensureBackendMode(ctx, BackendSelection{
				Mode:     "adc",
				MethodID: "agent-platform",
				Vertex:   vCfg,
			}); err != nil {
				return false
			}
			m.restoreCLIAuthProvider()
			if err := m.applyVertexConfig(ctx, vCfg); err != nil {
				log.ctxlogf(ctx, 1 /* inferred */, "Silent Agent Platform auth failed: %v", err)
				return false
			}
			m.refreshExperiments(ctx)
			m.mu.Lock()
			m.authenticated = true
			m.loggedOut = false
			m.activeMethod = "agent-platform"
			m.activeVertex = vCfg
			m.gatewayConfig = nil
			m.tierInfo = TierInfo{}
			m.mu.Unlock()
			return true

		case "gemini-api-key":
			key := os.Getenv("GEMINI_API_KEY")
			if key == "" {
				return false
			}
			if err := m.ensureBackendMode(ctx, BackendSelection{
				Mode:                "gemini-api",
				UseGeminiAPIKeyAuth: true, /* inferred from strhdr 'gemini-api' */
				GeminiAPIKey:        key,
			}); err != nil {
				return false
			}
			if m.be != nil && m.backendMode == "gemini-api" {
				m.be.TrySilentAuth()
			}
			m.refreshExperiments(ctx)
			m.mu.Lock()
			m.authenticated = true
			m.loggedOut = false
			m.activeMethod = "gemini-api-key"
			m.activeGeminiKey = key
			m.gatewayConfig = nil
			m.tierInfo = TierInfo{}
			m.mu.Unlock()
			return true

		default:
			if m.backendMode == "gemini-api" {
				key := os.Getenv("GEMINI_API_KEY")
				if key == "" || m.be == nil || !m.be.TrySilentAuth() {
					return false
				}
				m.refreshExperiments(ctx)
				m.mu.Lock()
				m.authenticated = true
				m.loggedOut = false
				m.activeMethod = "gemini-api-key"
				m.activeGeminiKey = key
				m.gatewayConfig = nil
				m.tierInfo = TierInfo{}
				m.mu.Unlock()
				return true
			}
			if m.backendMode == "gateway" {
				if gwDisp == 2 || m.be == nil || !m.be.TrySilentAuth() {
					return false
				}
				_, gwCfg, _, _ := resolveGateway(m.cfg.AdminSettingsPath, m.cfg.DisableDefaultAdminSettings, nil, true)
				m.refreshExperiments(ctx)
				m.mu.Lock()
				m.authenticated = true
				m.loggedOut = false
				m.activeMethod = "gateway"
				m.gatewayConfig = gwCfg
				m.tierInfo = TierInfo{}
				m.mu.Unlock()
				return true
			}
			if m.backendMode == "adc" || env.IsOnOrDefault("AGY_ADC_AUTH", false) {
				m.restoreCLIAuthProvider()
				if m.be == nil || !m.be.TrySilentAuth() {
					return false
				}
				vCfg := resolveVertexConfig(settingsBytes)
				if vCfg.Project != "" && m.cliAuth != nil {
					m.cliAuth.SetQuotaProject(vCfg.Project)
				}
				if vCfg.Location != "" && m.cliAuth != nil {
					m.cliAuth.SetLocation(vCfg.Location)
				}
				m.refreshExperiments(ctx)
				m.mu.Lock()
				m.authenticated = true
				m.loggedOut = false
				m.activeMethod = "agent-platform"
				m.activeVertex = vCfg
				m.tierInfo = TierInfo{}
				m.mu.Unlock()
				return true
			}
			if m.tokenStore != nil && settings.Project != "" {
				if tok, err := m.tokenStore.LoadStoredToken(); err == nil && tok != nil && tok.RefreshToken != "" && tok.AuthMethod == "gcp" {
					loc := cmp.Or(settings.Location, tok.Location, "global")
					if tok.Project != settings.Project || tok.Location != loc {
						_ = m.tokenStore.SaveProjectAndRegion(settings.Project, loc)
					}
				}
			}
		}
	}

	m.restoreCLIAuthProvider()
	m.resetVertexConfig()
	if m.be == nil || !m.be.TrySilentAuth() {
		return false
	}

	method := "oauth-personal"
	if !m.cfg.IsJetski && m.tokenStore != nil {
		if tok, err := m.tokenStore.LoadStoredToken(); err == nil && tok != nil && tok.AuthMethod == "gcp" {
			if m.cliAuth != nil && tok.TierID != "" {
				m.cliAuth.SetUserTier(tok.TierName, tok.TierID)
			}
			method = "oauth-business"
		}
	}

	m.refreshExperiments(ctx)
	tier := m.fetchTier(ctx, method)
	m.mu.Lock()
	m.authenticated = true
	m.loggedOut = false
	m.activeMethod = method
	m.gatewayConfig = nil
	m.tierInfo = tier
	m.mu.Unlock()
	return true
}

// Binary VA: 0x1023395d0 | Size: 1968 B | Source: third_party/jetski/cli/acp/auth/manager.go:L760
func (m *Manager) validateMethod(methodID string, meta map[string]any) (string, *GatewayConfig, error) {
	canonical := methodID
	switch methodID {
	case "vertex-ai":
		canonical = "agent-platform"
	case "terminal-login":
		canonical = "oauth-personal"
	}

	if m.cfg.IsJetski {
		if canonical != "oauth-personal" {
			return "", nil, &Error{
				Code:    -32602,
				Message: fmt.Sprintf("Unsupported authentication method %q; this agent supports %q.", methodID, "oauth-personal"),
			}
		}
		return canonical, nil, nil
	}

	switch canonical {
	case "gateway":
		if !m.cfg.GatewayEnabled && !IsGatewayAuthEnabled() {
			return "", nil, &Error{
				Code:    -32602,
				Message: fmt.Sprintf("Unsupported auth method: %q", methodID),
			}
		}
	case "agent-platform", "gemini-api-key", "oauth-business", "oauth-personal":
	default:
		return "", nil, &Error{
			Code:    -32602,
			Message: fmt.Sprintf("Unsupported auth method: %q", methodID),
		}
	}

	var gwMeta *GatewayConfig
	if canonical == "gateway" {
		if raw, ok := meta["gateway"]; ok && raw != nil {
			parsed, err := parseGatewayMeta(raw)
			if err != nil {
				return "", nil, err
			}
			gwMeta = parsed
		}
	}

	disp := resolveAdminGatewayDisposition(m.cfg.AdminSettingsPath, m.cfg.DisableDefaultAdminSettings)
	if disp == 1 /* adminGatewayRequired */ && canonical != "gateway" {
		return "", nil, &Error{
			Code:    -32602,
			Message: fmt.Sprintf("Auth method %q is not permitted on this machine: enterprise policy requires %q.", methodID, "gateway"),
		}
	}
	if disp == 2 /* adminGatewayDisabled */ && canonical == "gateway" {
		return "", nil, &Error{
			Code:    -32602,
			Message: "Gateway authentication is disabled by enterprise policy (gateway.enabled is false in admin_settings.json).",
		}
	}
	return canonical, gwMeta, nil
}

// Binary VA: 0x102339d80 | Size: 1744 B | Source: third_party/jetski/cli/acp/auth/manager.go:L818
func (m *Manager) Authenticate(ctx context.Context, params schema.AuthenticateParams) error {
	log.ctxlogf(ctx, 1 /* inferred */, "ACP authenticate: methodId=%q meta=%v", params.MethodId, redactAuthMeta(params.Meta))
	canonical, gwMeta, err := m.validateMethod(params.MethodId, params.Meta)
	if err != nil {
		return err
	}

	loginCtx, cancel := context.WithTimeout(ctx, m.loginTimeout)
	defer cancel()

	m.mu.Lock()
	if err := loginCtx.Err(); err != nil {
		m.mu.Unlock()
		return err
	}
	if m.cancelLogin != nil {
		m.cancelLogin()
	}
	m.cancelLogin = cancel
	m.mu.Unlock()

	m.backendMu.Lock()
	// Binary VA: 0x1023404d0 | Size: 112 B | Source: third_party/jetski/cli/acp/auth/manager.go:L841
	defer m.backendMu.Unlock()

	if err := loginCtx.Err(); err != nil {
		return err
	}

	switch canonical {
	case "oauth-personal":
		return m.authenticateOAuthPersonal(loginCtx, params.MethodId != "terminal-login")
	case "oauth-business":
		return m.authenticateOAuthBusiness(loginCtx, params.Meta)
	case "gemini-api-key":
		return m.authenticateGeminiAPIKey(loginCtx, params.Meta)
	case "agent-platform":
		return m.authenticateAgentPlatform(loginCtx)
	case "gateway":
		return m.authenticateGateway(loginCtx, gwMeta)
	default:
		return &Error{
			Code:    -32602,
			Message: fmt.Sprintf("Unsupported auth method: %q", params.MethodId),
		}
	}
}

// Binary VA: 0x10233a450 | Size: 192 B | Source: third_party/jetski/cli/acp/auth/manager.go:L867
func (m *Manager) trySilentConsumer() bool {
	if m.be == nil {
		return false
	}
	if m.tokenStore != nil {
		if tok, err := m.tokenStore.LoadStoredToken(); err == nil && tok != nil && tok.RefreshToken != "" && tok.AuthMethod == "gcp" {
			return false
		}
	}
	return m.be.TrySilentAuth()
}

// Binary VA: 0x10233a510 | Size: 160 B | Source: third_party/jetski/cli/acp/auth/manager.go:L882
func (m *Manager) hasSavedConsumerToken() bool {
	if m.tokenStore == nil {
		return true
	}
	tok, err := m.tokenStore.LoadStoredToken()
	if err != nil || tok == nil || tok.RefreshToken == "" {
		return false
	}
	return tok.AuthMethod != "gcp"
}

// Binary VA: 0x10233a5b0 | Size: 1072 B | Source: third_party/jetski/cli/acp/auth/manager.go:L890
func (m *Manager) authenticateOAuthPersonal(ctx context.Context, allowBrowser bool) error {
	if m.isAuthenticatedWith("oauth-personal") {
		return nil
	}
	if !allowBrowser && !m.hasSavedConsumerToken() {
		return &Error{
			Code:    -32000,
			Message: "Authentication required",
		}
	}
	m.clearActiveAuth()
	m.restoreInBandGeminiKey()
	if err := m.ensureBackendMode(ctx, BackendSelection{Mode: "ccpa"}); err != nil {
		return err
	}
	m.restoreCLIAuthProvider()
	m.resetVertexConfig()
	if !m.trySilentConsumer() {
		if !allowBrowser {
			return &Error{
				Code:    -32000,
				Message: "Authentication required",
			}
		}
		if m.consumerLogin == nil {
			return errors.New("browser sign-in is not configured")
		}
		if err := m.waitForBrowser(ctx, m.consumerLogin); err != nil {
			return err
		}
		if m.be == nil || !m.be.TrySilentAuth() {
			return errors.New("signed in, but couldn't load the saved credentials")
		}
	}
	m.refreshExperiments(ctx)
	tier := m.fetchTier(ctx, "oauth-personal")
	RememberMethod(m.cfg.AppDataDir, "oauth-personal")

	m.mu.Lock()
	// Binary VA: 0x102340540 | Size: 112 B | Source: third_party/jetski/cli/acp/auth/manager.go:L928
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	m.authenticated = true
	m.loggedOut = false
	m.activeMethod = "oauth-personal"
	m.gatewayConfig = nil
	m.tierInfo = tier
	return nil
}

// Binary VA: 0x10233a9e0 | Size: 704 B | Source: third_party/jetski/cli/acp/auth/manager.go:L940
func (m *Manager) trySilentBusiness() (TierInfo, bool) {
	if m.be == nil {
		return TierInfo{}, false
	}
	if m.tokenStore == nil {
		if m.businessLogin == nil && m.be.TrySilentAuth() {
			return TierInfo{}, true
		}
		return TierInfo{}, false
	}
	tok, err := m.tokenStore.LoadStoredToken()
	if err != nil || tok == nil || tok.RefreshToken == "" || tok.AuthMethod != "gcp" {
		return TierInfo{}, false
	}
	if tok.TierID == "" && tok.Project == "" {
		return TierInfo{}, false
	}
	if !m.be.TrySilentAuth() {
		return TierInfo{}, false
	}
	if m.cliAuth != nil && tok.TierID != "" {
		m.cliAuth.SetUserTier(tok.TierName, tok.TierID)
	}
	return TierInfo{
		Name: cmp.Or(tok.TierName, tok.TierID),
		ID:   tok.TierID,
	}, true
}

// Binary VA: 0x10233aca0 | Size: 2032 B | Source: third_party/jetski/cli/acp/auth/manager.go:L971
func (m *Manager) authenticateOAuthBusiness(ctx context.Context, meta map[string]any) error {
	if m.isAuthenticatedWith("oauth-business") {
		return nil
	}
	m.clearActiveAuth()
	m.restoreInBandGeminiKey()
	if err := m.ensureBackendMode(ctx, BackendSelection{Mode: "ccpa"}); err != nil {
		return err
	}
	m.restoreCLIAuthProvider()
	m.resetVertexConfig()

	tier, ok := m.trySilentBusiness()
	if !ok {
		if m.businessLogin == nil {
			return errors.New("enterprise browser sign-in is not configured")
		}
		var tok *oauth2.Token
		// Binary VA: 0x102340720 | Size: 128 B | Source: third_party/jetski/cli/acp/auth/manager.go:L992
		if err := m.waitForBrowser(ctx, func(ctx context.Context) error {
			var err error
			tok, err = m.businessLogin(ctx)
			return err
		}); err != nil {
			return err
		}

		var prevEndpoint string
		if m.cliAuth != nil {
			prevEndpoint = m.cliAuth.GetEndpointURL()
		}
		// Binary VA: 0x102340620 | Size: 256 B | Source: third_party/jetski/cli/acp/auth/manager.go:L1004
		rollback := func() {
			if m.cliAuth != nil {
				m.cliAuth.SetTokenSource(nil)
				m.cliAuth.SetAuthMethod("")
				m.cliAuth.SetQuotaProject("")
				m.cliAuth.SetLocation("")
				m.cliAuth.SetUserTier("", "")
				if prevEndpoint != "" {
					m.cliAuth.SetEndpointURL(prevEndpoint)
				}
			}
			if m.tokenStore != nil {
				_ = m.tokenStore.RemoveToken()
			}
			if m.codeAssistClient != nil {
				m.codeAssistClient.ResetCaches()
			}
		}

		if m.cliAuth != nil && tok != nil {
			scopes := adcOAuthScopes()
			oauthCfg := &oauth2.Config{
				ClientID:     "884354919052-<redacted-gcp-client-id>",
				ClientSecret: "<redacted-oauth-client-secret>",
				Endpoint:     google.Endpoint,
				Scopes:       scopes,
			}
			ts := codeassistclient.RefreshingTokenSource(ctx, oauthCfg, tok)
			m.cliAuth.SetTokenSource(ts)
			m.cliAuth.SetAuthMethod("gcp")
			m.cliAuth.SetEnableBusinessLogin(true)
			if os.Getenv("CLOUD_CODE_URL") == "" {
				m.cliAuth.SetEndpointURL("https://cloudcode-pa.googleapis.com")
			}
		}

		var err error
		tier, err = m.resolveEnterpriseLicense(ctx, meta)
		if err != nil {
			rollback()
			return err
		}
		if err := ctx.Err(); err != nil {
			rollback()
			return err
		}
		if m.be == nil || !m.be.TrySilentAuth() {
			rollback()
			return errors.New("signed in to Gemini Enterprise, but couldn't load the saved credentials")
		}
		if m.cliAuth != nil && tier.ID != "" {
			m.cliAuth.SetUserTier(tier.Name, tier.ID)
		}
	}

	m.refreshExperiments(ctx)
	if tier.IsEmpty() {
		tier = m.fetchTier(ctx, "oauth-business")
	}
	RememberMethod(m.cfg.AppDataDir, "oauth-business")

	m.mu.Lock()
	// Binary VA: 0x1023405b0 | Size: 112 B | Source: third_party/jetski/cli/acp/auth/manager.go:L1068
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	m.authenticated = true
	m.loggedOut = false
	m.activeMethod = "oauth-business"
	m.gatewayConfig = nil
	m.tierInfo = tier
	return nil
}

// Binary VA: 0x10233b490 | Size: 3424 B | Source: third_party/jetski/cli/acp/auth/manager.go:L1080
func (m *Manager) resolveEnterpriseLicense(ctx context.Context, meta map[string]any) (TierInfo, error) {
	settings := parseSettingsBytes(m.readSettings(ctx))
	project, _ := meta["project"].(string)
	if project == "" {
		project = settings.Project
	}
	location, _ := meta["location"].(string)
	if location == "" {
		location = settings.Location
	}

	lb, ok := m.be.(licenseBackend)
	if !ok {
		if m.tokenStore != nil && project != "" {
			_ = m.tokenStore.SaveProjectAndRegion(project, cmp.Or(location, "global"))
		}
		return TierInfo{}, nil
	}

	licenses, fetchErr := lb.FetchLicenses(ctx)
	if fetchErr != nil {
		log.ctxlogf(ctx, 1 /* inferred */, "FetchLicenses failed during oauth-business login: %v", fetchErr)
	}

	var selected *language_server_go_proto.License
	if project != "" {
		if location != "" {
			for _, lic := range licenses {
				if lic.GetProject() == project && lic.GetLocation() == location && !lic.GetIneligible() /* inferred: +0x40 == 0 */ {
					selected = lic
					break
				}
			}
			if selected == nil {
				var err error
				selected, err = lb.SelfAssignLicense(ctx, project, location)
				if err != nil {
					return TierInfo{}, fmt.Errorf("no Gemini Enterprise license found for project %q / location %q, and self-assign failed: %w", project, location, err)
				}
			}
		} else {
			for _, lic := range licenses {
				if lic.GetProject() == project {
					selected = lic
					break
				}
			}
			if selected == nil {
				var err error
				selected, err = lb.SelfAssignLicense(ctx, project, cmp.Or(location, "global"))
				if err != nil {
					return TierInfo{}, fmt.Errorf("no Gemini Enterprise license found for project %q, and self-assign failed: %w", project, err)
				}
			}
		}
	} else {
		if envProj := constants.GetEnvProject(); envProj != "" {
			for _, lic := range licenses {
				if lic.GetProject() == envProj {
					selected = lic
					break
				}
			}
		}
		if selected == nil && len(licenses) > 0 {
			selected = licenses[0]
		}
	}

	if selected == nil || selected.GetLicenseName() == "" /* inferred: +0x10 len == 0 */ {
		if fetchErr != nil {
			return TierInfo{}, fmt.Errorf("couldn't fetch Gemini Enterprise licenses: %w", fetchErr)
		}
		return TierInfo{}, errors.New("no Gemini Enterprise license found for this account; configure gcp.project and gcp.location in settings.json and try again")
	}

	licProject := selected.GetProject()
	licLocation := cmp.Or(selected.GetLocation(), location, "global")
	tierID := selected.GetTierId()
	tierName := cmp.Or(selected.GetTierName(), tierID)

	if m.tokenStore != nil {
		if err := m.tokenStore.SaveProjectAndRegion(licProject, licLocation); err != nil {
			log.ctxlogf(ctx, 1 /* inferred */, "Failed to persist enterprise project/location: %v", err)
		}
		if err := m.tokenStore.SaveUserTier(tierName, tierID); err != nil {
			log.ctxlogf(ctx, 1 /* inferred */, "Failed to persist enterprise user tier: %v", err)
		}
	}

	if err := lb.SelectLicense(ctx, tierID, licProject, licLocation); err != nil {
		return TierInfo{}, fmt.Errorf("failed to select Gemini Enterprise license: %w", err)
	}
	if m.cliAuth != nil {
		m.cliAuth.SetUserTier(tierName, tierID)
	}
	return TierInfo{
		Name: tierName,
		ID:   tierID,
	}, nil
}

// Binary VA: 0x10233c1f0 | Size: 1616 B | Source: third_party/jetski/cli/acp/auth/manager.go:L1181
func (m *Manager) authenticateGeminiAPIKey(ctx context.Context, meta map[string]any) error {
	inBandKey := "" /* inferred: from meta key lookup inlined from methods.go:327-345 */
	key, inBand := ExtractGeminiAPIKey(meta)
	if inBand {
		inBandKey = key
	}
	if key == "" {
		return &Error{
			Code:    -32602,
			Message: fmt.Sprintf("%s is not set; pass an API key in _meta.apiKey or set the %s environment variable.", "GEMINI_API_KEY", "GEMINI_API_KEY"),
		}
	}

	m.mu.Lock()
	alreadyActive := m.authenticated && m.activeMethod == "gemini-api-key" && m.activeGeminiKey == key
	m.mu.Unlock()
	if alreadyActive {
		return nil
	}

	m.setInBandGeminiKey(inBandKey)
	m.resetVertexConfig()
	if err := m.ensureBackendMode(ctx, BackendSelection{
		Mode:                "gemini-api",
		UseGeminiAPIKeyAuth: true, /* inferred */
		GeminiAPIKey:        key,
	}); err != nil {
		return err
	}
	if m.be != nil && m.backendMode == "gemini-api" {
		m.be.TrySilentAuth()
	}
	m.refreshExperiments(ctx)
	if inBandKey == "" {
		RememberMethod(m.cfg.AppDataDir, "gemini-api-key")
	}

	m.mu.Lock()
	// Binary VA: 0x1023407a0 | Size: 112 B | Source: third_party/jetski/cli/acp/auth/manager.go:L1219
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	m.authenticated = true
	m.loggedOut = false
	m.activeMethod = "gemini-api-key"
	m.activeGeminiKey = key
	m.gatewayConfig = nil
	m.tierInfo = TierInfo{}
	return nil
}

// Binary VA: 0x10233c840 | Size: 1488 B | Source: third_party/jetski/cli/acp/auth/manager.go:L1232
func (m *Manager) authenticateAgentPlatform(ctx context.Context) error {
	vCfg := resolveVertexConfig(m.readSettings(ctx))
	if !vCfg.IsConfigured() {
		msg := fmt.Sprintf("A project and location from %s and %s or from settings.json gcp.project and gcp.location (with Application Default Credentials) must be set for Agent Platform authentication.", "GOOGLE_CLOUD_PROJECT", "GOOGLE_CLOUD_LOCATION")
		if os.Getenv("GOOGLE_API_KEY") != "" {
			msg = fmt.Sprintf("Agent Platform authentication in CLI ACP requires Application Default Credentials with %s and %s (or settings.json gcp.project and gcp.location); %s Express Mode without project/location is not supported (use %q for Gemini Developer API keys).", "GOOGLE_CLOUD_PROJECT", "GOOGLE_CLOUD_LOCATION", "GOOGLE_API_KEY", "gemini-api-key")
		}
		return &Error{
			Code:    -32602,
			Message: msg,
		}
	}

	m.mu.Lock()
	alreadyActive := m.authenticated && m.activeMethod == "agent-platform" && m.activeVertex == vCfg
	m.mu.Unlock()
	if alreadyActive {
		return nil
	}

	m.restoreInBandGeminiKey()
	if err := m.ensureBackendMode(ctx, BackendSelection{
		Mode:     "adc",
		MethodID: "agent-platform",
		Vertex:   vCfg,
	}); err != nil {
		return err
	}
	m.restoreCLIAuthProvider()
	if err := m.applyVertexConfig(ctx, vCfg); err != nil {
		return err
	}
	m.refreshExperiments(ctx)
	RememberMethod(m.cfg.AppDataDir, "agent-platform")

	m.mu.Lock()
	// Binary VA: 0x102340810 | Size: 112 B | Source: third_party/jetski/cli/acp/auth/manager.go:L1275
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	m.authenticated = true
	m.loggedOut = false
	m.activeMethod = "agent-platform"
	m.activeVertex = vCfg
	m.gatewayConfig = nil
	m.tierInfo = TierInfo{}
	return nil
}

// Binary VA: 0x10233ce10 | Size: 1216 B | Source: third_party/jetski/cli/acp/auth/manager.go:L1288
func (m *Manager) authenticateGateway(ctx context.Context, gwMeta *GatewayConfig) error {
	_, gwCfg, _, err := resolveGateway(m.cfg.AdminSettingsPath, m.cfg.DisableDefaultAdminSettings, gwMeta, true)
	if err != nil || gwCfg == nil {
		return &Error{
			Code:    -32602,
			Message: fmt.Sprintf("Gateway authentication could not be configured: %v.", err),
		}
	}

	m.mu.Lock()
	alreadyActive := m.authenticated && m.activeMethod == "gateway" && m.gatewayConfig.Equal(gwCfg)
	m.mu.Unlock()
	if alreadyActive {
		return nil
	}

	m.restoreInBandGeminiKey()
	m.resetVertexConfig()
	if err := m.ensureBackendMode(ctx, BackendSelection{
		Mode:    "gateway",
		Gateway: gwCfg,
	}); err != nil {
		return err
	}
	m.applyGatewayProvider(ctx, gwCfg)
	if m.be != nil && m.backendMode == "gateway" {
		m.be.TrySilentAuth()
	}
	m.refreshExperiments(ctx)

	m.mu.Lock()
	// Binary VA: 0x102340880 | Size: 112 B | Source: third_party/jetski/cli/acp/auth/manager.go:L1322
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	m.authenticated = true
	m.loggedOut = false
	m.activeMethod = "gateway"
	m.gatewayConfig = gwCfg
	m.tierInfo = TierInfo{}
	return nil
}

// Binary VA: 0x10233d2d0 | Size: 944 B | Source: third_party/jetski/cli/acp/auth/manager.go:L1336
func (m *Manager) Logout(ctx context.Context) error {
	m.mu.Lock()
	if m.cancelLogin != nil {
		m.cancelLogin()
	}
	m.mu.Unlock()

	m.backendMu.Lock()
	// Binary VA: 0x1023408f0 | Size: 112 B | Source: third_party/jetski/cli/acp/auth/manager.go:L1344
	defer m.backendMu.Unlock()

	m.mu.Lock()
	prevMethod := m.activeMethod
	m.authenticated = false
	m.loggedOut = true
	m.activeMethod = ""
	m.activeGeminiKey = ""
	m.activeVertex = vertexConfig{}
	m.gatewayConfig = nil
	m.tierInfo = TierInfo{}
	m.mu.Unlock()

	m.restoreInBandGeminiKey()
	m.restoreCLIAuthProvider()
	m.resetVertexConfig()
	if prevMethod != "gateway" {
		clearPersistedMethod(m.cfg.AppDataDir)
	}
	if m.be == nil {
		return nil
	}
	return m.be.Logout()
}
