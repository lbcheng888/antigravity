package aicode

// ClientMetadata identifies the Antigravity client environment in CloudCode RPCs.
type ClientMetadata struct {
	IDEType    string `json:"ideType,omitempty"`
	IDEVersion string `json:"ideVersion,omitempty"`
	Platform   string `json:"platform,omitempty"`
	PluginType string `json:"pluginType,omitempty"`
	IDEName    string `json:"ideName,omitempty"`
}

// UserTier represents a CloudCode subscription or entitlement tier.
type UserTier struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	IsDefault   bool   `json:"isDefault,omitempty"`
}

// LoadCodeAssistRequest queries project onboarding and active tier state.
type LoadCodeAssistRequest struct {
	CloudAICompanionProject string          `json:"cloudaicompanionProject,omitempty"`
	Metadata                *ClientMetadata `json:"metadata,omitempty"`
}

// LoadCodeAssistResponse carries tier entitlement and companion project details.
type LoadCodeAssistResponse struct {
	CurrentTier             *UserTier `json:"currentTier,omitempty"`
	PaidTier                *UserTier `json:"paidTier,omitempty"`
	CloudAICompanionProject string    `json:"cloudaicompanionProject,omitempty"`
}

// EffectiveTier returns PaidTier when present, otherwise CurrentTier.
func (r *LoadCodeAssistResponse) EffectiveTier() *UserTier {
	if r == nil {
		return nil
	}
	if r.PaidTier != nil && r.PaidTier.ID != "" {
		return r.PaidTier
	}
	return r.CurrentTier
}

// FetchAvailableModelsRequest queries available model definitions and quotas.
type FetchAvailableModelsRequest struct {
	Project   string `json:"project,omitempty"`
	RequestID string `json:"requestId,omitempty"`
}

// QuotaInfo reports remaining quota fraction and next reset timestamp for a model.
type QuotaInfo struct {
	RemainingFraction float64 `json:"remainingFraction"`
	ResetTime         string  `json:"resetTime,omitempty"`
}

// ModelDetails describes capabilities and quota state for one served model.
type ModelDetails struct {
	Model              string     `json:"model,omitempty"`
	DisplayName        string     `json:"displayName,omitempty"`
	SupportsImages     bool       `json:"supportsImages,omitempty"`
	SupportsThinking   bool       `json:"supportsThinking,omitempty"`
	MaxTokens          int        `json:"maxTokens,omitempty"`
	MaxOutputTokens    int        `json:"maxOutputTokens,omitempty"`
	Disabled           bool       `json:"disabled,omitempty"`
	Recommended        bool       `json:"recommended,omitempty"`
	QuotaInfo          *QuotaInfo `json:"quotaInfo,omitempty"`
	SupportedMimeTypes map[string]bool `json:"supportedMimeTypes,omitempty"`
}

// FetchAvailableModelsResponse contains served model entries keyed by model ID.
type FetchAvailableModelsResponse struct {
	Models              map[string]*ModelDetails `json:"models,omitempty"`
	DefaultAgentModelID string                   `json:"defaultAgentModelId,omitempty"`
}

// RetrieveUserQuotaSummaryRequest queries aggregated quota groups for a project.
type RetrieveUserQuotaSummaryRequest struct {
	Project string `json:"project,omitempty"`
}

// QuotaSummaryBucket represents an individual quota bucket within a summary group.
type QuotaSummaryBucket struct {
	BucketID          string  `json:"bucketId,omitempty"`
	DisplayName       string  `json:"displayName,omitempty"`
	Window            string  `json:"window,omitempty"`
	RemainingFraction float64 `json:"remainingFraction"`
	RemainingAmount   int64   `json:"remainingAmount,omitempty"`
	Disabled          bool    `json:"disabled,omitempty"`
	ResetTime         string  `json:"resetTime,omitempty"`
}

// QuotaSummaryGroup groups related quota buckets for display.
type QuotaSummaryGroup struct {
	DisplayName string                `json:"displayName,omitempty"`
	Description string                `json:"description,omitempty"`
	Buckets     []*QuotaSummaryBucket `json:"buckets,omitempty"`
}

// RetrieveUserQuotaSummaryResponse holds bucket-level and grouped quota data.
type RetrieveUserQuotaSummaryResponse struct {
	Buckets     []*QuotaSummaryBucket `json:"buckets,omitempty"`
	Groups      []*QuotaSummaryGroup  `json:"groups,omitempty"`
	Description string                `json:"description,omitempty"`
}

// Blob holds inline base64-encoded binary content.
type Blob struct {
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"`
}

// FunctionCall represents a model-requested tool invocation.
type FunctionCall struct {
	ID   string         `json:"id,omitempty"`
	Name string         `json:"name,omitempty"`
	Args map[string]any `json:"args,omitempty"`
}

// FunctionResponse carries the result of a tool invocation back to the model.
type FunctionResponse struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name,omitempty"`
	Response map[string]any `json:"response,omitempty"`
}

// Part is one structured segment inside a Content message.
type Part struct {
	Text             string            `json:"text,omitempty"`
	Thought          bool              `json:"thought,omitempty"`
	ThoughtSignature string            `json:"thoughtSignature,omitempty"`
	InlineData       *Blob             `json:"inlineData,omitempty"`
	FunctionCall     *FunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *FunctionResponse `json:"functionResponse,omitempty"`
}

// Content represents one conversational turn (user, model, or function).
type Content struct {
	Role  string  `json:"role,omitempty"`
	Parts []*Part `json:"parts,omitempty"`
}

// FunctionDeclaration describes a single callable tool parameter schema.
type FunctionDeclaration struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// Tool bundles function declarations exposed to the model.
type Tool struct {
	FunctionDeclarations []*FunctionDeclaration `json:"functionDeclarations,omitempty"`
}

// ThinkingConfig controls model reasoning output and budget.
type ThinkingConfig struct {
	IncludeThoughts bool `json:"includeThoughts,omitempty"`
	ThinkingBudget  int  `json:"thinkingBudget,omitempty"`
}

// GenerationConfig configures sampling and thinking parameters.
type GenerationConfig struct {
	Temperature     *float64        `json:"temperature,omitempty"`
	TopP            *float64        `json:"topP,omitempty"`
	MaxOutputTokens int             `json:"maxOutputTokens,omitempty"`
	StopSequences   []string        `json:"stopSequences,omitempty"`
	ThinkingConfig  *ThinkingConfig `json:"thinkingConfig,omitempty"`
}

// GeminiRequest is the inner payload expected by both CloudCode v1internal and Gemini v1beta.
type GeminiRequest struct {
	Contents          []*Content        `json:"contents"`
	SystemInstruction *Content          `json:"systemInstruction,omitempty"`
	Tools             []*Tool           `json:"tools,omitempty"`
	GenerationConfig  *GenerationConfig `json:"generationConfig,omitempty"`
}

// GenerateContentRequest wraps GeminiRequest for CloudCode v1internal endpoints.
type GenerateContentRequest struct {
	Project      string         `json:"project,omitempty"`
	RequestID    string         `json:"requestId,omitempty"`
	Model        string         `json:"model"`
	UserPromptID string         `json:"userPromptId,omitempty"`
	UserAgent    string         `json:"userAgent,omitempty"`
	Request      *GeminiRequest `json:"request"`
}

// Candidate represents one generated model response candidate.
type Candidate struct {
	Index        int      `json:"index,omitempty"`
	Content      *Content `json:"content,omitempty"`
	FinishReason string   `json:"finishReason,omitempty"`
}

// UsageMetadata reports token accounting for a request/response pair.
type UsageMetadata struct {
	PromptTokenCount        int `json:"promptTokenCount,omitempty"`
	CandidatesTokenCount    int `json:"candidatesTokenCount,omitempty"`
	TotalTokenCount         int `json:"totalTokenCount,omitempty"`
	ThoughtsTokenCount      int `json:"thoughtsTokenCount,omitempty"`
	CachedContentTokenCount int `json:"cachedContentTokenCount,omitempty"`
}

// InnerGenerateContentResponse matches the Gemini v1beta GenerateContentResponse body.
type InnerGenerateContentResponse struct {
	Candidates    []*Candidate   `json:"candidates,omitempty"`
	UsageMetadata *UsageMetadata `json:"usageMetadata,omitempty"`
	ModelVersion  string         `json:"modelVersion,omitempty"`
}

// APIErrorBody captures an RPC or in-band SSE JSON error payload.
type APIErrorBody struct {
	Code    int    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	Status  string `json:"status,omitempty"`
}

// GenerateContentResponse is the CloudCode v1internal response envelope.
type GenerateContentResponse struct {
	Response *InnerGenerateContentResponse `json:"response,omitempty"`
	TraceID  string                        `json:"traceId,omitempty"`
	Error    *APIErrorBody                 `json:"error,omitempty"`
}

// SSEChunk is the normalized emission delivered to stream callbacks.
type SSEChunk struct {
	Response     *InnerGenerateContentResponse
	TraceID      string
	RawData      []byte
	SherlogLink  string
	BackendTrace string
}
