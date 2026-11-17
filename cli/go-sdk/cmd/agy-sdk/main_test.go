package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"antigravity.dev/agy-sdk/pkg/aicode"
	"antigravity.dev/agy-sdk/pkg/auth"
	"antigravity.dev/agy-sdk/pkg/customizations"
	"antigravity.dev/agy-sdk/pkg/mcp"
	"antigravity.dev/agy-sdk/pkg/sessionstore"
	"antigravity.dev/agy-sdk/pkg/swarm"
	"antigravity.dev/agy-sdk/pkg/swarmrun"
)

func TestVersionAndHelp(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}, {"-v"}} {
		var out, errBuf bytes.Buffer
		code := run(context.Background(), args, nil, &out, &errBuf)
		if code != 0 {
			t.Fatalf("%v exit=%d stderr=%s", args, code, errBuf.String())
		}
		if !strings.Contains(out.String(), aicode.DefaultClientVersion) {
			t.Fatalf("%v output=%q missing version %s", args, out.String(), aicode.DefaultClientVersion)
		}
	}

	for _, args := range [][]string{nil, {"help"}, {"--help"}, {"-h"}} {
		var out, errBuf bytes.Buffer
		code := run(context.Background(), args, nil, &out, &errBuf)
		if code != 0 {
			t.Fatalf("%v exit=%d stderr=%s", args, code, errBuf.String())
		}
		for _, want := range []string{"auth status", "auth login", "auth logout", "models", "quota", "prompt", "agent", "swarm inspect", "swarm run", "mcp", "plugin", "skills", "sessions", "trace-proxy", "acp"} {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("%v help output missing %q", args, want)
			}
		}
	}

	var out, errBuf bytes.Buffer
	if code := run(context.Background(), []string{"no-such-cmd"}, nil, &out, &errBuf); code != 2 {
		t.Fatalf("unknown command exit=%d, want 2", code)
	}
}

func TestAuthStatusMasksSecretsAndIsolatesDir(t *testing.T) {
	dir := t.TempDir()
	rawAPIKey := "AIzaSySecretKey1234567890"
	rawAccess := "ya29.super-secret-access-token-value"
	rawRefresh := "1//0gSuperSecretRefreshTokenValue"
	rawGatewayKey := "gw-secret-key-987654321"

	t.Setenv("GEMINI_API_KEY", rawAPIKey)
	t.Setenv("AGY_LLM_GATEWAY_API_KEY", rawGatewayKey)

	storage := auth.NewCLITokenStorage(dir, "test-app", true)
	err := storage.SaveToken(&auth.OAuthToken{
		AccessToken:  rawAccess,
		RefreshToken: rawRefresh,
		Expiry:       time.Now().Add(time.Hour),
	}, string(auth.AuthKindConsumerOAuth))
	if err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	var out, errBuf bytes.Buffer
	code := run(context.Background(), []string{"auth", "status", "--data-dir", dir, "--app-data-dir", "test-app", "--file", "--json"}, nil, &out, &errBuf)
	if code != 0 {
		t.Fatalf("auth status exit=%d stderr=%s", code, errBuf.String())
	}

	body := out.String()
	for _, secret := range []string{rawAPIKey, rawAccess, rawRefresh, rawGatewayKey} {
		if strings.Contains(body, secret) {
			t.Fatalf("auth status leaked raw secret %q in output:\n%s", secret, body)
		}
	}

	var parsed map[string]any
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, body)
	}
	if parsed["gemini_api_key"] != auth.MaskSecret(rawAPIKey) {
		t.Fatalf("gemini_api_key = %v, want %v", parsed["gemini_api_key"], auth.MaskSecret(rawAPIKey))
	}
	if parsed["access_token_redacted"] != auth.MaskSecret(rawAccess) {
		t.Fatalf("access_token_redacted = %v, want %v", parsed["access_token_redacted"], auth.MaskSecret(rawAccess))
	}
	if parsed["gateway_api_key"] != auth.MaskSecret(rawGatewayKey) {
		t.Fatalf("gateway_api_key = %v, want %v", parsed["gateway_api_key"], auth.MaskSecret(rawGatewayKey))
	}
}

func TestAuthLoginAndLogoutPKCEFlow(t *testing.T) {
	dir := t.TempDir()
	const issuedAccess = "ya29.pkce-issued-access-token"
	const issuedRefresh = "1//pkce-refresh-token"

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "auth-code-123" || r.Form.Get("code_verifier") == "" {
			http.Error(w, "invalid token exchange request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  issuedAccess,
			"refresh_token": issuedRefresh,
			"expires_in":    3600,
			"token_type":    "Bearer",
		})
	}))
	defer tokenSrv.Close()

	cfg := auth.OAuthConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		AuthURL:      "https://accounts.example.test/auth",
		TokenURL:     tokenSrv.URL,
		Scopes:       []string{"openid", "email"},
	}

	fakeBrowser := func(authURL string) error {
		u, err := url.Parse(authURL)
		if err != nil {
			return err
		}
		q := u.Query()
		if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
			return fmt.Errorf("missing PKCE S256 challenge in %s", authURL)
		}
		cbURL := q.Get("redirect_uri") + "?code=auth-code-123&state=" + url.QueryEscape(q.Get("state"))
		go func() {
			resp, err := http.Get(cbURL)
			if err == nil {
				_ = resp.Body.Close()
			}
		}()
		return nil
	}

	var out, errBuf bytes.Buffer
	code := runAuthLogin(
		context.Background(),
		[]string{"--data-dir", dir, "--app-data-dir", "test-app", "--file"},
		&out,
		&errBuf,
		&cfg,
		fakeBrowser,
	)
	if code != 0 {
		t.Fatalf("auth login exit=%d stderr=%s", code, errBuf.String())
	}
	if strings.Contains(out.String(), issuedAccess) {
		t.Fatalf("auth login output leaked raw token: %s", out.String())
	}
	if !strings.Contains(out.String(), auth.MaskSecret(issuedAccess)) {
		t.Fatalf("auth login output missing masked token: %s", out.String())
	}

	// Verify logout removes the stored token
	out.Reset()
	errBuf.Reset()
	code = run(context.Background(), []string{"auth", "logout", "--data-dir", dir, "--app-data-dir", "test-app", "--file"}, nil, &out, &errBuf)
	if code != 0 {
		t.Fatalf("auth logout exit=%d stderr=%s", code, errBuf.String())
	}

	out.Reset()
	errBuf.Reset()
	code = run(context.Background(), []string{"auth", "status", "--data-dir", dir, "--app-data-dir", "test-app", "--file", "--json"}, nil, &out, &errBuf)
	if code != 0 {
		t.Fatalf("auth status after logout exit=%d stderr=%s", code, errBuf.String())
	}
	var status map[string]any
	if err := json.Unmarshal(out.Bytes(), &status); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if status["stored_token_present"] != false {
		t.Fatalf("expected stored_token_present=false after logout, got %v", status["stored_token_present"])
	}
}

func TestModelsAndQuotaCommands(t *testing.T) {
	dir := t.TempDir()
	storage := auth.NewCLITokenStorage(dir, "test-app", true)
	if err := storage.SaveToken(&auth.OAuthToken{
		AccessToken: "ya29.test-token-for-models",
		Expiry:      time.Now().Add(time.Hour),
	}, string(auth.AuthKindConsumerOAuth)); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1internal:fetchAvailableModels":
			_ = json.NewEncoder(w).Encode(aicode.FetchAvailableModelsResponse{
				DefaultAgentModelID: "gemini-4-hydrogen-medium",
				Models: map[string]*aicode.ModelDetails{
					"gemini-4-hydrogen-medium": {
						Model:            "gemini-4-hydrogen-medium",
						DisplayName:      "Gemini 4 Hydrogen (Medium)",
						Recommended:      true,
						SupportsThinking: true,
						QuotaInfo:        &aicode.QuotaInfo{RemainingFraction: 0.75},
					},
				},
			})
		case "/v1internal:retrieveUserQuotaSummary":
			_ = json.NewEncoder(w).Encode(aicode.RetrieveUserQuotaSummaryResponse{
				Description: "Pro Tier Quota",
				Groups: []*aicode.QuotaSummaryGroup{
					{
						DisplayName: "Gemini",
						Buckets: []*aicode.QuotaSummaryBucket{
							{BucketID: "flash", DisplayName: "Flash", RemainingFraction: 0.9, Window: "5h"},
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("AGY_LLM_GATEWAY_URL", "")
	t.Setenv("CLOUDCODE_ENDPOINT_URL", srv.URL)

	var out, errBuf bytes.Buffer
	if code := run(context.Background(), []string{"models", "--data-dir", dir, "--app-data-dir", "test-app", "--file"}, nil, &out, &errBuf); code != 0 {
		t.Fatalf("models exit=%d stderr=%s", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "gemini-4-hydrogen-medium") || !strings.Contains(out.String(), "75%") {
		t.Fatalf("unexpected models output:\n%s", out.String())
	}

	out.Reset()
	errBuf.Reset()
	if code := run(context.Background(), []string{"quota", "--data-dir", dir, "--app-data-dir", "test-app", "--file"}, nil, &out, &errBuf); code != 0 {
		t.Fatalf("quota exit=%d stderr=%s", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "Pro Tier Quota") || !strings.Contains(out.String(), "90% remaining") {
		t.Fatalf("unexpected quota output:\n%s", out.String())
	}
}

func TestPromptStreamingCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "gemini-2.5-flash:streamGenerateContent") {
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		chunks := []aicode.InnerGenerateContentResponse{
			{Candidates: []*aicode.Candidate{{Content: &aicode.Content{Role: "model", Parts: []*aicode.Part{{Text: "thinking...", Thought: true}}}}}},
			{Candidates: []*aicode.Candidate{{Content: &aicode.Content{Role: "model", Parts: []*aicode.Part{{Text: "Hello "}}}}}},
			{Candidates: []*aicode.Candidate{{Content: &aicode.Content{Role: "model", Parts: []*aicode.Part{{Text: "world!"}}}, FinishReason: "STOP"}}},
		}
		for _, c := range chunks {
			b, _ := json.Marshal(c)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
		}
	}))
	defer srv.Close()

	t.Setenv("GEMINI_API_KEY", "test-gemini-key")
	t.Setenv("GEMINI_API_BASE_URL", srv.URL)

	var out, errBuf bytes.Buffer
	code := run(context.Background(), []string{"prompt", "-p", "Say hi", "-m", "gemini-2.5-flash"}, nil, &out, &errBuf)
	if code != 0 {
		t.Fatalf("prompt exit=%d stderr=%s", code, errBuf.String())
	}
	got := out.String()
	if !strings.Contains(got, "[thought]") || !strings.Contains(got, "thinking...") || !strings.Contains(got, "Hello world!") {
		t.Fatalf("unexpected prompt output:\n%s", got)
	}
}

func TestSwarmInspectCommand(t *testing.T) {
	swarmDir := filepath.Join(t.TempDir(), "swarm")
	st, err := swarm.Create(swarmDir, "CLI Test Swarm", "coord-1", "gemini-4-hydrogen-medium")
	if err != nil {
		t.Fatalf("swarm.Create: %v", err)
	}
	for i := 1; i <= 3; i++ {
		if _, err := st.Post(swarm.PostRequest{
			Author: "coordinator",
			Text:   fmt.Sprintf("message-%d", i),
		}); err != nil {
			t.Fatalf("Post %d: %v", i, err)
		}
	}

	var out, errBuf bytes.Buffer
	code := run(context.Background(), []string{"swarm", "inspect", swarmDir, "--tail", "2"}, nil, &out, &errBuf)
	if code != 0 {
		t.Fatalf("swarm inspect exit=%d stderr=%s", code, errBuf.String())
	}
	got := out.String()
	if strings.Contains(got, "message-1") || !strings.Contains(got, "message-2") || !strings.Contains(got, "message-3") {
		t.Fatalf("expected --tail 2 to show only message-2 and message-3, got:\n%s", got)
	}

	out.Reset()
	errBuf.Reset()
	code = run(context.Background(), []string{"swarm", "inspect", swarmDir, "--tail", "1", "--json"}, nil, &out, &errBuf)
	if code != 0 {
		t.Fatalf("swarm inspect --json exit=%d stderr=%s", code, errBuf.String())
	}
	var snap swarm.BoardSnapshot
	if err := json.Unmarshal(out.Bytes(), &snap); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(snap.Messages) != 1 || snap.Messages[0].Text != "message-3" {
		t.Fatalf("unexpected JSON messages: %+v", snap.Messages)
	}
}

func TestACPStdioCommand(t *testing.T) {
	dir := t.TempDir()
	// Ensure no accidental env interference
	_ = os.MkdirAll(dir, 0o755)

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}`,
		`{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"cwd":"` + dir + `"}}`,
		"",
	}, "\n")

	var out, errBuf bytes.Buffer
	code := run(context.Background(), []string{"acp", "--stdio", "--data-dir", dir, "--file"}, strings.NewReader(input), &out, &errBuf)
	if code != 0 {
		t.Fatalf("acp exit=%d stderr=%s", code, errBuf.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected >= 2 JSON-RPC responses, got %d:\n%s", len(lines), out.String())
	}
	for _, line := range lines {
		var resp map[string]any
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("invalid JSON-RPC line %q: %v", line, err)
		}
		if resp["error"] != nil {
			t.Fatalf("unexpected JSON-RPC error in %q", line)
		}
	}
}

func TestAgentOneShotCommand(t *testing.T) {
	dir := t.TempDir()
	notePath := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(notePath, []byte("before\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	trajPath := filepath.Join(dir, "transcript.jsonl")

	var reqCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "gemini-4-hydrogen-medium:streamGenerateContent") {
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
			return
		}
		var req aicode.GeminiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		reqCount++
		w.Header().Set("Content-Type", "text/event-stream")

		var chunks []aicode.InnerGenerateContentResponse
		if reqCount == 1 {
			chunks = []aicode.InnerGenerateContentResponse{
				{Candidates: []*aicode.Candidate{{Content: &aicode.Content{
					Role: "model",
					Parts: []*aicode.Part{{
						FunctionCall: &aicode.FunctionCall{
							ID:   "call-1",
							Name: "edit_file",
							Args: map[string]any{
								"Path":        "note.txt",
								"Target":      "before",
								"Replacement": "after",
							},
						},
					}},
				}}}},
			}
		} else {
			var sawFuncResp bool
			for _, c := range req.Contents {
				for _, p := range c.Parts {
					if p.FunctionResponse != nil && p.FunctionResponse.Name == "edit_file" {
						sawFuncResp = true
					}
				}
			}
			if !sawFuncResp {
				http.Error(w, "missing edit_file FunctionResponse", http.StatusBadRequest)
				return
			}
			chunks = []aicode.InnerGenerateContentResponse{
				{Candidates: []*aicode.Candidate{{
					Content:      &aicode.Content{Role: "model", Parts: []*aicode.Part{{Text: "Updated note.txt successfully."}}},
					FinishReason: "STOP",
				}}},
			}
		}
		for _, c := range chunks {
			b, _ := json.Marshal(c)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
		}
	}))
	defer srv.Close()

	t.Setenv("GEMINI_API_KEY", "test-gemini-key")
	t.Setenv("GEMINI_API_BASE_URL", srv.URL)

	var out, errBuf bytes.Buffer
	code := run(context.Background(), []string{
		"agent",
		"-p", "update note.txt",
		"--cwd", dir,
		"--transcript", trajPath,
		"--thoughts=false",
	}, nil, &out, &errBuf)
	if code != 0 {
		t.Fatalf("agent -p exit=%d stderr=%s\nout=%s", code, errBuf.String(), out.String())
	}

	gotFile, err := os.ReadFile(notePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(gotFile) != "after\n" {
		t.Fatalf("expected note.txt to be updated to 'after\\n', got %q", string(gotFile))
	}
	if !strings.Contains(out.String(), "Updated note.txt successfully.") {
		t.Fatalf("expected final answer in stdout, got:\n%s", out.String())
	}

	trajRaw, err := os.ReadFile(trajPath)
	if err != nil {
		t.Fatalf("ReadFile transcript: %v", err)
	}
	for _, want := range []string{"USER_INPUT", "PLANNER_RESPONSE", "TOOL_RESULT"} {
		if !strings.Contains(string(trajRaw), want) {
			t.Fatalf("transcript missing %q:\n%s", want, string(trajRaw))
		}
	}
}

func TestAgentREPLCommand(t *testing.T) {
	dir := t.TempDir()

	var reqCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req aicode.GeminiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		reqCount++
		w.Header().Set("Content-Type", "text/event-stream")

		var reply string
		switch reqCount {
		case 1:
			if !strings.Contains(r.URL.Path, "gemini-2.5-flash:streamGenerateContent") {
				http.Error(w, "expected model switch to gemini-2.5-flash, got "+r.URL.Path, http.StatusBadRequest)
				return
			}
			// In plan mode, mutating tools like edit_file must be omitted.
			for _, tool := range req.Tools {
				for _, decl := range tool.FunctionDeclarations {
					if decl.Name == "edit_file" {
						http.Error(w, "edit_file should not be declared in plan mode", http.StatusBadRequest)
						return
					}
				}
			}
			reply = "First REPL reply"
		case 2:
			if len(req.Contents) != 1 {
				http.Error(w, fmt.Sprintf("expected cleared history with 1 user turn, got %d", len(req.Contents)), http.StatusBadRequest)
				return
			}
			reply = "Second REPL reply"
		default:
			http.Error(w, "unexpected extra request", http.StatusBadRequest)
			return
		}

		chunk := aicode.InnerGenerateContentResponse{
			Candidates: []*aicode.Candidate{{
				Content:      &aicode.Content{Role: "model", Parts: []*aicode.Part{{Text: reply}}},
				FinishReason: "STOP",
			}},
		}
		b, _ := json.Marshal(chunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
	}))
	defer srv.Close()

	t.Setenv("GEMINI_API_KEY", "test-gemini-key")
	t.Setenv("GEMINI_API_BASE_URL", srv.URL)

	replInput := strings.Join([]string{
		"/help",
		"/mode plan",
		"/model gemini-2.5-flash",
		"hello",
		"/clear",
		"world",
		"/exit",
		"",
	}, "\n")

	var out, errBuf bytes.Buffer
	code := run(context.Background(), []string{"agent", "--cwd", dir, "--thoughts=false"}, strings.NewReader(replInput), &out, &errBuf)
	if code != 0 {
		t.Fatalf("agent REPL exit=%d stderr=%s\nout=%s", code, errBuf.String(), out.String())
	}

	got := out.String()
	for _, want := range []string{
		"REPL commands:",
		"Mode set to plan",
		"Model set to gemini-2.5-flash",
		"First REPL reply",
		"Conversation history cleared.",
		"Second REPL reply",
		"Bye.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("REPL output missing %q:\n%s", want, got)
		}
	}
}

func TestMCPCommands(t *testing.T) {
	dataDir := t.TempDir()

	mcpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			JSONRPC string `json:"jsonrpc"`
			ID      any    `json:"id"`
			Method  string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"protocolVersion": "2024-11-05",
					"serverInfo":      map[string]any{"name": "mock-mcp", "version": "1.0.0"},
				},
			})
		case "tools/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"tools": []mcp.MCPToolInfo{
						{Name: "echo", Description: "Echo input text", InputSchema: map[string]any{"type": "object"}},
					},
				},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{}})
		}
	}))
	defer mcpSrv.Close()

	var out, errBuf bytes.Buffer
	code := run(context.Background(), []string{
		"mcp", "add", "remote", "--type", "http", "--header", "Authorization:Bearer tok", "--data-dir", dataDir, mcpSrv.URL,
	}, nil, &out, &errBuf)
	if code != 0 {
		t.Fatalf("mcp add http exit=%d stderr=%s", code, errBuf.String())
	}

	out.Reset()
	errBuf.Reset()
	code = run(context.Background(), []string{"mcp", "list", "--data-dir", dataDir, "--json"}, nil, &out, &errBuf)
	if code != 0 {
		t.Fatalf("mcp list exit=%d stderr=%s", code, errBuf.String())
	}
	var listed []struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Disabled bool   `json:"disabled"`
	}
	if err := json.Unmarshal(out.Bytes(), &listed); err != nil {
		t.Fatalf("unmarshal mcp list: %v", err)
	}
	if len(listed) != 1 || listed[0].Name != "remote" || listed[0].Type != "http" || listed[0].Disabled {
		t.Fatalf("unexpected mcp list: %+v", listed)
	}

	out.Reset()
	errBuf.Reset()
	code = run(context.Background(), []string{"mcp", "tools", "remote", "--data-dir", dataDir, "--json"}, nil, &out, &errBuf)
	if code != 0 {
		t.Fatalf("mcp tools exit=%d stderr=%s", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "echo") {
		t.Fatalf("expected echo tool in output:\n%s", out.String())
	}

	out.Reset()
	errBuf.Reset()
	if code := run(context.Background(), []string{"mcp", "disable", "remote", "--data-dir", dataDir}, nil, &out, &errBuf); code != 0 {
		t.Fatalf("mcp disable exit=%d stderr=%s", code, errBuf.String())
	}
	if code := run(context.Background(), []string{"mcp", "enable", "remote", "--data-dir", dataDir}, nil, &out, &errBuf); code != 0 {
		t.Fatalf("mcp enable exit=%d stderr=%s", code, errBuf.String())
	}
	if code := run(context.Background(), []string{"mcp", "remove", "remote", "--data-dir", dataDir}, nil, &out, &errBuf); code != 0 {
		t.Fatalf("mcp remove exit=%d stderr=%s", code, errBuf.String())
	}
}

func TestPluginValidateAndList(t *testing.T) {
	pluginsRoot := t.TempDir()
	plugDir := filepath.Join(pluginsRoot, "demo-plugin")
	if err := os.MkdirAll(filepath.Join(plugDir, "skills", "hello"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Missing description -> warning in strict mode, valid in non-strict mode.
	if err := os.WriteFile(filepath.Join(plugDir, "plugin.json"), []byte(`{"name":"demo-plugin","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatalf("WriteFile plugin.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(plugDir, "skills", "hello", "SKILL.md"), []byte("---\nname: hello\ndescription: Greets user\n---\nBody\n"), 0o644); err != nil {
		t.Fatalf("WriteFile SKILL.md: %v", err)
	}

	var out, errBuf bytes.Buffer
	code := run(context.Background(), []string{"plugin", "validate", plugDir, "--json"}, nil, &out, &errBuf)
	if code != 0 {
		t.Fatalf("plugin validate non-strict exit=%d stderr=%s out=%s", code, errBuf.String(), out.String())
	}
	var report customizations.ValidationReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if !report.Valid || report.Name != "demo-plugin" {
		t.Fatalf("unexpected report: %+v", report)
	}

	out.Reset()
	errBuf.Reset()
	code = run(context.Background(), []string{"plugin", "validate", plugDir, "--strict"}, nil, &out, &errBuf)
	if code != 1 {
		t.Fatalf("expected strict validation failure (exit 1), got %d:\n%s", code, out.String())
	}

	out.Reset()
	errBuf.Reset()
	code = run(context.Background(), []string{"plugin", "list", "--plugins-dir", pluginsRoot, "--json"}, nil, &out, &errBuf)
	if code != 0 {
		t.Fatalf("plugin list exit=%d stderr=%s", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "demo-plugin") {
		t.Fatalf("expected demo-plugin in list output:\n%s", out.String())
	}
}

func TestSkillsDiscoveryCommand(t *testing.T) {
	skillsDir := t.TempDir()
	skillSub := filepath.Join(skillsDir, "my-skill")
	if err := os.MkdirAll(skillSub, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillSub, "SKILL.md"), []byte("---\nname: my-skill\ndescription: Custom skill\n---\nInstructions\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var out, errBuf bytes.Buffer
	code := run(context.Background(), []string{"skills", "--skills-dir", skillsDir, "--json"}, nil, &out, &errBuf)
	if code != 0 {
		t.Fatalf("skills exit=%d stderr=%s", code, errBuf.String())
	}
	var skills []customizations.SkillInfo
	if err := json.Unmarshal(out.Bytes(), &skills); err != nil {
		t.Fatalf("unmarshal skills: %v", err)
	}
	if len(skills) != 1 || skills[0].Name != "my-skill" || skills[0].Description != "Custom skill" {
		t.Fatalf("unexpected skills: %+v", skills)
	}
}

func TestSessionsAndAgentContinue(t *testing.T) {
	dataDir := t.TempDir()
	cwd := t.TempDir()

	var reqCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req aicode.GeminiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		reqCount++
		if reqCount == 2 && len(req.Contents) < 3 {
			http.Error(w, fmt.Sprintf("expected resumed history >= 3 contents, got %d", len(req.Contents)), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		reply := fmt.Sprintf("reply-%d", reqCount)
		chunk := aicode.InnerGenerateContentResponse{
			Candidates: []*aicode.Candidate{{
				Content:      &aicode.Content{Role: "model", Parts: []*aicode.Part{{Text: reply}}},
				FinishReason: "STOP",
			}},
		}
		b, _ := json.Marshal(chunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
	}))
	defer srv.Close()

	t.Setenv("GEMINI_API_KEY", "test-gemini-key")
	t.Setenv("GEMINI_API_BASE_URL", srv.URL)

	var out, errBuf bytes.Buffer
	code := run(context.Background(), []string{
		"agent", "-p", "first turn", "--cwd", cwd, "--data-dir", dataDir, "--file", "--thoughts=false",
	}, nil, &out, &errBuf)
	if code != 0 {
		t.Fatalf("agent first turn exit=%d stderr=%s", code, errBuf.String())
	}

	out.Reset()
	errBuf.Reset()
	code = run(context.Background(), []string{
		"agent", "-c", "-p", "second turn", "--cwd", cwd, "--data-dir", dataDir, "--file", "--thoughts=false",
	}, nil, &out, &errBuf)
	if code != 0 {
		t.Fatalf("agent -c second turn exit=%d stderr=%s", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "reply-2") {
		t.Fatalf("expected reply-2, got:\n%s", out.String())
	}

	out.Reset()
	errBuf.Reset()
	code = run(context.Background(), []string{
		"sessions", "list", "--cwd", cwd, "--data-dir", dataDir, "--json",
	}, nil, &out, &errBuf)
	if code != 0 {
		t.Fatalf("sessions list exit=%d stderr=%s", code, errBuf.String())
	}
	var list []sessionstore.SessionSummary
	if err := json.Unmarshal(out.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal sessions list: %v", err)
	}
	if len(list) != 1 || list[0].MessageCount != 4 {
		t.Fatalf("expected 1 session with 4 messages, got %+v", list)
	}

	out.Reset()
	errBuf.Reset()
	code = run(context.Background(), []string{
		"sessions", "show", list[0].ID, "--data-dir", dataDir, "--json",
	}, nil, &out, &errBuf)
	if code != 0 {
		t.Fatalf("sessions show exit=%d stderr=%s", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "second turn") {
		t.Fatalf("expected 'second turn' in sessions show output:\n%s", out.String())
	}
}

func TestAgentStreamJSONAndSchema(t *testing.T) {
	cwd := t.TempDir()
	dataDir := t.TempDir()

	var replyText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunk := aicode.InnerGenerateContentResponse{
			Candidates: []*aicode.Candidate{{
				Content:      &aicode.Content{Role: "model", Parts: []*aicode.Part{{Text: replyText}}},
				FinishReason: "STOP",
			}},
		}
		b, _ := json.Marshal(chunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
	}))
	defer srv.Close()

	t.Setenv("GEMINI_API_KEY", "test-gemini-key")
	t.Setenv("GEMINI_API_BASE_URL", srv.URL)

	schema := `{"type":"object","required":["status","count"],"properties":{"status":{"type":"string"},"count":{"type":"integer"}}}`
	replyText = `{"status":"ok","count":3}`

	var out, errBuf bytes.Buffer
	code := run(context.Background(), []string{
		"agent", "-p", "return json", "--cwd", cwd, "--data-dir", dataDir, "--file",
		"--output-format", "stream-json", "--json-schema", schema, "--thoughts=false",
	}, nil, &out, &errBuf)
	if code != 0 {
		t.Fatalf("agent stream-json exit=%d stderr=%s out=%s", code, errBuf.String(), out.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected >= 3 NDJSON lines, got %d:\n%s", len(lines), out.String())
	}
	if !strings.Contains(lines[0], `"type":"init"`) || !strings.Contains(lines[len(lines)-1], `"status":"success"`) {
		t.Fatalf("unexpected NDJSON stream:\n%s", out.String())
	}

	// Schema mismatch must fail with exit code 1 and status=error.
	replyText = `{"status":"ok"}`
	out.Reset()
	errBuf.Reset()
	code = run(context.Background(), []string{
		"agent", "-p", "return bad json", "--cwd", cwd, "--data-dir", dataDir, "--file",
		"--output-format", "stream-json", "--json-schema", schema, "--thoughts=false",
	}, nil, &out, &errBuf)
	if code != 1 {
		t.Fatalf("expected exit 1 on schema mismatch, got %d", code)
	}
	if !strings.Contains(out.String(), `"status":"error"`) {
		t.Fatalf("expected error status in stream-json output:\n%s", out.String())
	}
}

func TestSwarmRunCommand(t *testing.T) {
	cwd := t.TempDir()
	swarmDir := filepath.Join(t.TempDir(), "swarm")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunk := aicode.InnerGenerateContentResponse{
			Candidates: []*aicode.Candidate{{
				Content:      &aicode.Content{Role: "model", Parts: []*aicode.Part{{Text: "Coordinator finished goal."}}},
				FinishReason: "STOP",
			}},
		}
		b, _ := json.Marshal(chunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
	}))
	defer srv.Close()

	t.Setenv("GEMINI_API_KEY", "test-gemini-key")
	t.Setenv("GEMINI_API_BASE_URL", srv.URL)

	var out, errBuf bytes.Buffer
	code := run(context.Background(), []string{
		"swarm", "run", "-p", "complete demo task", "--cwd", cwd, "--swarm-dir", swarmDir, "--json",
	}, nil, &out, &errBuf)
	if code != 0 {
		t.Fatalf("swarm run exit=%d stderr=%s", code, errBuf.String())
	}
	var res swarmrun.SwarmRunResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal swarm run result: %v\n%s", err, out.String())
	}
	if res.FinalText != "Coordinator finished goal." {
		t.Fatalf("unexpected FinalText: %q", res.FinalText)
	}
	if _, err := os.Stat(filepath.Join(swarmDir, "board.jsonl")); err != nil {
		t.Fatalf("expected board.jsonl in swarmDir: %v", err)
	}
}
