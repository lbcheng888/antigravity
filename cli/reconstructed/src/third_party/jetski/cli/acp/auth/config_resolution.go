// Reconstructed from agy v1.5.10 binary evidence (disassembly + Go type table).
// Binary VA: 0x1023324d0 | Size: 17 funcs (first func IsConfigured, 32 B) | Source: third_party/jetski/cli/acp/auth/config_resolution.go:L42
// Reconstructed from agy v1.5.10 (arm64 Mach-O)
// Package auth: third_party/jetski/cli/acp/auth/config_resolution.go

package auth

import (
	"bytes"
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"google3/third_party/jetski/admin_controls/admincontrols"
	"google3/third_party/jetski/cli/acp/schema/schema"
	"google3/third_party/jetski/cli/env/env"
	"google3/third_party/jetski/cli/types/types"
	"google3/third_party/jetski/language_server/constants/constants"
)

// adminGatewayDisposition represents MDM admin gateway policy state. /* inferred */
type adminGatewayDisposition int

const (
	adminGatewayUnset    adminGatewayDisposition = 0
	adminGatewayRequired adminGatewayDisposition = 1
	adminGatewayDisabled adminGatewayDisposition = 2
)

// vertexConfig holds Vertex AI / Agent Platform project and location.
// Binary Type VA: 0x10a4181a8
type vertexConfig struct {
	Project  string
	Location string
}

// Binary VA: 0x1023324d0 | Size: 32 B | Source: third_party/jetski/cli/acp/auth/config_resolution.go:L42
func (v vertexConfig) IsConfigured() bool {
	return v.Project != "" && v.Location != ""
}

// parsedSettings holds auth/vertex fields extracted from settings.json. /* inferred */
type parsedSettings struct {
	authSelectedType string
	authType         string
	gcpProject       string
	gcpLocation      string
}

// Binary VA: 0x1023324f0 | Size: 832 B | Source: third_party/jetski/cli/acp/auth/config_resolution.go:L56
func parseSettingsBytes(data []byte) parsedSettings {
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	if len(data) == 0 {
		return parsedSettings{}
	}
	var raw map[string]jsontext.Value
	if err := json.Unmarshal(data, &raw); err != nil {
		return parsedSettings{}
	}
	var out parsedSettings /* inferred */
	fields := types.ParseSettingsFields(raw)
	out.authSelectedType = fields.SelectedAuthType /* inferred: +0x0 */
	if fields.GCP != nil {                         /* inferred: +0x1b8 */
		out.gcpProject = strings.TrimSpace(fields.GCP.Project)
		out.gcpLocation = strings.TrimSpace(fields.GCP.Location)
	}
	if authRaw, ok := raw["auth"]; ok {
		var authObj struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(authRaw, &authObj); err == nil {
			out.authType = strings.TrimSpace(authObj.Type)
		}
	}
	return out
}

// Binary VA: 0x102332830 | Size: 320 B | Source: third_party/jetski/cli/acp/auth/config_resolution.go:L91
func resolveVertexConfig(settingsData []byte) vertexConfig {
	parsed := parseSettingsBytes(settingsData)
	project := cmp.Or(os.Getenv("GOOGLE_CLOUD_PROJECT"), parsed.gcpProject)
	location := cmp.Or(os.Getenv("GOOGLE_CLOUD_LOCATION"), parsed.gcpLocation)
	return vertexConfig{
		Project:  project,
		Location: location,
	}
}

// GatewayConfig holds resolved AI API Gateway configuration.
// Binary Type VA: 0x10a7b8fe0
type GatewayConfig struct {
	BaseURL       string
	Headers       map[string]string
	APIKey        string
	WireProtocol  string
	CACertPath    string
	ProxyURL      string
	RouteWebFetch bool
}

// Binary VA: 0x102332970 | Size: 576 B | Source: third_party/jetski/cli/acp/auth/config_resolution.go:L113
func (g *GatewayConfig) Equal(other *GatewayConfig) bool {
	if g == nil || other == nil {
		return g == other
	}
	if g.BaseURL != other.BaseURL ||
		g.APIKey != other.APIKey ||
		g.WireProtocol != other.WireProtocol ||
		g.CACertPath != other.CACertPath ||
		g.ProxyURL != other.ProxyURL ||
		g.RouteWebFetch != other.RouteWebFetch {
		return false
	}
	return maps.Equal(g.Headers, other.Headers)
}

// Binary VA: 0x102332bb0 | Size: 1584 B | Source: third_party/jetski/cli/acp/auth/config_resolution.go:L136
func parseGatewayMeta(raw interface{}) (*GatewayConfig, error) {
	if raw == nil {
		return nil, nil
	}
	makeErr := func(msg string) error { /* inferred: L127-129 */
		return &Error{
			Code:    -32602, // -0x7f5a
			Message: fmt.Sprintf("authenticate(gateway): %s.", msg),
		}
	}
	obj, ok := raw.(map[string]interface{})
	if !ok {
		return nil, makeErr(fmt.Sprintf("`_meta.gateway` must be an object, got %T", raw))
	}
	baseURL, _ := obj["baseUrl"].(string)
	if baseURL == "" {
		return nil, makeErr("`_meta.gateway.baseUrl` is required and must be a non-empty string")
	}
	var headers map[string]string
	if rawHeaders, hasHeaders := obj["headers"]; hasHeaders && rawHeaders != nil {
		headersMap, ok := rawHeaders.(map[string]interface{})
		if !ok {
			return nil, makeErr(fmt.Sprintf("`_meta.gateway.headers` must be an object, got %T", rawHeaders))
		}
		if len(headersMap) > 0 {
			headers = make(map[string]string, len(headersMap))
			for k, v := range headersMap {
				if k == "" {
					return nil, makeErr("`_meta.gateway.headers` names must be non-empty strings")
				}
				sv, ok := v.(string)
				if !ok {
					return nil, makeErr(fmt.Sprintf("header %q must map to a string, got %T", k, v))
				}
				headers[k] = sv
			}
		}
	}
	return &GatewayConfig{
		BaseURL: baseURL,
		Headers: headers,
	}, nil
}

// Binary VA: 0x1023331e0 | Size: 288 B | Source: third_party/jetski/cli/acp/auth/config_resolution.go:L174
func loadMDMAdminSettings(adminSettingsPath string, disableDefaultAdminSettings bool) (*admincontrols.AdminSettings, string, error) {
	path := adminSettingsPath
	if path == "" && !disableDefaultAdminSettings {
		path = admincontrols.defaultAdminSettingsPathForOS("darwin", os.Getenv) /* inferred: unexported in binary via go:linkname or package-internal */
	}
	if path == "" {
		return nil, "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, path, nil
		}
		return nil, path, err
	}
	settings, err := admincontrols.ParseAdminSettings(data)
	return settings, path, err
}

// Binary VA: 0x102333300 | Size: 144 B | Source: third_party/jetski/cli/acp/auth/config_resolution.go:L196
func resolveAdminGatewayDisposition(adminSettingsPath string, disableDefaultAdminSettings bool) adminGatewayDisposition {
	settings, _, err := loadMDMAdminSettings(adminSettingsPath, disableDefaultAdminSettings)
	if err != nil || settings == nil || settings.Gateway == nil || settings.Gateway.Enabled == nil {
		return adminGatewayUnset
	}
	if *settings.Gateway.Enabled {
		return adminGatewayRequired
	}
	return adminGatewayDisabled
}

// Binary VA: 0x102333390 | Size: 1744 B | Source: third_party/jetski/cli/acp/auth/config_resolution.go:L210
func resolveGateway(adminSettingsPath string, disableDefaultAdminSettings bool, metaGateway *GatewayConfig, allowMetaOnly bool) (adminGatewayDisposition, *GatewayConfig, string, error) {
	adminSettings, resolvedPath, err := loadMDMAdminSettings(adminSettingsPath, disableDefaultAdminSettings)
	if err != nil {
		return adminGatewayUnset, nil, resolvedPath, fmt.Errorf("failed to load admin_settings.json (%s): %w", resolvedPath, err)
	}
	var disp adminGatewayDisposition
	if adminSettings != nil && adminSettings.Gateway != nil && adminSettings.Gateway.Enabled != nil {
		if *adminSettings.Gateway.Enabled {
			disp = adminGatewayRequired
		} else {
			disp = adminGatewayDisabled
		}
	}
	if disp == adminGatewayDisabled {
		return adminGatewayDisabled, nil, resolvedPath, nil
	}

	envSettings := admincontrols.ParseEnvOverrides(os.Getenv)

	var metaSettings *admincontrols.AdminSettings
	if metaGateway != nil {
		enabled := true
		var clonedHeaders map[string]string
		if len(metaGateway.Headers) > 0 {
			clonedHeaders = maps.Clone(metaGateway.Headers)
		}
		metaSettings = &admincontrols.AdminSettings{
			CACertPath: metaGateway.CACertPath,
			Gateway: &admincontrols.GatewayConfig{
				Enabled:       &enabled,
				URL:           metaGateway.BaseURL,
				WireProtocol:  metaGateway.WireProtocol,
				CustomHeaders: clonedHeaders,
			},
		}
	}

	merged := admincontrols.MergeSettings(adminSettings, metaSettings, envSettings)
	if !allowMetaOnly && (merged.Gateway == nil || merged.Gateway.Enabled == nil || !*merged.Gateway.Enabled) {
		return disp, nil, resolvedPath, nil
	}
	if merged.Gateway == nil {
		merged.Gateway = &admincontrols.GatewayConfig{}
	}
	enabled := true
	merged.Gateway.Enabled = &enabled

	if strings.TrimSpace(merged.Gateway.URL) == "" {
		return disp, nil, resolvedPath, errors.New("gateway URL is empty")
	}
	if metaGateway == nil || adminSettings == nil {
		if err := merged.Validate(); err != nil {
			return disp, nil, resolvedPath, err
		}
	}

	apiKey := resolveWinningGatewayAPIKey(adminSettings, metaSettings, envSettings)
	var proxyURL string
	if merged.Proxy != nil {
		proxyURL = merged.Proxy.URL
	}
	var headers map[string]string
	if len(merged.Gateway.CustomHeaders) > 0 {
		headers = make(map[string]string, len(merged.Gateway.CustomHeaders))
		for k, v := range merged.Gateway.CustomHeaders {
			if strings.EqualFold(k, "user-agent") {
				continue
			}
			headers[k] = v
		}
	}
	var routeWebFetch bool
	if merged.Gateway.RouteWebFetch != nil {
		routeWebFetch = *merged.Gateway.RouteWebFetch
	}
	return disp, &GatewayConfig{
		BaseURL:       merged.Gateway.URL,
		Headers:       headers,
		APIKey:        apiKey,
		WireProtocol:  merged.Gateway.WireProtocol,
		CACertPath:    merged.CACertPath,
		ProxyURL:      proxyURL,
		RouteWebFetch: routeWebFetch,
	}, resolvedPath, nil
}

// Binary VA: 0x102333a60 | Size: 272 B | Source: third_party/jetski/cli/acp/auth/config_resolution.go:L300
func resolveWinningGatewayAPIKey(sources ...*admincontrols.AdminSettings) string {
	for _, s := range sources {
		if s == nil || s.Gateway == nil {
			continue
		}
		for k := range s.Gateway.CustomHeaders {
			if strings.EqualFold(k, "authorization") {
				return ""
			}
		}
		if s.Gateway.APIKey != "" {
			return s.Gateway.APIKey
		}
	}
	return ""
}

// persistedACPAuthState is the JSON schema for acp-auth-state.json.
// Binary Type VA: 0x10a1b1b88
type persistedACPAuthState struct {
	MethodID string `json:"methodId"`
}

// Binary VA: 0x102333b70 | Size: 160 B | Source: third_party/jetski/cli/acp/auth/config_resolution.go:L325
func acpAuthStateFilePath(appDataDir string) string {
	if !filepath.IsAbs(appDataDir) {
		return ""
	}
	return filepath.Join(appDataDir, "cache", "acp-auth-state.json")
}

// Binary VA: 0x102333c10 | Size: 400 B | Source: third_party/jetski/cli/acp/auth/config_resolution.go:L332
func loadPersistedMethod(appDataDir string) string {
	path := acpAuthStateFilePath(appDataDir)
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var state persistedACPAuthState
	if err := json.Unmarshal(data, &state); err != nil {
		return ""
	}
	return normalizeAuthMethod(strings.TrimSpace(state.MethodID))
}

// Binary VA: 0x102333da0 | Size: 464 B | Source: third_party/jetski/cli/acp/auth/config_resolution.go:L350
func RememberMethod(appDataDir string, methodID string) error {
	if methodID == "" || methodID == "gateway" {
		return nil
	}
	path := acpAuthStateFilePath(appDataDir)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.Marshal(persistedACPAuthState{
		MethodID: normalizeAuthMethod(methodID),
	})
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// Binary VA: 0x102333f70 | Size: 96 B | Source: third_party/jetski/cli/acp/auth/config_resolution.go:L369
func clearPersistedMethod(appDataDir string) error {
	path := acpAuthStateFilePath(appDataDir)
	if path == "" {
		return nil
	}
	return os.Remove(path)
}

// Binary VA: 0x102333fd0 | Size: 768 B | Source: third_party/jetski/cli/acp/auth/config_resolution.go:L381
func inferSilentMethod(settingsData []byte, isJetski bool, appDataDir string, adminSettingsPath string, disableDefaultAdminSettings bool) string {
	parsed := parseSettingsBytes(settingsData)
	method := normalizeAuthMethod(parsed.authType)
	if method == "" && parsed.authSelectedType == "gemini" {
		method = "gemini-api-key"
	}

	usable := true
	switch method {
	case "":
		usable = false
	case "gateway":
		_, gwCfg, _, err := resolveGateway(adminSettingsPath, disableDefaultAdminSettings, nil, true)
		usable = err == nil && gwCfg != nil
	case "agent-platform":
		vc := resolveVertexConfig(settingsData)
		usable = vc.IsConfigured()
	case "gemini-api-key":
		usable = os.Getenv("GEMINI_API_KEY") != ""
	}
	if usable {
		return method
	}
	return cmp.Or(loadPersistedMethod(appDataDir), method)
}

// Binary VA: 0x1023342d0 | Size: 704 B | Source: third_party/jetski/cli/acp/auth/config_resolution.go:L451
func resolveFallbackSettingsData(appDataDir string, cliDirName string, isJetski bool) []byte {
	if !filepath.IsAbs(appDataDir) {
		return nil
	}
	var candidates []string
	if cliDirName != "" {
		candidates = []string{filepath.Join(appDataDir, cliDirName, "settings.json")}
	} else if isJetski {
		candidates = []string{
			filepath.Join(appDataDir, "cli", "settings.json"),
			filepath.Join(appDataDir, "settings.json"),
		}
	} else {
		candidates = []string{
			filepath.Join(appDataDir, "settings.json"),
			filepath.Join(appDataDir, "cli", "settings.json"),
		}
	}
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err == nil {
			return data
		}
	}
	return nil
}

// Binary VA: 0x102334590 | Size: 240 B | Source: third_party/jetski/cli/acp/auth/config_resolution.go:L477
func readSettingsBytes(isJetski bool, appDataDir string, cliDirName string, settingsPath string, settingsData []byte) []byte {
	if len(settingsData) > 0 {
		return settingsData
	}
	if settingsPath != "" {
		if data, err := os.ReadFile(settingsPath); err == nil {
			return data
		}
	}
	return resolveFallbackSettingsData(appDataDir, cliDirName, isJetski)
}

// Binary VA: 0x102334680 | Size: 3696 B | Source: third_party/jetski/cli/acp/auth/config_resolution.go:L493
func ResolveBackendSelection(
	isJetski bool,
	appDataDir string,
	cliDirName string,
	settingsPath string,
	settingsData []byte,
	adminSettingsPath string,
	disableDefaultAdminSettings bool,
	gatewayEnabled bool,
	params *schema.AuthenticateParams,
) (BackendSelection, error) {
	if isJetski {
		return BackendSelection{
			Mode:     "ccpa",
			MethodID: "oauth-personal",
		}, nil
	}

	data := readSettingsBytes(isJetski, appDataDir, cliDirName, settingsPath, settingsData)

	if params != nil {
		methodID := normalizeAuthMethod(params.MethodID)
		disp := resolveAdminGatewayDisposition(adminSettingsPath, disableDefaultAdminSettings)
		if disp == adminGatewayRequired && methodID != "gateway" {
			return BackendSelection{}, &Error{
				Code:    -32602, // -0x7f5a
				Message: fmt.Sprintf("Auth method %q is not permitted: enterprise policy requires %q.", params.MethodID, "gateway"),
			}
		}

		switch methodID {
		case "gateway":
			var metaGW *GatewayConfig
			if rawGW, ok := params.Meta["gateway"]; ok && rawGW != nil {
				var err error
				metaGW, err = parseGatewayMeta(rawGW)
				if err != nil {
					return BackendSelection{}, err
				}
			}
			_, gwCfg, _, err := resolveGateway(adminSettingsPath, disableDefaultAdminSettings, metaGW, true)
			if err != nil {
				return BackendSelection{}, &Error{
					Code:    -32602, // -0x7f5a
					Message: fmt.Sprintf("Gateway authentication could not be configured: %v", err),
				}
			}
			return BackendSelection{
				Mode:                "gateway",
				MethodID:            "gateway",
				UseSnapshotMendelFP: true,
				DisableTelemetry:    true,
				Gateway:             gwCfg,
			}, nil

		case "agent-platform":
			vc := resolveVertexConfig(data)
			return BackendSelection{
				Mode:     "adc",
				MethodID: "agent-platform",
				Vertex:   vc,
			}, nil

		case "gemini-api-key":
			apiKey := ExtractGeminiAPIKey(params.Meta)
			return BackendSelection{
				Mode:                "gemini-api",
				MethodID:            "gemini-api-key",
				UseGeminiAPIKeyAuth: true,
				UseSnapshotMendelFP: true,
				GeminiAPIKey:        apiKey,
			}, nil

		default:
			return BackendSelection{
				Mode:     "ccpa",
				MethodID: methodID,
			}, nil
		}
	}

	disp, gwCfg, _, gwErr := resolveGateway(adminSettingsPath, disableDefaultAdminSettings, nil, false)
	if disp == adminGatewayRequired {
		if gwErr != nil || gwCfg == nil {
			return BackendSelection{
				Mode:                "gateway",
				MethodID:            "gateway",
				UseSnapshotMendelFP: true,
				DisableTelemetry:    true,
			}, nil
		}
		return BackendSelection{
			Mode:                "gateway",
			MethodID:            "gateway",
			UseSnapshotMendelFP: true,
			DisableTelemetry:    true,
			Gateway:             gwCfg,
		}, nil
	}

	silent := inferSilentMethod(data, isJetski, appDataDir, adminSettingsPath, disableDefaultAdminSettings)
	switch silent {
	case "gateway":
		if disp != adminGatewayDisabled {
			_, gwCfg, _, err := resolveGateway(adminSettingsPath, disableDefaultAdminSettings, nil, true)
			if err == nil && gwCfg != nil {
				return BackendSelection{
					Mode:                "gateway",
					MethodID:            "gateway",
					UseSnapshotMendelFP: true,
					DisableTelemetry:    true,
					Gateway:             gwCfg,
				}, nil
			}
		}
	case "agent-platform":
		vc := resolveVertexConfig(data)
		if vc.IsConfigured() {
			return BackendSelection{
				Mode:     "adc",
				MethodID: "agent-platform",
				Vertex:   vc,
			}, nil
		}
	case "gemini-api-key":
		if apiKey := os.Getenv("GEMINI_API_KEY"); apiKey != "" {
			return BackendSelection{
				Mode:                "gemini-api",
				MethodID:            "gemini-api-key",
				UseGeminiAPIKeyAuth: true,
				UseSnapshotMendelFP: true,
				GeminiAPIKey:        apiKey,
			}, nil
		}
	case "oauth-business", "oauth-personal":
		return BackendSelection{
			Mode:     "ccpa",
			MethodID: silent,
		}, nil
	}

	if env.IsOnOrDefault("AGY_ADC_AUTH", false) {
		vc := resolveVertexConfig(data)
		return BackendSelection{
			Mode:     "adc",
			MethodID: "agent-platform",
			Vertex:   vc,
		}, nil
	}
	if disp != adminGatewayDisabled && os.Getenv("AGY_LLM_GATEWAY_URL") != "" {
		_, gwCfg, _, err := resolveGateway(adminSettingsPath, disableDefaultAdminSettings, nil, true)
		if err == nil && gwCfg != nil {
			return BackendSelection{
				Mode:                "gateway",
				MethodID:            "gateway",
				UseSnapshotMendelFP: true,
				DisableTelemetry:    true,
				Gateway:             gwCfg,
			}, nil
		}
	}
	_ = constants.GetEnvProject /* inferred: constants import if needed */
	return BackendSelection{
		Mode: "ccpa",
	}, nil
}
