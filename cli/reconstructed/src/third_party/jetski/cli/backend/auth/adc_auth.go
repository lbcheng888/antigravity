// Binary VA: 0x101d3ea20 | Size: 2688 B (7 funcs) | Source: third_party/jetski/cli/backend/auth/adc_auth.go
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google3/base/go/log"
)

// Binary VA: 0x10a1085a8 | Size: 16 B | Type: auth.ADCStorage
type ADCStorage interface {
	LoadCredentials() (*google.Credentials, error)
}

// Binary VA: 0x10a300160 | Size: 32 B | Type: auth.adcAuth
type adcAuth struct {
	storage ADCStorage
	ctx     context.Context
}

// Binary VA: 0x10a1adcb8 | Size: 16 B | Type: auth.adcFile
type adcFile struct {
	QuotaProjectID string `json:"quota_project_id"`
}

// Binary VA: 0x10a1add30 | Size: 16 B | Type: auth.adcUserInfoResponse
type adcUserInfoResponse struct {
	Email string `json:"email"`
}

// Binary VA: 0x101d3ea20 | Size: 80 B | Source: third_party/jetski/cli/backend/auth/adc_auth.go:L31
func (a *adcAuth) TryAuth(ctx context.Context) *AuthResult {
	res, _ := a.Authenticate(ctx)
	return res
}

// Binary VA: 0x101d3ea70 | Size: 1584 B | Source: third_party/jetski/cli/backend/auth/adc_auth.go:L36
func (a *adcAuth) Authenticate(ctx context.Context) (*AuthResult, error) {
	creds, err := a.storage.LoadCredentials()
	if err != nil {
		log.ctxlogf(ctx, "adcAuth: failed to load credentials: %v", err)
		return nil, fmt.Errorf("adcAuth: failed to load credentials: %w", err)
	}
	if creds == nil {
		log.ctxlogf(ctx, "adcAuth: no credentials found")
		return nil, fmt.Errorf("adcAuth: no credentials found")
	}
	tok, err := creds.TokenSource.Token()
	if err != nil {
		log.ctxlogf(ctx, "adcAuth: failed to get token from token source: %v", err)
		return nil, fmt.Errorf("adcAuth: failed to get token from token source: %w", err)
	}

	var quotaProject string
	if len(creds.JSON) > 0 {
		var af adcFile
		if err := json.Unmarshal(creds.JSON, &af); err == nil && af.QuotaProjectID != "" {
			quotaProject = af.QuotaProjectID
		}
	}
	if quotaProject == "" {
		if envProj := os.Getenv("GOOGLE_CLOUD_QUOTA_PROJECT"); envProj != "" {
			quotaProject = envProj
		}
	}
	if quotaProject == "" {
		quotaProject = creds.ProjectID
	}

	log.ctxlogf(ctx, "adcAuth: authenticated successfully with quota project %s, token is non-empty: %v", quotaProject, tok != nil && tok.AccessToken != "")
	res := &AuthResult{
		TokenSource:  creds.TokenSource,
		AuthMethod:   "adc",
		QuotaProject: quotaProject,
	}
	if email, err := fetchAdcUserInfo(ctx, creds.TokenSource); err == nil {
		res.Email = email
	} else {
		log.ctxlogf(ctx, "adcAuth: fetching user info failed: %v", err)
	}
	return res, nil
}

// Binary VA: 0x101d3f0a0 | Size: 96 B | Source: third_party/jetski/cli/backend/auth/adc_auth.go:L91
func (a *adcAuth) StartInteractive(ctx context.Context, onComplete func(*AuthResult, error)) string {
	res, err := a.Authenticate(ctx)
	onComplete(res, err)
	return ""
}

// Binary VA: 0x101d3f100 | Size: 16 B | Source: third_party/jetski/cli/backend/auth/adc_auth.go:L97
func (a *adcAuth) Logout() {}

// Binary VA: 0x101d3f110 | Size: 16 B | Source: third_party/jetski/cli/backend/auth/adc_auth.go:L101
func (a *adcAuth) Name() string {
	return "adc"
}

// Binary VA: 0x101d3f120 | Size: 832 B | Source: third_party/jetski/cli/backend/auth/adc_auth.go:L110
func fetchAdcUserInfo(ctx context.Context, ts oauth2.TokenSource) (string, error) {
	client := oauth2.NewClient(ctx, ts)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		return "", fmt.Errorf("userinfo request failed: %w", err)
	}
	// Binary VA: 0x101d451e0 | Size: 64 B | Source: third_party/jetski/cli/backend/auth/adc_auth.go:L116
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("userinfo returned status %d", resp.StatusCode)
	}
	var info adcUserInfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", fmt.Errorf("failed to decode userinfo: %w", err)
	}
	return info.Email, nil
}
