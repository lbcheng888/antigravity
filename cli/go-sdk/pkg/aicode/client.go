package aicode

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"antigravity.dev/agy-sdk/pkg/auth"
)

const (
	DefaultCloudCodeEndpoint         = "https://daily-cloudcode-pa.googleapis.com"
	DefaultCloudCodeFallbackEndpoint = "https://cloudcode-pa.googleapis.com"
	DefaultAICodeEndpoint            = "https://aicode.googleapis.com"
	DefaultGeminiAPIEndpoint         = "https://generativelanguage.googleapis.com"
	DefaultClientVersion             = "1.5.10"
)

// Client calls CloudCode PA, AICode, Gemini Developer API, or an OpenAI-compatible Gateway.
type Client struct {
	Auth              *auth.ChainedAuth
	HTTPClient        *http.Client
	CloudCodeEndpoint string
	AICodeEndpoint    string
	GeminiEndpoint    string
	ClientVersion     string
	StreamBufferSize  int
	StreamIdleTimeout time.Duration
}

// NewClient constructs a Client backed by chainedAuth with default endpoints.
func NewClient(chainedAuth *auth.ChainedAuth) *Client {
	cloudCodeURL := strings.TrimRight(os.Getenv("CLOUDCODE_ENDPOINT_URL"), "/")
	if cloudCodeURL == "" {
		cloudCodeURL = DefaultCloudCodeEndpoint
	}
	aiCodeURL := strings.TrimRight(os.Getenv("AICODE_ENDPOINT_URL"), "/")
	if aiCodeURL == "" {
		aiCodeURL = DefaultAICodeEndpoint
	}
	geminiURL := strings.TrimRight(os.Getenv("GEMINI_API_BASE_URL"), "/")
	if geminiURL == "" {
		geminiURL = DefaultGeminiAPIEndpoint
	}
	return &Client{
		Auth:              chainedAuth,
		HTTPClient:        &http.Client{Timeout: 5 * time.Minute},
		CloudCodeEndpoint: cloudCodeURL,
		AICodeEndpoint:    aiCodeURL,
		GeminiEndpoint:    geminiURL,
		ClientVersion:     DefaultClientVersion,
		StreamBufferSize:  DefaultStreamBufferSize,
		StreamIdleTimeout: DefaultStreamIdleTimeout,
	}
}

// DefaultMetadata builds Antigravity platform metadata matching binary defaults.
func (c *Client) DefaultMetadata() *ClientMetadata {
	ver := c.ClientVersion
	if ver == "" {
		ver = DefaultClientVersion
	}
	return &ClientMetadata{
		IDEType:    "ANTIGRAVITY",
		IDEVersion: ver,
		Platform:   runtimePlatform(),
		PluginType: "CLOUD_CODE",
		IDEName:    "antigravity",
	}
}

// UserAgent formats the standard Antigravity User-Agent header.
func (c *Client) UserAgent(authMethod auth.AuthKind) string {
	ver := c.ClientVersion
	if ver == "" {
		ver = DefaultClientVersion
	}
	method := string(authMethod)
	if method == "" {
		method = "unknown"
	}
	return fmt.Sprintf("antigravity/%s (os_type=%s; arch=%s; auth_method=%s)", ver, runtime.GOOS, runtime.GOARCH, method)
}

func runtimePlatform() string {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64":
		return "DARWIN_ARM64"
	case "darwin/amd64":
		return "DARWIN_AMD64"
	case "linux/amd64":
		return "LINUX_AMD64"
	case "linux/arm64":
		return "LINUX_ARM64"
	default:
		return strings.ToUpper(runtime.GOOS + "_" + runtime.GOARCH)
	}
}

func newRequestID(prefix string) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(b[:]))
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

// resolveCreds resolves credentials via ChainedAuth.
func (c *Client) resolveCreds(ctx context.Context) (*auth.Credentials, error) {
	if c.Auth == nil {
		return nil, fmt.Errorf("aicode client has nil Auth")
	}
	return c.Auth.Resolve(ctx)
}

// LoadCodeAssist queries tier/project entitlement and persists discovered metadata when Storage exists.
func (c *Client) LoadCodeAssist(ctx context.Context, projectID string) (*LoadCodeAssistResponse, error) {
	creds, err := c.resolveCreds(ctx)
	if err != nil {
		return nil, err
	}
	if projectID == "" {
		projectID = creds.ProjectID
	}
	reqBody := &LoadCodeAssistRequest{
		CloudAICompanionProject: projectID,
		Metadata:                c.DefaultMetadata(),
	}
	var out LoadCodeAssistResponse
	if err := c.doJSON(ctx, creds, http.MethodPost, c.cloudCodeURL("v1internal", "loadCodeAssist", nil), reqBody, &out); err != nil {
		return nil, err
	}
	if c.Auth != nil && c.Auth.Storage != nil {
		if out.CloudAICompanionProject != "" {
			_ = c.Auth.Storage.SaveProjectAndRegion(out.CloudAICompanionProject, creds.Region)
		}
		if tier := out.EffectiveTier(); tier != nil && tier.ID != "" {
			_ = c.Auth.Storage.SaveUserTier(tier.ID, tier.Name)
		}
	}
	return &out, nil
}

// FetchAvailableModels retrieves served models and per-model quota state.
func (c *Client) FetchAvailableModels(ctx context.Context, projectID string) (*FetchAvailableModelsResponse, error) {
	creds, err := c.resolveCreds(ctx)
	if err != nil {
		return nil, err
	}
	if projectID == "" {
		projectID = creds.ProjectID
	}
	reqBody := &FetchAvailableModelsRequest{
		Project:   projectID,
		RequestID: newRequestID("models"),
	}
	var out FetchAvailableModelsResponse
	if err := c.doJSON(ctx, creds, http.MethodPost, c.cloudCodeURL("v1internal", "fetchAvailableModels", nil), reqBody, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RetrieveUserQuotaSummary retrieves grouped quota buckets for the active project.
func (c *Client) RetrieveUserQuotaSummary(ctx context.Context, projectID string) (*RetrieveUserQuotaSummaryResponse, error) {
	creds, err := c.resolveCreds(ctx)
	if err != nil {
		return nil, err
	}
	if projectID == "" {
		projectID = creds.ProjectID
	}
	reqBody := &RetrieveUserQuotaSummaryRequest{Project: projectID}
	var out RetrieveUserQuotaSummaryResponse
	if err := c.doJSON(ctx, creds, http.MethodPost, c.cloudCodeURL("v1internal", "retrieveUserQuotaSummary", nil), reqBody, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GenerateContent performs a unary non-streaming generation request routed by active auth kind.
func (c *Client) GenerateContent(ctx context.Context, model string, gemReq *GeminiRequest) (*GenerateContentResponse, error) {
	creds, err := c.resolveCreds(ctx)
	if err != nil {
		return nil, err
	}
	if gemReq == nil {
		return nil, fmt.Errorf("gemini request must not be nil")
	}

	switch creds.Kind {
	case auth.AuthKindGeminiAPIKey:
		endpoint := c.geminiURL(model, "generateContent", nil)
		var inner InnerGenerateContentResponse
		if err := c.doJSON(ctx, creds, http.MethodPost, endpoint, gemReq, &inner); err != nil {
			return nil, err
		}
		return &GenerateContentResponse{Response: &inner}, nil

	case auth.AuthKindGateway:
		// Use SSE streaming or non-streaming /chat/completions and coalesce into one response.
		var acc GenerateContentResponse
		acc.Response = &InnerGenerateContentResponse{Candidates: []*Candidate{{Content: &Content{Role: "model"}}}}
		err := c.StreamGenerateContent(ctx, model, gemReq, func(ch *SSEChunk) error {
			if ch.Response == nil {
				return nil
			}
			for _, cand := range ch.Response.Candidates {
				if cand.Content != nil {
					acc.Response.Candidates[0].Content.Parts = append(acc.Response.Candidates[0].Content.Parts, cand.Content.Parts...)
				}
				if cand.FinishReason != "" {
					acc.Response.Candidates[0].FinishReason = cand.FinishReason
				}
			}
			if ch.Response.UsageMetadata != nil {
				acc.Response.UsageMetadata = ch.Response.UsageMetadata
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &acc, nil

	default:
		reqBody := c.wrapCloudCodeRequest(creds, model, gemReq)
		var out GenerateContentResponse
		if err := c.doJSON(ctx, creds, http.MethodPost, c.cloudCodeURL("v1internal", "generateContent", nil), reqBody, &out); err != nil {
			return nil, err
		}
		return &out, nil
	}
}

// StreamGenerateContent executes an SSE generation stream routed to CloudCode PA, Gemini v1beta, or Gateway.
func (c *Client) StreamGenerateContent(ctx context.Context, model string, gemReq *GeminiRequest, onChunk func(*SSEChunk) error) error {
	creds, err := c.resolveCreds(ctx)
	if err != nil {
		return err
	}
	if gemReq == nil {
		return fmt.Errorf("gemini request must not be nil")
	}

	var targetURL string
	var payload any
	var format StreamFormat

	switch creds.Kind {
	case auth.AuthKindGeminiAPIKey:
		targetURL = c.geminiURL(model, "streamGenerateContent", url.Values{"alt": []string{"sse"}})
		payload = gemReq
		format = StreamFormatGeminiV1Beta
	case auth.AuthKindGateway:
		targetURL = strings.TrimRight(creds.GatewayURL, "/") + "/chat/completions"
		payload = toOpenAIChatRequest(model, gemReq, true)
		format = StreamFormatOpenAIGateway
	default:
		targetURL = c.cloudCodeURL("v1internal", "streamGenerateContent", url.Values{"alt": []string{"sse"}})
		payload = c.wrapCloudCodeRequest(creds, model, gemReq)
		format = StreamFormatCloudCode
	}

	resp, err := c.sendRequest(ctx, creds, http.MethodPost, targetURL, payload, true)
	if err != nil {
		return err
	}
	return ProcessSSEStream(ctx, resp, format, c.StreamBufferSize, c.StreamIdleTimeout, onChunk)
}

func (c *Client) wrapCloudCodeRequest(creds *auth.Credentials, model string, gemReq *GeminiRequest) *GenerateContentRequest {
	return &GenerateContentRequest{
		Project:      creds.ProjectID,
		RequestID:    newRequestID("req"),
		Model:        model,
		UserPromptID: newRequestID("prompt"),
		UserAgent:    c.UserAgent(creds.Kind),
		Request:      gemReq,
	}
}

func (c *Client) cloudCodeURL(apiVersion, method string, params url.Values) string {
	base := strings.TrimRight(c.CloudCodeEndpoint, "/")
	if base == "" {
		base = DefaultCloudCodeEndpoint
	}
	u := fmt.Sprintf("%s/%s:%s", base, apiVersion, method)
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	return u
}

func (c *Client) geminiURL(model, method string, params url.Values) string {
	base := strings.TrimRight(c.GeminiEndpoint, "/")
	if base == "" {
		base = DefaultGeminiAPIEndpoint
	}
	cleanModel := strings.TrimPrefix(model, "models/")
	u := fmt.Sprintf("%s/v1beta/models/%s:%s", base, url.PathEscape(cleanModel), method)
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	return u
}

func (c *Client) doJSON(ctx context.Context, creds *auth.Credentials, method, targetURL string, body, out any) error {
	resp, err := c.sendRequest(ctx, creds, method, targetURL, body, false)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response json: %w", err)
	}
	return nil
}

func (c *Client) sendRequest(ctx context.Context, creds *auth.Credentials, method, targetURL string, body any, acceptSSE bool) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.UserAgent(creds.Kind))
	if acceptSSE {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}

	switch creds.Kind {
	case auth.AuthKindGeminiAPIKey:
		req.Header.Set("x-goog-api-key", creds.APIKey)
	case auth.AuthKindGateway:
		if creds.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+creds.APIKey)
		}
	default:
		if creds.AccessToken != "" {
			req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
		}
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
		apiErr := &APIError{
			StatusCode:  resp.StatusCode,
			SherlogLink: resp.Header.Get("x-goog-sherlog-Link"),
			TraceID:     resp.Header.Get("x-cloudaicompanion-trace-id"),
			Body:        strings.TrimSpace(string(errBody)),
		}
		var errProbe struct {
			Error *APIErrorBody `json:"error"`
		}
		if json.Unmarshal(errBody, &errProbe) == nil && errProbe.Error != nil {
			apiErr.Code = errProbe.Error.Code
			apiErr.Status = errProbe.Error.Status
			apiErr.Message = errProbe.Error.Message
		}
		return nil, apiErr
	}
	return resp, nil
}

func toOpenAIChatRequest(model string, gemReq *GeminiRequest, stream bool) map[string]any {
	var messages []map[string]any
	if gemReq.SystemInstruction != nil {
		var sysText strings.Builder
		for _, p := range gemReq.SystemInstruction.Parts {
			sysText.WriteString(p.Text)
		}
		if sysText.Len() > 0 {
			messages = append(messages, map[string]any{"role": "system", "content": sysText.String()})
		}
	}
	for _, c := range gemReq.Contents {
		role := c.Role
		if role == "model" {
			role = "assistant"
		}
		var text strings.Builder
		for _, p := range c.Parts {
			if !p.Thought {
				text.WriteString(p.Text)
			}
		}
		messages = append(messages, map[string]any{"role": role, "content": text.String()})
	}
	out := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   stream,
	}
	if gemReq.GenerationConfig != nil {
		if gemReq.GenerationConfig.Temperature != nil {
			out["temperature"] = *gemReq.GenerationConfig.Temperature
		}
		if gemReq.GenerationConfig.MaxOutputTokens > 0 {
			out["max_tokens"] = gemReq.GenerationConfig.MaxOutputTokens
		}
	}
	return out
}
