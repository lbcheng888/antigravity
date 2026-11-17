// Binary source reconstruction of agy v1.5.10
// Source: third_party/jetski/language_server/code_assist_client/http_helpers.go
// Package: codeassistclient (google3/third_party/jetski/language_server/code_assist_client/codeassistclient)
package codeassistclient

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
)

const (
	// Binary VA: 0x101c23560 (aicodeBaseURL strings)
	DefaultAICodeBaseURL         = "https://aicode.googleapis.com"
	PreprodAICodeBaseURL         = "https://preprod-aicode.sandbox.googleapis.com"
	PreprodCloudCodeEndpointURL  = "https://preprod-daily-cloudcode-pa.sandbox.googleapis.com"
	DefaultCloudCodeEndpointURL  = "https://daily-cloudcode-pa.googleapis.com"
	DefaultAPIVersion            = "v1internal"
	AICodeAPIVersion             = "v1alpha"
)

// Binary VA: 0x101c23090 | Size: 208 B | Source: third_party/jetski/language_server/code_assist_client/http_helpers.go:L470
func (c *CodeAssistClient) constructCodeAssistEndpoint(endpoint, method string) string {
	base := strings.TrimRight(endpoint, "/")
	return fmt.Sprintf("%s/%s:%s", base, DefaultAPIVersion /* inferred */, method)
}

// Binary VA: 0x101c23160 | Size: 96 B | Source: third_party/jetski/language_server/code_assist_client/http_helpers.go:L474
func (c *CodeAssistClient) constructCodeAssistEndpointWithParams(endpoint, method string, params map[string]string) (string, error) {
	u := c.constructCodeAssistEndpoint(endpoint, method)
	return addParamsToURL(u, params)
}

// Binary VA: 0x101c231c0 | Size: 432 B | Source: third_party/jetski/language_server/code_assist_client/http_helpers.go:L479
func addParamsToURL(rawURL string, params map[string]string) (string, error) {
	if len(params) == 0 {
		return rawURL, nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	q := parsed.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	parsed.RawQuery = q.Encode()
	return parsed.String(), nil
}

// Binary VA: 0x101c23560 | Size: 160 B | Source: third_party/jetski/language_server/code_assist_client/http_helpers.go:L513
func aicodeBaseURL(ccpaEndpoint string) string {
	trimmed := strings.TrimRight(ccpaEndpoint, "/")
	if trimmed == PreprodCloudCodeEndpointURL {
		return PreprodAICodeBaseURL
	}
	return DefaultAICodeBaseURL
}

// Binary VA: 0x101c23600 | Size: 176 B | Source: third_party/jetski/language_server/code_assist_client/http_helpers.go:L523
func (c *CodeAssistClient) resolveAICodeBaseURL(ccpaEndpoint string) string {
	if envURL := os.Getenv("AICODE_ENDPOINT_URL"); envURL != "" {
		return strings.TrimRight(envURL, "/")
	}
	return aicodeBaseURL(ccpaEndpoint)
}

// Binary VA: 0x101c23b00 | Size: 144 B | Source: third_party/jetski/language_server/code_assist_client/http_helpers.go:L593
func (c *CodeAssistClient) userAgent(ctx any /* context.Context */) string {
	_ = ctx
	return userAgent(c.AuthProvider.GetUserAgentName(), c.AuthProvider.GetAuthMethod())
}

// Binary VA: 0x101c23b90 | Size: 2256 B | Source: third_party/jetski/language_server/code_assist_client/http_helpers.go:L599
func userAgent(appName, authMethod string) string {
	if appName == "" {
		appName = "antigravity"
	}
	parts := []string{
		fmt.Sprintf("os_type=%s", runtime.GOOS),
		fmt.Sprintf("arch=%s", runtime.GOARCH),
	}
	if authMethod != "" {
		parts = append(parts, fmt.Sprintf("auth_method=%s", authMethod))
	}
	// [unrecovered] metadata_provider.SafeGetMetadata(), cl=%d, hostclientidentity, requestPurposeKey
	return fmt.Sprintf("%s/%s (%s)", appName, "1.5.10" /* inferred */, strings.Join(parts, "; "))
}

// Binary VA: 0x101c24460 | Size: 848 B | Source: third_party/jetski/language_server/code_assist_client/http_helpers.go:L644
func (c *CodeAssistClient) setHeaders(req *http.Request) {
	if req == nil {
		return
	}
	ctx := req.Context()
	req.Header.Set("User-Agent", c.userAgent(ctx))
	// [inlined] requestPurpose(ctx) -> "X-Goog-Request-Reason" if non-empty
	req.Header.Set("Content-Type", "application/json")
	if c.AuthProvider != nil {
		c.AuthProvider.SetHTTPHeaders(req)
	}
}

// Binary VA: 0x101c24830 | Size: 272 B | Source: third_party/jetski/language_server/code_assist_client/http_helpers.go:L666
func (c *CodeAssistClient) postHTTPRequest(ctx any /* context.Context */, httpClient *http.Client, targetURL string, body any) (*http.Response, error) {
	// [unrecovered] delegates to http.NewRequestWithContext("POST", targetURL, body) -> c.setHeaders(req) -> httpClient.Do(req)
	return nil, nil /* inferred placeholder for signature completeness */
}
