package acp

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"antigravity.dev/agy-sdk/pkg/aicode"
	"antigravity.dev/agy-sdk/pkg/cortex"
)

type stubStreamer struct {
	chunks []*aicode.SSEChunk
	delay  time.Duration
}

func (s *stubStreamer) Stream(ctx context.Context, model string, req *aicode.GeminiRequest, onChunk func(*aicode.SSEChunk) error) error {
	for _, ch := range s.chunks {
		if s.delay > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(s.delay):
			}
		}
		if err := onChunk(ch); err != nil {
			return err
		}
	}
	return nil
}

func TestACPServerNDJSONSessionPrompt(t *testing.T) {
	streamer := &stubStreamer{
		chunks: []*aicode.SSEChunk{
			{
				Response: &aicode.InnerGenerateContentResponse{
					Candidates: []*aicode.Candidate{{
						Content: &aicode.Content{Parts: []*aicode.Part{
							{Text: "checking file", Thought: true},
						}},
					}},
				},
			},
			{
				Response: &aicode.InnerGenerateContentResponse{
					Candidates: []*aicode.Candidate{{
						Content: &aicode.Content{Parts: []*aicode.Part{
							{FunctionCall: &aicode.FunctionCall{
								ID:   "tc-1",
								Name: "read_file",
								Args: map[string]any{"path": "go.mod", "line": 1},
							}},
						}},
					}},
				},
			},
			{
				Response: &aicode.InnerGenerateContentResponse{
					Candidates: []*aicode.Candidate{{
						Content: &aicode.Content{Parts: []*aicode.Part{
							{Text: "All good!"},
						}},
						FinishReason: "STOP",
					}},
					UsageMetadata: &aicode.UsageMetadata{
						PromptTokenCount:     10,
						CandidatesTokenCount: 4,
						ThoughtsTokenCount:   2,
						TotalTokenCount:      16,
					},
				},
			},
		},
	}

	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	srv := NewServer(ServerConfig{Version: "1.5.10-test", Streamer: streamer})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	srvDone := make(chan error, 1)
	go func() {
		srvDone <- srv.Serve(ctx, c2sR, s2cW)
	}()

	var notifMu sync.Mutex
	var updateTypes []string
	var lastToolTitle string

	clientConn := NewConn(NewStream(s2cR, c2sW), func(_ context.Context, r *Request) {
		if r.Method != "session/update" {
			return
		}
		var envelope struct {
			SessionID string          `json:"sessionId"`
			Update    json.RawMessage `json:"update"`
		}
		if err := json.Unmarshal(r.Params, &envelope); err != nil {
			return
		}
		var header struct {
			SessionUpdate string `json:"sessionUpdate"`
			Title         string `json:"title"`
		}
		if err := json.Unmarshal(envelope.Update, &header); err == nil {
			notifMu.Lock()
			updateTypes = append(updateTypes, header.SessionUpdate)
			if header.SessionUpdate == "tool_call" {
				lastToolTitle = header.Title
			}
			notifMu.Unlock()
		}
	})
	go func() {
		_ = clientConn.Serve(ctx)
	}()

	var initRes InitializeResult
	if err := clientConn.Call(ctx, "initialize", InitializeParams{ProtocolVersion: 1}, &initRes); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	if initRes.ProtocolVersion != 1 || initRes.AgentInfo.Version != "1.5.10-test" {
		t.Fatalf("unexpected initialize result: %+v", initRes)
	}

	var sessRes NewSessionResult
	if err := clientConn.Call(ctx, "session/new", NewSessionParams{Cwd: t.TempDir()}, &sessRes); err != nil {
		t.Fatalf("session/new failed: %v", err)
	}
	if !strings.HasPrefix(sessRes.SessionID, "sess-") {
		t.Fatalf("unexpected sessionId: %q", sessRes.SessionID)
	}

	var promptRes PromptResult
	err := clientConn.Call(ctx, "session/prompt", PromptParams{
		SessionID: sessRes.SessionID,
		Prompt:    []ContentBlock{NewTextContentBlock("check go.mod")},
	}, &promptRes)
	if err != nil {
		t.Fatalf("session/prompt failed: %v", err)
	}
	if promptRes.StopReason != "end_turn" || promptRes.Usage == nil || promptRes.Usage.TotalTokens != 16 {
		t.Fatalf("unexpected promptResult: %+v", promptRes)
	}

	notifMu.Lock()
	gotUpdates := append([]string(nil), updateTypes...)
	gotTitle := lastToolTitle
	notifMu.Unlock()

	if len(gotUpdates) != 3 || gotUpdates[0] != "agent_thought_chunk" || gotUpdates[1] != "tool_call" || gotUpdates[2] != "agent_message_chunk" {
		t.Fatalf("unexpected session/update sequence: %v", gotUpdates)
	}
	if gotTitle != "Read go.mod" {
		t.Fatalf("expected tool_call title 'Read go.mod', got %q", gotTitle)
	}

	_ = c2sW.Close()
	_ = s2cW.Close()
	<-srvDone
}

func TestACPContentLengthFramingAndCancel(t *testing.T) {
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()

	slowStreamer := &stubStreamer{
		delay: 100 * time.Millisecond,
		chunks: []*aicode.SSEChunk{
			{Response: &aicode.InnerGenerateContentResponse{Candidates: []*aicode.Candidate{{Content: &aicode.Content{Parts: []*aicode.Part{{Text: "one"}}}}}}},
			{Response: &aicode.InnerGenerateContentResponse{Candidates: []*aicode.Candidate{{Content: &aicode.Content{Parts: []*aicode.Part{{Text: "two"}}}}}}},
		},
	}

	srv := NewServer(ServerConfig{Streamer: slowStreamer})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { _ = srv.Serve(ctx, c2sR, s2cW) }()

	clientStream := NewStream(s2cR, c2sW)
	clientStream.SetHeaderFraming(true)
	clientConn := NewConn(clientStream, nil)
	go func() { _ = clientConn.Serve(ctx) }()

	var sessRes NewSessionResult
	if err := clientConn.Call(ctx, "session/new", NewSessionParams{Cwd: "/tmp"}, &sessRes); err != nil {
		t.Fatalf("session/new failed: %v", err)
	}
	if !clientStream.hasHeaders.Load() {
		t.Fatal("expected Content-Length framing preserved")
	}

	if err := clientConn.Call(ctx, "session/set_model", SetModelParams{
		SessionID: sessRes.SessionID,
		ModelID:   "gemini-4-hydrogen-medium",
	}, nil); err != nil {
		t.Fatalf("session/set_model failed: %v", err)
	}

	var listRes ListSessionsResult
	if err := clientConn.Call(ctx, "session/list", struct{}{}, &listRes); err != nil {
		t.Fatalf("session/list failed: %v", err)
	}
	if len(listRes.Sessions) != 1 || listRes.Sessions[0].SessionID != sessRes.SessionID {
		t.Fatalf("unexpected listRes: %+v", listRes)
	}

	promptDone := make(chan PromptResult, 1)
	go func() {
		var res PromptResult
		_ = clientConn.Call(ctx, "session/prompt", PromptParams{
			SessionID: sessRes.SessionID,
			Prompt:    []ContentBlock{NewTextContentBlock("slow")},
		}, &res)
		promptDone <- res
	}()

	time.Sleep(20 * time.Millisecond)
	if err := clientConn.Call(ctx, "session/cancel", CancelSessionParams{SessionID: sessRes.SessionID}, nil); err != nil {
		t.Fatalf("session/cancel failed: %v", err)
	}

	got := <-promptDone
	if got.StopReason != "cancelled" {
		t.Fatalf("expected stopReason=cancelled, got %q", got.StopReason)
	}

	_ = c2sW.Close()
	_ = s2cW.Close()
}

func TestModelGroupingAndStepViewHelpers(t *testing.T) {
	if got := StripEffortSuffix("Gemini 4 Hydrogen (Medium thinking)", "medium"); got != "Gemini 4 Hydrogen" {
		t.Fatalf("StripEffortSuffix got %q", got)
	}
	opts := GroupModels([]ModelInfo{
		{ModelID: "gemini-4-hydrogen-low", Name: "Gemini 4 Hydrogen (Low)"},
		{ModelID: "gemini-4-hydrogen-medium", Name: "Gemini 4 Hydrogen (Medium)"},
		{ModelID: "gemini-4-hydrogen-high", Name: "Gemini 4 Hydrogen (High)"},
	}, "gemini-4-hydrogen-medium")
	if len(opts) != 2 || opts[0].ID != "model" || opts[1].ID != "thought_level" || opts[1].CurrentValue != "medium" {
		t.Fatalf("unexpected grouped options: %+v", opts)
	}

	kind, title, locs := KindTitleAndLocations("edit_file", map[string]any{"path": "/a/b.go", "line": 42})
	if kind != "edit" || title != "Edit /a/b.go" || len(locs) != 1 || *locs[0].Line != 42 {
		t.Fatalf("unexpected stepview classification: kind=%q title=%q locs=%+v", kind, title, locs)
	}
}

type funcStreamer struct {
	fn func(ctx context.Context, model string, req *aicode.GeminiRequest, onChunk func(*aicode.SSEChunk) error) error
}

func (f *funcStreamer) Stream(ctx context.Context, model string, req *aicode.GeminiRequest, onChunk func(*aicode.SSEChunk) error) error {
	return f.fn(ctx, model, req, onChunk)
}

func TestACPMultiTurnToolExecutionLoop(t *testing.T) {
	dir := t.TempDir()
	targetFile := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(targetFile, []byte("hello old\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var turn int
	streamer := &funcStreamer{
		fn: func(_ context.Context, _ string, req *aicode.GeminiRequest, onChunk func(*aicode.SSEChunk) error) error {
			turn++
			switch turn {
			case 1:
				if len(req.Tools) == 0 || len(req.Tools[0].FunctionDeclarations) == 0 {
					t.Fatalf("expected tool declarations on turn 1")
				}
				return onChunk(&aicode.SSEChunk{
					Response: &aicode.InnerGenerateContentResponse{
						Candidates: []*aicode.Candidate{{
							Content: &aicode.Content{
								Role: "model",
								Parts: []*aicode.Part{{
									FunctionCall: &aicode.FunctionCall{
										ID:   "call-edit-1",
										Name: "edit_file",
										Args: map[string]any{
											"Path":        "hello.txt",
											"Target":      "old",
											"Replacement": "new",
										},
									},
								}},
							},
						}},
					},
				})
			case 2:
				var sawFuncResp bool
				for _, c := range req.Contents {
					for _, p := range c.Parts {
						if p.FunctionResponse != nil && p.FunctionResponse.Name == "edit_file" {
							sawFuncResp = true
						}
					}
				}
				if !sawFuncResp {
					t.Fatalf("expected FunctionResponse for edit_file in turn 2 contents")
				}
				return onChunk(&aicode.SSEChunk{
					Response: &aicode.InnerGenerateContentResponse{
						Candidates: []*aicode.Candidate{{
							Content: &aicode.Content{
								Role:  "model",
								Parts: []*aicode.Part{{Text: "Updated hello.txt"}},
							},
							FinishReason: "STOP",
						}},
						UsageMetadata: &aicode.UsageMetadata{
							PromptTokenCount:     12,
							CandidatesTokenCount: 5,
							TotalTokenCount:      17,
						},
					},
				})
			case 3:
				// In plan mode, edit_file should not be declared and if called anyway should fail permission check.
				return onChunk(&aicode.SSEChunk{
					Response: &aicode.InnerGenerateContentResponse{
						Candidates: []*aicode.Candidate{{
							Content: &aicode.Content{
								Role: "model",
								Parts: []*aicode.Part{{
									FunctionCall: &aicode.FunctionCall{
										ID:   "call-edit-2",
										Name: "edit_file",
										Args: map[string]any{"Path": "hello.txt", "Target": "new", "Replacement": "bad"},
									},
								}},
							},
						}},
					},
				})
			default:
				return onChunk(&aicode.SSEChunk{
					Response: &aicode.InnerGenerateContentResponse{
						Candidates: []*aicode.Candidate{{
							Content: &aicode.Content{
								Role:  "model",
								Parts: []*aicode.Part{{Text: "Read-only mode blocked edit"}},
							},
							FinishReason: "STOP",
						}},
					},
				})
			}
		},
	}

	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	srv := NewServer(ServerConfig{
		Version:      "1.5.10-test",
		Streamer:     streamer,
		ToolRegistry: cortex.NewDefaultRegistry(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	srvDone := make(chan error, 1)
	go func() {
		srvDone <- srv.Serve(ctx, c2sR, s2cW)
	}()

	type capturedUpdate struct {
		SessionUpdate string          `json:"sessionUpdate"`
		Status        string          `json:"status"`
		ToolCallID    string          `json:"toolCallId"`
		Content       json.RawMessage `json:"content"`
	}
	var notifMu sync.Mutex
	var updates []capturedUpdate

	clientConn := NewConn(NewStream(s2cR, c2sW), func(_ context.Context, r *Request) {
		if r.Method != "session/update" {
			return
		}
		var env struct {
			Update json.RawMessage `json:"update"`
		}
		if err := json.Unmarshal(r.Params, &env); err != nil {
			return
		}
		var u capturedUpdate
		if err := json.Unmarshal(env.Update, &u); err == nil {
			notifMu.Lock()
			updates = append(updates, u)
			notifMu.Unlock()
		}
	})
	go func() { _ = clientConn.Serve(ctx) }()

	var sessRes NewSessionResult
	if err := clientConn.Call(ctx, "session/new", NewSessionParams{Cwd: dir}, &sessRes); err != nil {
		t.Fatalf("session/new failed: %v", err)
	}

	var promptRes PromptResult
	if err := clientConn.Call(ctx, "session/prompt", PromptParams{
		SessionID: sessRes.SessionID,
		Prompt:    []ContentBlock{NewTextContentBlock("update hello.txt")},
	}, &promptRes); err != nil {
		t.Fatalf("session/prompt failed: %v", err)
	}
	if promptRes.StopReason != "end_turn" || promptRes.Usage == nil || promptRes.Usage.TotalTokens != 17 {
		t.Fatalf("unexpected promptRes: %+v", promptRes)
	}

	gotBytes, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(gotBytes) != "hello new\n" {
		t.Fatalf("expected hello.txt to be updated, got %q", string(gotBytes))
	}

	var turn1Updates []capturedUpdate
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		notifMu.Lock()
		if len(updates) >= 3 {
			turn1Updates = append([]capturedUpdate(nil), updates...)
			updates = nil
			notifMu.Unlock()
			break
		}
		notifMu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	if turn1Updates == nil {
		notifMu.Lock()
		turn1Updates = append([]capturedUpdate(nil), updates...)
		updates = nil
		notifMu.Unlock()
	}

	var sawInProgress, sawCompletedDiff, sawMsg bool
	for _, u := range turn1Updates {
		switch u.SessionUpdate {
		case "tool_call":
			if u.Status == "in_progress" && u.ToolCallID == "call-edit-1" {
				sawInProgress = true
			}
		case "tool_call_update":
			var items []ToolCallContent
			if len(u.Content) > 0 {
				_ = json.Unmarshal(u.Content, &items)
			}
			if u.Status == "completed" && u.ToolCallID == "call-edit-1" && len(items) > 0 && items[0].Type == "diff" {
				sawCompletedDiff = true
			}
		case "agent_message_chunk":
			sawMsg = true
		}
	}
	if !sawInProgress || !sawCompletedDiff || !sawMsg {
		t.Fatalf("missing expected updates: inProgress=%v completedDiff=%v msg=%v updates=%+v",
			sawInProgress, sawCompletedDiff, sawMsg, turn1Updates)
	}

	// Switch to plan mode and verify mutating tool is blocked with status=failed.
	if err := clientConn.Call(ctx, "session/set_mode", SetModeParams{
		SessionID: sessRes.SessionID,
		ModeID:    "plan",
	}, nil); err != nil {
		t.Fatalf("session/set_mode failed: %v", err)
	}

	var planRes PromptResult
	if err := clientConn.Call(ctx, "session/prompt", PromptParams{
		SessionID: sessRes.SessionID,
		Prompt:    []ContentBlock{NewTextContentBlock("try edit in plan mode")},
	}, &planRes); err != nil {
		t.Fatalf("session/prompt in plan mode failed: %v", err)
	}

	var planUpdates []capturedUpdate
	planDeadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(planDeadline) {
		notifMu.Lock()
		for _, u := range updates {
			if u.SessionUpdate == "tool_call_update" && u.Status == "failed" {
				planUpdates = append([]capturedUpdate(nil), updates...)
				break
			}
		}
		notifMu.Unlock()
		if planUpdates != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if planUpdates == nil {
		notifMu.Lock()
		planUpdates = append([]capturedUpdate(nil), updates...)
		notifMu.Unlock()
	}

	var sawFailed bool
	for _, u := range planUpdates {
		if u.SessionUpdate == "tool_call_update" && u.Status == "failed" {
			sawFailed = true
		}
	}
	if !sawFailed {
		t.Fatalf("expected failed tool_call_update in plan mode, got %+v", planUpdates)
	}

	_ = c2sW.Close()
	_ = s2cW.Close()
	<-srvDone
}
