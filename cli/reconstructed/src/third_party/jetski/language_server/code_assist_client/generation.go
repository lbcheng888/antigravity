// Binary source reconstruction of agy v1.5.10
// Source: third_party/jetski/language_server/code_assist_client/generation.go
// Package: codeassistclient (google3/third_party/jetski/language_server/code_assist_client/codeassistclient)
package codeassistclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Binary VA: 0x101c1c340 | Size: 592 B | Source: third_party/jetski/language_server/code_assist_client/generation.go:L1145
func computeRequestID(parts ...any) string {
	switch len(parts) {
	case 5:
		return fmt.Sprintf("%v/%v/%v/%v/%v", parts[0], parts[1], parts[2], parts[3], parts[4])
	case 4:
		return fmt.Sprintf("%v/%v/%v/%v", parts[0], parts[1], parts[2], parts[3])
	case 2:
		return fmt.Sprintf("%v/%v", parts[0], parts[1])
	default:
		return fmt.Sprint(parts...)
	}
}

// Binary VA: 0x101c1c590 | Size: 1728 B | Source: third_party/jetski/language_server/code_assist_client/generation.go:L1166
// Routes request across Gateway -> BusinessAICode -> AICode -> VertexAI -> CCPA fallback.
func (c *CodeAssistClient) StreamGenerateContent(ctx context.Context, payload []byte, modelName string) (*http.Response, error) {
	// Decision tree verified at 0x101c1c590..0x101c1cc50:
	// 1. If Gateway auth -> StreamGenerateContentFromGateway
	// 2. If BusinessAICode enabled -> StreamGenerateContentFromBusinessAICode
	// 3. If useAICodePredictionFlow() && !isInAICodeScopeCooldown() -> StreamGenerateContentFromAICode
	// 4. If VertexAI -> StreamGenerateContentFromVertexAI
	// 5. Default -> streamGenerateContentWithContinuationFromCCPA -> streamGenerateContentFromCCPA
	return c.streamGenerateContentFromCCPA(ctx, payload)
}

// Binary VA: 0x101c1cd40 | Size: 2016 B | Source: third_party/jetski/language_server/code_assist_client/generation.go:L1246
func (c *CodeAssistClient) streamGenerateContentFromCCPA(ctx context.Context, jsonBody []byte) (*http.Response, error) {
	endpoint := c.AuthProvider.GetEndpointURL(ctx)
	targetURL, err := c.constructCodeAssistEndpointWithParams(endpoint, "streamGenerateContent", map[string]string{"alt": "sse"})
	if err != nil {
		return nil, err
	}
	httpClient, err := c.AuthProvider.GetHTTPClient(ctx, nil)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	return httpClient.Do(req)
}

// Binary VA: 0x101c1e640 | Size: 1040 B | Source: third_party/jetski/language_server/code_assist_client/generation.go:L1603
func (c *CodeAssistClient) generateContentFromCCPA(ctx context.Context, jsonBody []byte) ([]byte, error) {
	endpoint := c.AuthProvider.GetEndpointURL(ctx)
	targetURL := c.constructCodeAssistEndpoint(endpoint, "generateContent")
	httpClient, err := c.AuthProvider.GetHTTPClient(ctx, nil)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("failed to generate content: %s, body: %s", resp.Status, string(raw))
	}
	return raw, nil
}

// Binary VA: 0x101c1fe50 | Size: 2512 B | Source: third_party/jetski/language_server/code_assist_client/generation.go:L1924
func (c *CodeAssistClient) StreamGenerateContentFromGateway(ctx context.Context, baseURL, modelName string, openAI bool, body []byte) (*http.Response, error) {
	if modelName == "" {
		return nil, fmt.Errorf("could not determine model for Gateway request: model name is empty")
	}
	trimmed := strings.TrimRight(baseURL, "/")
	var targetURL string
	if openAI {
		if strings.HasSuffix(trimmed, "/chat/completions") {
			targetURL = trimmed
		} else if strings.HasSuffix(trimmed, "/v1") {
			targetURL = trimmed + "/chat/completions"
		} else {
			targetURL = trimmed + "/v1/chat/completions"
		}
	} else {
		targetURL = fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse", trimmed, modelName)
	}
	httpClient, err := c.AuthProvider.GetHTTPClient(ctx, nil)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	return httpClient.Do(req)
}
