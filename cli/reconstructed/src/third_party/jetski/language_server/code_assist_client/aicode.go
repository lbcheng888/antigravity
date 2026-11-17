// Binary source reconstruction of agy v1.5.10
// Source: third_party/jetski/language_server/code_assist_client/aicode.go
// Package: codeassistclient (google3/third_party/jetski/language_server/code_assist_client/codeassistclient)
package codeassistclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Binary VA: 0x101beff30 | Size: 448 B | Source: third_party/jetski/language_server/code_assist_client/aicode.go:L34
func (c *CodeAssistClient) aicodeEntitlement(ctx context.Context) (*Entitlement, error) {
	ent, err := c.AuthProvider.GetEntitlement(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get the user tier: %w", err)
	}
	if ent == nil {
		return nil, fmt.Errorf("LoadCodeAssist response has no paid tier")
	}
	return ent, nil
}

// Binary VA: 0x101bf0210 | Size: 2160 B | Source: third_party/jetski/language_server/code_assist_client/aicode.go:L85
func (c *CodeAssistClient) StreamGenerateContentFromAICode(ctx context.Context, payload []byte) (*http.Response, error) {
	ent, err := c.aicodeEntitlement(ctx)
	if err != nil {
		return nil, err
	}
	_ = ent
	baseURL := c.resolveAICodeBaseURL(c.AuthProvider.GetEndpointURL(ctx))
	// Target URL pattern: fmt.Sprintf("%s/%s/%s:%s", baseURL, "v1alpha", ent.Parent, "streamGenerateContent") + ?alt=sse
	rawURL := fmt.Sprintf("%s/%s:%s", baseURL+"/"+AICodeAPIVersion, "locations/global" /* inferred parent from Entitlement */, "streamGenerateContent")
	targetURL, err := addParamsToURL(rawURL, map[string]string{"alt": "sse"})
	if err != nil {
		return nil, err
	}
	httpClient, err := c.AuthProvider.GetHTTPClient(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get HTTP client: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytesReader(payload))
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	return httpClient.Do(req)
}

// Binary VA: 0x101bf0a80 | Size: 2544 B | Source: third_party/jetski/language_server/code_assist_client/aicode.go:L153
func (c *CodeAssistClient) GenerateContentFromAICode(ctx context.Context, payload []byte) ([]byte, error) {
	if _, err := c.aicodeEntitlement(ctx); err != nil {
		return nil, err
	}
	baseURL := c.resolveAICodeBaseURL(c.AuthProvider.GetEndpointURL(ctx))
	targetURL := fmt.Sprintf("%s/%s/%s:%s", baseURL, AICodeAPIVersion, "locations/global" /* inferred */, "generateContent")
	httpClient, err := c.AuthProvider.GetHTTPClient(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get HTTP client: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytesReader(payload))
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make AI Code GenerateContent request: %s, body: %s", err, "")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Checks body for "ACCESS_TOKEN_SCOPE_INSUFFICIENT" -> markAICodeScopeMissing
		return nil, fmt.Errorf("failed to make AI Code GenerateContent request: %s, body: %s", resp.Status, string(body))
	}
	return body, nil
}

// Binary VA: 0x101bf2690 | Size: 2480 B | Source: third_party/jetski/language_server/code_assist_client/aicode.go:L347
func (c *CodeAssistClient) FetchLicensesFromAICode(ctx context.Context) (json.RawMessage, error) {
	baseURL := c.resolveAICodeBaseURL(c.AuthProvider.GetEndpointURL(ctx))
	targetURL := fmt.Sprintf("%s/%s/%s:%s", baseURL, AICodeAPIVersion, "" /* inferred */, "fetchLicenses")
	httpClient, err := c.AuthProvider.GetHTTPClient(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get HTTP client: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create FetchLicenses request: %w", err)
	}
	c.setHeaders(req)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make AI Code FetchLicenses request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read AI Code FetchLicenses response body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Failed to make AI Code FetchLicenses request (%s): %s", resp.Status, string(raw))
	}
	return raw, nil
}

// Binary VA: 0x101bf3310 | Size: 2000 B | Source: third_party/jetski/language_server/code_assist_client/aicode.go:L455
func (c *CodeAssistClient) FetchQuotaStatusFromAICode(ctx context.Context) (json.RawMessage, error) {
	baseURL := c.resolveAICodeBaseURL(c.AuthProvider.GetEndpointURL(ctx))
	targetURL := fmt.Sprintf("%s/%s/%s:%s", baseURL, AICodeAPIVersion, "" /* inferred */, "fetchQuotaStatus")
	httpClient, err := c.AuthProvider.GetHTTPClient(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("getting HTTP client: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating FetchQuotaStatus request: %w", err)
	}
	c.setHeaders(req)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("making AI Code FetchQuotaStatus request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading AICode FetchQuotaStatus response body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("AICode FetchQuotaStatus request returned status %q, body: %q", resp.Status, string(raw))
	}
	return raw, nil
}

func bytesReader(b []byte) io.Reader { /* inferred helper */ return nil }
