package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"antigravity.dev/agy-sdk/pkg/cortex"
)

func TestConfigStoreCRUDAndPermissions(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "mcp_config.json")
	store := NewConfigStore(cfgPath)

	if err := store.Load(); err != nil {
		t.Fatalf("Load on missing file failed: %v", err)
	}
	if got := store.List(); len(got) != 0 {
		t.Fatalf("expected 0 servers, got %d", len(got))
	}

	err := store.Add(ServerConfig{
		Name:    "local-fs",
		Type:    "stdio",
		Command: "npx",
		Args:    []string{"-y", "@modelcontextprotocol/server-filesystem"},
		Env:     map[string]string{"ROOT": "/tmp"},
	})
	if err != nil {
		t.Fatalf("Add stdio failed: %v", err)
	}

	err = store.Add(ServerConfig{
		Name:    "remote-api",
		Type:    "http",
		URL:     "https://example.com/mcp",
		Headers: map[string]string{"Authorization": "Bearer secret"},
	})
	if err != nil {
		t.Fatalf("Add http failed: %v", err)
	}

	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("Stat config failed: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected 0600 permissions, got %04o", perm)
	}

	if err := store.SetEnabled("remote-api", false); err != nil {
		t.Fatalf("SetEnabled failed: %v", err)
	}

	// Reload from disk into a fresh store.
	reloaded := NewConfigStore(cfgPath)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("Load reloaded failed: %v", err)
	}
	list := reloaded.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 servers after reload, got %d", len(list))
	}
	if list[0].Name != "local-fs" || list[0].Disabled {
		t.Fatalf("unexpected first server: %+v", list[0])
	}
	if list[1].Name != "remote-api" || !list[1].Disabled || list[1].Headers["Authorization"] != "Bearer secret" {
		t.Fatalf("unexpected second server: %+v", list[1])
	}

	if err := reloaded.Remove("local-fs"); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	var nf *NotFoundError
	if err := reloaded.Remove("missing"); !errors.As(err, &nf) {
		t.Fatalf("expected NotFoundError, got %v", err)
	}
	if got := reloaded.List(); len(got) != 1 || got[0].Name != "remote-api" {
		t.Fatalf("unexpected remaining list: %+v", got)
	}
}

func TestHTTPClientAndBridge(t *testing.T) {
	var sawAuth string
	var sawInitialized bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var req struct {
			JSONRPC string         `json:"jsonrpc"`
			ID      any            `json:"id"`
			Method  string         `json:"method"`
			Params  map[string]any `json:"params"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.Method == "notifications/initialized" {
			sawInitialized = true
			w.WriteHeader(http.StatusAccepted)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "sess-123")

		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": mcpProtocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "mock-http", "version": "1.0"},
			}
		case "tools/list":
			result = map[string]any{
				"tools": []map[string]any{
					{
						"name":        "echo",
						"description": "Echo input text",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"text": map[string]any{"type": "string"},
							},
						},
					},
					{
						"name":        "fail",
						"description": "Always returns tool error",
					},
				},
			}
		case "tools/call":
			name, _ := req.Params["name"].(string)
			args, _ := req.Params["arguments"].(map[string]any)
			if name == "fail" {
				result = map[string]any{
					"content": []map[string]any{{"type": "text", "text": "boom"}},
					"isError": true,
				}
			} else {
				txt, _ := args["text"].(string)
				result = map[string]any{
					"content": []map[string]any{{"type": "text", "text": "echo:" + txt}},
					"isError": false,
				}
			}
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"error":   map[string]any{"code": -32601, "message": "method not found"},
			})
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  result,
		})
	}))
	defer srv.Close()

	client, err := NewHTTPClient(srv.URL, map[string]string{"Authorization": "Bearer tok-123"})
	if err != nil {
		t.Fatalf("NewHTTPClient failed: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if sawAuth != "Bearer tok-123" {
		t.Fatalf("expected custom Authorization header, got %q", sawAuth)
	}
	if !sawInitialized {
		t.Fatal("expected notifications/initialized to be sent")
	}

	reg := cortex.NewRegistry()
	count, err := RegisterMCPTools(ctx, reg, "demo", client)
	if err != nil {
		t.Fatalf("RegisterMCPTools failed: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 registered tools, got %d", count)
	}

	if roTools := reg.Tools(true); len(roTools) != 0 {
		t.Fatalf("expected 0 read-only tools, got %d", len(roTools))
	}
	allTools := reg.Tools(false)
	if len(allTools) != 1 || len(allTools[0].FunctionDeclarations) != 2 {
		t.Fatalf("expected 2 declarations, got %+v", allTools)
	}

	echoTool, ok := reg.Get("mcp__demo__echo")
	if !ok {
		t.Fatal("missing mcp__demo__echo in registry")
	}
	if echoTool.ReadOnly() {
		t.Fatal("expected MCP tool ReadOnly() to be false by default")
	}
	res, err := echoTool.Execute(ctx, cortex.ToolContext{}, map[string]any{"text": "hello"})
	if err != nil {
		t.Fatalf("Execute echo failed: %v", err)
	}
	if res.Text != "echo:hello" {
		t.Fatalf("expected echo:hello, got %q", res.Text)
	}

	failTool, ok := reg.Get("mcp__demo__fail")
	if !ok {
		t.Fatal("missing mcp__demo__fail in registry")
	}
	res, err = failTool.Execute(ctx, cortex.ToolContext{}, nil)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected error containing boom, got res=%+v err=%v", res, err)
	}
}

func TestStdioClientSubprocess(t *testing.T) {
	client, err := NewStdioClient(
		os.Args[0],
		[]string{"-test.run=TestHelperMCPStdioProcess"},
		map[string]string{"GO_WANT_HELPER_PROCESS": "1"},
	)
	if err != nil {
		t.Fatalf("NewStdioClient failed: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("stdio Initialize failed: %v", err)
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("stdio ListTools failed: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "add" {
		t.Fatalf("unexpected stdio tools: %+v", tools)
	}

	out, isErr, err := client.CallTool(ctx, "add", map[string]any{"a": 2, "b": 3})
	if err != nil {
		t.Fatalf("stdio CallTool failed: %v", err)
	}
	if isErr || out != "5" {
		t.Fatalf("expected output \"5\" with isError=false, got %q (isErr=%v)", out, isErr)
	}
}

// TestHelperMCPStdioProcess acts as a mock stdio MCP server when GO_WANT_HELPER_PROCESS=1.
func TestHelperMCPStdioProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params map[string]any  `json:"params"`
		}
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}
		if len(req.ID) == 0 || string(req.ID) == "null" {
			// Notification; no response.
			continue
		}

		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": mcpProtocolVersion,
				"capabilities":    map[string]any{},
				"serverInfo":      map[string]any{"name": "mock-stdio", "version": "1.0"},
			}
		case "tools/list":
			result = map[string]any{
				"tools": []map[string]any{
					{
						"name":        "add",
						"description": "Add two numbers",
					},
				},
			}
		case "tools/call":
			args, _ := req.Params["arguments"].(map[string]any)
			a, _ := args["a"].(float64)
			b, _ := args["b"].(float64)
			result = map[string]any{
				"content": []map[string]any{{"type": "text", "text": formatFloat(a + b)}},
				"isError": false,
			}
		}

		resp, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  result,
		})
		_, _ = os.Stdout.Write(append(resp, '\n'))
	}
	os.Exit(0)
}

func formatFloat(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}
