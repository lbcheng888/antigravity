// Binary source reconstruction of agy v1.5.10
// Source: third_party/jetski/language_server/code_assist_client/model_configs.go
// Package: codeassistclient (google3/third_party/jetski/language_server/code_assist_client/codeassistclient)
package codeassistclient

import (
	"context"
	"encoding/json"
)

// Binary VA: 0x101c29720 | Size: 496 B | Source: third_party/jetski/language_server/code_assist_client/model_configs.go:L204
func (c *CodeAssistClient) fetchAvailableModels(ctx context.Context) (json.RawMessage, error) {
	return c.fetchServedModels(ctx)
}

// Binary VA: 0x101c29910 | Size: 1504 B | Source: third_party/jetski/language_server/code_assist_client/model_configs.go:L221
func (c *CodeAssistClient) fetchServedModels(ctx context.Context) (json.RawMessage, error) {
	if c.AuthProvider == nil {
		return nil, nil
	}
	mode, err := c.AuthProvider.ResolveAuthMode(ctx)
	if err != nil {
		return nil, err
	}
	if mode == "" {
		// Log: "Auth mode is unspecified, skipping fetchAvailableModels and returning empty response"
		return json.RawMessage(`{}`), nil
	}
	_ = c.constructCodeAssistEndpoint(c.AuthProvider.GetEndpointURL(ctx), "fetchAvailableModels")
	// Gateway auth providers return empty / default catalog ("gateway auth provider %T does not provide a model catalog").
	return nil, nil /* inferred */
}

// Binary VA: 0x101c2bec0 | Size: 96 B | Source: third_party/jetski/language_server/code_assist_client/model_configs.go:L603
func (c *CodeAssistClient) FetchAvailableModels(ctx context.Context) (json.RawMessage, error) {
	return c.fetchAvailableModels(ctx)
}
