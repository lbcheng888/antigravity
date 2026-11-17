// Binary source reconstruction of agy v1.5.10
// Source: third_party/jetski/language_server/code_assist_client/code_assist_client.go
// Package: codeassistclient (google3/third_party/jetski/language_server/code_assist_client/codeassistclient)
package codeassistclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Binary VA: 0x10a71b250 | Size: 16 B | Type: codeassistclient.AuthProvider
type AuthProvider interface {
	GetAuthMethod() string
	GetCAICProject(context.Context) (string, error)
	GetEndpointURL(context.Context) string
	GetEntitlement(context.Context) (*Entitlement, error)
	GetGCPLocation(context.Context) (string, error)
	GetHTTPClient(context.Context, func()) (*http.Client, error)
	GetTokenSource(context.Context) (TokenSource, bool, error)
	GetUserAgentName() string
	GetUserTier() (string, string)
	GetWIFProvider() string
	IsBusinessEnterprise(context.Context) bool
	IsBusinessPayGo(context.Context) (bool, error)
	IsEval() bool
	ResolveAuthMode(context.Context) (AuthMode, error)
	SetEndpointURL(string)
	SetHTTPHeaders(*http.Request)
	SetUserTier(string, string)
	ShouldUseCAICProjectAsOverride() bool
}

type AuthMode string
type Entitlement struct {
	Parent string /* inferred */
}
type TokenSource interface {
	Token() (any, error)
}

// Binary VA: 0x10aa57bf8 | Size: 608 B | Type: codeassistclient.CodeAssistClient
type CodeAssistClient struct {
	AuthProvider              AuthProvider
	getOAuthTokenMu           sync.Mutex
	isGoogleInternal          bool
	aicodeScopeMu             sync.Mutex
	aicodeScopeCooldownUntil  time.Time
	aicodeScopeMissing        bool
	authGen                   uint64
	authEpoch                 atomic.Uint64
	// [unrecovered] additional cache.Cache fields at +0x18..+0x240 documented in go-types/types_recovered.go
}

// Binary VA: 0x101c0a730 | Size: 1664 B | Source: third_party/jetski/language_server/code_assist_client/code_assist_client.go:L177
// Initializes the 8 singleflight/cache instances: availableModels, peopleInfo, userInfo,
// loadCodeAssistResponse, fetchAdminControls, fetchBAICAdminControls, retrieveUserQuotaSummary, aicodeConfig.
func NewCodeAssistClient(authProvider AuthProvider) *CodeAssistClient {
	return &CodeAssistClient{
		AuthProvider: authProvider,
	}
}

// Binary VA: 0x101c0bb10 | Size: 528 B | Source: third_party/jetski/language_server/code_assist_client/code_assist_client.go:L377
func (c *CodeAssistClient) GetOAuthToken(ctx context.Context) (any, error) {
	c.getOAuthTokenMu.Lock()
	defer c.getOAuthTokenMu.Unlock()
	ts, ok, err := c.AuthProvider.GetTokenSource(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting token source from auth provider: %w", err)
	}
	if !ok || ts == nil {
		return nil, errors.New("error getting token source from auth provider")
	}
	tok, err := ts.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to compute token: %w", err)
	}
	return tok, nil
}

// Binary VA: 0x101c3cc20 | Size: 3552 B | Source: third_party/jetski/language_server/code_assist_client/user_management.go:L151
func (c *CodeAssistClient) FetchLoadCodeAssistResponse(ctx context.Context, reqBody []byte) (json.RawMessage, error) {
	_ = c.constructCodeAssistEndpoint(c.AuthProvider.GetEndpointURL(ctx), "loadCodeAssist")
	// Falls through FetchLicensesFromAICode -> POST /v1internal:loadCodeAssist
	return nil, nil /* inferred */
}

// Binary VA: 0x101c40300 | Size: 704 B | Source: third_party/jetski/language_server/code_assist_client/user_management.go:L895
func (c *CodeAssistClient) RetrieveUserQuotaSummary(ctx context.Context) (json.RawMessage, error) {
	_ = c.constructCodeAssistEndpoint(c.AuthProvider.GetEndpointURL(ctx), "retrieveUserQuotaSummary")
	return nil, nil /* inferred */
}
