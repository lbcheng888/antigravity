package aicode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"antigravity.dev/agy-sdk/pkg/auth"
)

func newTestClientWithToken(t *testing.T, serverURL string) (*Client, *auth.CompositeTokenStorage) {
	t.Helper()
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("AGY_LLM_GATEWAY_URL", "")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")

	tmp := t.TempDir()
	storage := auth.NewCLITokenStorage(tmp, tmp, true)
	err := storage.SaveToken(&auth.OAuthToken{
		AccessToken: "test-access-token",
		TokenType:   "Bearer",
		Expiry:      time.Now().Add(time.Hour),
	}, "consumer")
	if err != nil {
		t.Fatalf("SaveToken failed: %v", err)
	}
	_ = storage.SaveProjectAndRegion("proj-test-123", "us-central1")

	ca := &auth.ChainedAuth{Storage: storage, HTTPClient: http.DefaultClient}
	c := NewClient(ca)
	c.CloudCodeEndpoint = serverURL
	c.GeminiEndpoint = serverURL
	return c, storage
}

func TestCloudCodeEndpointsLoadModelsAndQuota(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-access-token" {
			t.Errorf("Authorization = %q, want Bearer test-access-token", got)
		}
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "antigravity/") {
			t.Errorf("unexpected User-Agent: %q", r.Header.Get("User-Agent"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1internal:loadCodeAssist":
			_, _ = w.Write([]byte(`{
				"currentTier":{"id":"free-tier","name":"Free"},
				"paidTier":{"id":"g1-ultra-tier","name":"Google AI Ultra"},
				"cloudaicompanionProject":"proj-updated-999"
			}`))
		case "/v1internal:fetchAvailableModels":
			_, _ = w.Write([]byte(`{
				"defaultAgentModelId":"gemini-3.5-pro",
				"models":{
					"gemini-3.5-pro":{"displayName":"Gemini 3.5 Pro","supportsThinking":true,"maxTokens":1000000,"quotaInfo":{"remainingFraction":0.85,"resetTime":"2026-11-18T12:00:00Z"}}
				}
			}`))
		case "/v1internal:retrieveUserQuotaSummary":
			_, _ = w.Write([]byte(`{
				"groups":[{
					"displayName":"Gemini Models",
					"buckets":[{"bucketId":"gemini-pro","displayName":"Gemini Pro","remainingFraction":0.85,"remainingAmount":850}]
				}]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c, storage := newTestClientWithToken(t, srv.URL)
	ctx := context.Background()

	loadResp, err := c.LoadCodeAssist(ctx, "")
	if err != nil {
		t.Fatalf("LoadCodeAssist error: %v", err)
	}
	if loadResp.PaidTier == nil || loadResp.PaidTier.ID != "g1-ultra-tier" {
		t.Fatalf("unexpected tier: %+v", loadResp.PaidTier)
	}
	stored, err := storage.LoadStoredToken()
	if err != nil || stored.ProjectID != "proj-updated-999" || stored.UserTier != "g1-ultra-tier" {
		t.Fatalf("LoadCodeAssist did not persist metadata: stored=%+v err=%v", stored, err)
	}

	modelsResp, err := c.FetchAvailableModels(ctx, "")
	if err != nil {
		t.Fatalf("FetchAvailableModels error: %v", err)
	}
	if modelsResp.DefaultAgentModelID != "gemini-3.5-pro" || modelsResp.Models["gemini-3.5-pro"].QuotaInfo.RemainingFraction != 0.85 {
		t.Fatalf("unexpected models response: %+v", modelsResp)
	}

	quotaResp, err := c.RetrieveUserQuotaSummary(ctx, "")
	if err != nil {
		t.Fatalf("RetrieveUserQuotaSummary error: %v", err)
	}
	if len(quotaResp.Groups) != 1 || quotaResp.Groups[0].Buckets[0].RemainingAmount != 850 {
		t.Fatalf("unexpected quota response: %+v", quotaResp)
	}
}

func TestStreamGenerateContentThinkingTextAndFunctionCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1internal:streamGenerateContent" || r.URL.Query().Get("alt") != "sse" {
			t.Fatalf("unexpected URL %s", r.URL.String())
		}
		var req GenerateContentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "gemini-3.5-pro" || req.Project != "proj-test-123" {
			t.Fatalf("unexpected req fields: model=%q project=%q", req.Model, req.Project)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("x-cloudaicompanion-trace-id", "trace-header-1")
		flusher, _ := w.(http.Flusher)

		events := []string{
			"data: {\"response\":{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"thinking step\",\"thought\":true}]}}]}}\r\n\r\n",
			"data: {\"response\":{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"hello world\"}]}}]},\"traceId\":\"trace-body-2\"}\n\n",
			"data: {\"response\":{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"functionCall\":{\"id\":\"fc-1\",\"name\":\"read_file\",\"args\":{\"path\":\"go.mod\"}}}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":12,\"candidatesTokenCount\":5,\"totalTokenCount\":20,\"thoughtsTokenCount\":3}}}\n\n",
			"data: [DONE]\n\n",
		}
		for _, ev := range events {
			_, _ = w.Write([]byte(ev))
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	c, _ := newTestClientWithToken(t, srv.URL)
	var thoughts, text, funcNames []string
	var finalUsage *UsageMetadata

	err := c.StreamGenerateContent(context.Background(), "gemini-3.5-pro", &GeminiRequest{
		Contents: []*Content{{Role: "user", Parts: []*Part{{Text: "hi"}}}},
	}, func(ch *SSEChunk) error {
		if ch.Response == nil {
			return nil
		}
		for _, cand := range ch.Response.Candidates {
			if cand.Content == nil {
				continue
			}
			for _, p := range cand.Content.Parts {
				if p.Thought {
					thoughts = append(thoughts, p.Text)
				} else if p.Text != "" {
					text = append(text, p.Text)
				}
				if p.FunctionCall != nil {
					funcNames = append(funcNames, p.FunctionCall.Name)
				}
			}
		}
		if ch.Response.UsageMetadata != nil {
			finalUsage = ch.Response.UsageMetadata
		}
		return nil
	})
	if err != nil {
		t.Fatalf("StreamGenerateContent error: %v", err)
	}
	if strings.Join(thoughts, "") != "thinking step" {
		t.Fatalf("unexpected thoughts: %v", thoughts)
	}
	if strings.Join(text, "") != "hello world" {
		t.Fatalf("unexpected text: %v", text)
	}
	if len(funcNames) != 1 || funcNames[0] != "read_file" {
		t.Fatalf("unexpected tool calls: %v", funcNames)
	}
	if finalUsage == nil || finalUsage.TotalTokenCount != 20 {
		t.Fatalf("unexpected usage: %+v", finalUsage)
	}
}

func TestStreamGenerateContentInBandSSEError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("x-goog-sherlog-Link", "https://sherlog/123")
		w.Header().Set("x-cloudaicompanion-trace-id", "trace-err-9")
		_, _ = fmt.Fprint(w, "data: {\"error\":{\"code\":429,\"message\":\"quota exhausted\",\"status\":\"RESOURCE_EXHAUSTED\"}}\n\n")
	}))
	defer srv.Close()

	c, _ := newTestClientWithToken(t, srv.URL)
	err := c.StreamGenerateContent(context.Background(), "gemini-3.5-pro", &GeminiRequest{
		Contents: []*Content{{Role: "user", Parts: []*Part{{Text: "hi"}}}},
	}, func(*SSEChunk) error { return nil })

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != 429 || apiErr.Status != "RESOURCE_EXHAUSTED" || apiErr.SherlogLink != "https://sherlog/123" || apiErr.TraceID != "trace-err-9" {
		t.Fatalf("unexpected APIError fields: %+v", apiErr)
	}
}

func TestGeminiAPIKeyRouting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-2.5-flash:streamGenerateContent" {
			t.Fatalf("unexpected Gemini API path: %s", r.URL.Path)
		}
		if got := r.Header.Get("x-goog-api-key"); got != "AIzaTestKey999" {
			t.Fatalf("x-goog-api-key = %q, want AIzaTestKey999", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"api key ok\"}]},\"finishReason\":\"STOP\"}]}\n\n")
	}))
	defer srv.Close()

	t.Setenv("GEMINI_API_KEY", "AIzaTestKey999")
	t.Setenv("AGY_LLM_GATEWAY_URL", "")
	tmp := t.TempDir()
	c := NewClient(auth.NewChainedAuth(tmp, tmp, true))
	c.GeminiEndpoint = srv.URL

	var got strings.Builder
	err := c.StreamGenerateContent(context.Background(), "gemini-2.5-flash", &GeminiRequest{
		Contents: []*Content{{Role: "user", Parts: []*Part{{Text: "ping"}}}},
	}, func(ch *SSEChunk) error {
		for _, cand := range ch.Response.Candidates {
			for _, p := range cand.Content.Parts {
				got.WriteString(p.Text)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("StreamGenerateContent with GEMINI_API_KEY error: %v", err)
	}
	if got.String() != "api key ok" {
		t.Fatalf("got %q, want %q", got.String(), "api key ok")
	}
}
