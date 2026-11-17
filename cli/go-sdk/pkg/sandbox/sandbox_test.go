package sandbox

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"antigravity.dev/agy-sdk/pkg/auth"
	"antigravity.dev/agy-sdk/pkg/cortex"
)

func TestBuildSeatbeltProfile(t *testing.T) {
	ws := t.TempDir()
	extra := t.TempDir()

	profile := BuildSeatbeltProfile(ws, []string{extra}, false)
	if !strings.Contains(profile, "(version 1)") {
		t.Fatalf("expected (version 1) in profile:\n%s", profile)
	}
	if !strings.Contains(profile, "(deny network*)") {
		t.Fatalf("expected (deny network*) when allowNetwork=false:\n%s", profile)
	}
	if !strings.Contains(profile, "(deny file-write*)") {
		t.Fatalf("expected (deny file-write*) in profile:\n%s", profile)
	}
	resolvedWS, err := filepath.EvalSymlinks(ws)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(profile, resolvedWS) {
		t.Fatalf("expected resolved workspace %q in profile:\n%s", resolvedWS, profile)
	}

	netProfile := BuildSeatbeltProfile(ws, nil, true)
	if strings.Contains(netProfile, "(deny network*)") {
		t.Fatalf("did not expect (deny network*) when allowNetwork=true:\n%s", netProfile)
	}
}

func TestSandboxedRunCommandWorkspaceIsolation(t *testing.T) {
	if runtime.GOOS != "darwin" || !hasSeatbelt() {
		t.Skip("macOS sandbox-exec not available")
	}

	workspace := t.TempDir()
	forbidden := t.TempDir()

	tool := NewSandboxedRunCommandTool(nil, false)
	tc := cortex.ToolContext{
		WorkspaceRoot: workspace,
		Mode:          cortex.ModeAgent,
	}

	allowedFile := filepath.Join(workspace, "allowed.txt")
	res, err := tool.Execute(context.Background(), tc, map[string]any{
		"CommandLine": fmt.Sprintf("echo hello > %q", allowedFile),
	})
	if err != nil {
		t.Fatalf("allowed write Execute error: %v", err)
	}
	if res.ExitCode == nil || *res.ExitCode != 0 {
		t.Fatalf("expected exit code 0 for workspace write, got %v (out=%q)", res.ExitCode, res.Text)
	}
	data, err := os.ReadFile(allowedFile)
	if err != nil || strings.TrimSpace(string(data)) != "hello" {
		t.Fatalf("expected allowed.txt to contain hello, got %q (err=%v)", string(data), err)
	}

	blockedFile := filepath.Join(forbidden, "blocked.txt")
	res, err = tool.Execute(context.Background(), tc, map[string]any{
		"CommandLine": fmt.Sprintf("echo nope > %q", blockedFile),
	})
	if err != nil {
		t.Fatalf("blocked write Execute error: %v", err)
	}
	if res.ExitCode == nil || *res.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code when writing outside workspace, got %v", res.ExitCode)
	}
	if _, statErr := os.Stat(blockedFile); !os.IsNotExist(statErr) {
		t.Fatalf("expected %s to not exist after sandboxed write", blockedFile)
	}
}

func TestTraceProxySSEAndRedaction(t *testing.T) {
	eventReady := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)

		_, _ = fmt.Fprint(w, "data: chunk-1\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		<-eventReady
		_, _ = fmt.Fprint(w, "data: chunk-2\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	upURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}

	var traceBuf bytes.Buffer
	proxy, err := NewTraceProxy(upURL, &traceBuf)
	if err != nil {
		t.Fatal(err)
	}
	proxySrv := httptest.NewServer(proxy)
	defer proxySrv.Close()

	secretAuth := "Bearer super-secret-oauth-token-9999"
	secretKey := "AIzaSySuperSecretKey1234"
	req, err := http.NewRequest(http.MethodPost, proxySrv.URL+"/v1/stream?alt=sse", strings.NewReader(`{"ping":true}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", secretAuth)
	req.Header.Set("x-goog-api-key", secretKey)
	req.Header.Set("X-Custom", "visible")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("proxy request failed: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	line1, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(line1, "chunk-1") {
		t.Fatalf("expected immediate chunk-1 before chunk-2, got %q (err=%v)", line1, err)
	}
	close(eventReady)

	var rest strings.Builder
	for {
		line, err := reader.ReadString('\n')
		rest.WriteString(line)
		if err != nil {
			break
		}
	}
	if !strings.Contains(rest.String(), "chunk-2") {
		t.Fatalf("expected chunk-2 in stream tail, got %q", rest.String())
	}

	rawTrace := traceBuf.String()
	if strings.Contains(rawTrace, secretAuth) || strings.Contains(rawTrace, secretKey) {
		t.Fatalf("raw secret leaked in trace: %s", rawTrace)
	}

	var entry TraceEntry
	if err := json.Unmarshal(bytes.TrimSpace(traceBuf.Bytes()), &entry); err != nil {
		t.Fatalf("decode trace entry: %v (raw=%q)", err, rawTrace)
	}
	if entry.Method != http.MethodPost || entry.Path != "/v1/stream?alt=sse" || entry.Status != http.StatusOK {
		t.Fatalf("unexpected trace entry metadata: %+v", entry)
	}
	if entry.RequestBytes != int64(len(`{"ping":true}`)) || entry.ResponseBytes == 0 {
		t.Fatalf("unexpected byte counts: %+v", entry)
	}
	if entry.RedactedHeaders["Authorization"] != auth.MaskSecret(secretAuth) {
		t.Fatalf("expected masked Authorization, got %q", entry.RedactedHeaders["Authorization"])
	}
	if entry.RedactedHeaders["X-Goog-Api-Key"] != auth.MaskSecret(secretKey) {
		t.Fatalf("expected masked X-Goog-Api-Key, got %q", entry.RedactedHeaders["X-Goog-Api-Key"])
	}
	if entry.RedactedHeaders["X-Custom"] != "visible" {
		t.Fatalf("expected X-Custom to remain visible, got %q", entry.RedactedHeaders["X-Custom"])
	}
}
