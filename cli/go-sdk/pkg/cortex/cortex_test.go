package cortex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"antigravity.dev/agy-sdk/pkg/aicode"
)

type scriptedStreamer struct {
	turns [][]*aicode.SSEChunk
	reqs  []*aicode.GeminiRequest
}

func (s *scriptedStreamer) Stream(_ context.Context, _ string, req *aicode.GeminiRequest, onChunk func(*aicode.SSEChunk) error) error {
	idx := len(s.reqs)
	s.reqs = append(s.reqs, req)
	if idx >= len(s.turns) {
		return fmt.Errorf("unexpected stream call #%d", idx+1)
	}
	for _, ch := range s.turns[idx] {
		if err := onChunk(ch); err != nil {
			return err
		}
	}
	return nil
}

func TestEngineMultiTurnReadEditRunAndTrajectory(t *testing.T) {
	ws := t.TempDir()
	targetFile := filepath.Join(ws, "hello.txt")
	if err := os.WriteFile(targetFile, []byte("hello world\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	trajPath := filepath.Join(ws, ".transcript", "transcript.jsonl")
	traj, err := NewTrajectoryRecorder(trajPath)
	if err != nil {
		t.Fatalf("NewTrajectoryRecorder: %v", err)
	}
	defer traj.Close()

	streamer := &scriptedStreamer{
		turns: [][]*aicode.SSEChunk{
			// Turn 1: thought + read_file
			{
				{Response: &aicode.InnerGenerateContentResponse{
					Candidates: []*aicode.Candidate{{
						Content: &aicode.Content{
							Role: "model",
							Parts: []*aicode.Part{
								{Text: "Let me inspect hello.txt.", Thought: true, ThoughtSignature: "sig-1"},
								{FunctionCall: &aicode.FunctionCall{
									ID:   "call-1",
									Name: "read_file",
									Args: map[string]any{"Path": "hello.txt"},
								}},
							},
						},
					}},
					UsageMetadata: &aicode.UsageMetadata{PromptTokenCount: 10, CandidatesTokenCount: 5, TotalTokenCount: 15},
				}},
			},
			// Turn 2: edit_file
			{
				{Response: &aicode.InnerGenerateContentResponse{
					Candidates: []*aicode.Candidate{{
						Content: &aicode.Content{
							Role: "model",
							Parts: []*aicode.Part{
								{FunctionCall: &aicode.FunctionCall{
									ID:   "call-2",
									Name: "edit_file",
									Args: map[string]any{
										"Path":        "hello.txt",
										"Target":      "world",
										"Replacement": "cortex",
									},
								}},
							},
						},
					}},
				}},
			},
			// Turn 3: run_command
			{
				{Response: &aicode.InnerGenerateContentResponse{
					Candidates: []*aicode.Candidate{{
						Content: &aicode.Content{
							Role: "model",
							Parts: []*aicode.Part{
								{FunctionCall: &aicode.FunctionCall{
									ID:   "call-3",
									Name: "run_command",
									Args: map[string]any{"CommandLine": "cat hello.txt"},
								}},
							},
						},
					}},
				}},
			},
			// Turn 4: final answer
			{
				{Response: &aicode.InnerGenerateContentResponse{
					Candidates: []*aicode.Candidate{{
						FinishReason: "STOP",
						Content: &aicode.Content{
							Role: "model",
							Parts: []*aicode.Part{
								{Text: "Updated hello.txt "},
								{Text: "to say hello cortex."},
							},
						},
					}},
					UsageMetadata: &aicode.UsageMetadata{PromptTokenCount: 40, CandidatesTokenCount: 12, TotalTokenCount: 52},
				}},
			},
		},
	}

	var events []EventType
	engine := &Engine{
		Streamer:     streamer,
		Model:        "gemini-4-hydrogen-medium",
		Registry:     NewDefaultRegistry(),
		Guard:        NewPermissionGuard(ws, ModeAgent),
		SystemPrompt: BuildSystemPrompt(ws, ModeAgent, "Be brief."),
		Trajectory:   traj,
		OnEvent: func(ev Event) {
			events = append(events, ev.Type)
		},
	}

	history, summary, err := engine.RunTurn(context.Background(), nil, "Update hello.txt and verify it.")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	if summary.Steps != 4 {
		t.Fatalf("expected 4 steps, got %d", summary.Steps)
	}
	if summary.ToolCalls != 3 {
		t.Fatalf("expected 3 tool calls, got %d", summary.ToolCalls)
	}
	if summary.FinalText != "Updated hello.txt to say hello cortex." {
		t.Fatalf("unexpected FinalText: %q", summary.FinalText)
	}
	if summary.FinishReason != "STOP" {
		t.Fatalf("unexpected FinishReason: %q", summary.FinishReason)
	}
	if summary.Usage == nil || summary.Usage.TotalTokenCount != 52 {
		t.Fatalf("unexpected Usage: %+v", summary.Usage)
	}

	gotBytes, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("read edited file: %v", err)
	}
	if string(gotBytes) != "hello cortex\n" {
		t.Fatalf("unexpected edited file content: %q", string(gotBytes))
	}

	// Verify history contains user + (model + functionResponse)*3 + final model = 8 contents.
	if len(history) != 8 {
		t.Fatalf("expected 8 history turns, got %d", len(history))
	}
	if history[1].Parts[0].ThoughtSignature != "sig-1" {
		t.Fatalf("expected ThoughtSignature preserved, got %q", history[1].Parts[0].ThoughtSignature)
	}

	// Verify events included thought, tool_start, tool_done, text, usage.
	eventSet := make(map[EventType]bool)
	for _, et := range events {
		eventSet[et] = true
	}
	for _, want := range []EventType{EventThought, EventToolStart, EventToolDone, EventText, EventUsage} {
		if !eventSet[want] {
			t.Fatalf("missing expected event %q in %v", want, events)
		}
	}

	// Verify transcript.jsonl records.
	if err := traj.Close(); err != nil {
		t.Fatalf("close traj: %v", err)
	}
	f, err := os.Open(trajPath)
	if err != nil {
		t.Fatalf("open transcript: %v", err)
	}
	defer f.Close()

	var stepTypes []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var st TrajectoryStep
		if err := json.Unmarshal(scanner.Bytes(), &st); err != nil {
			t.Fatalf("unmarshal transcript line: %v", err)
		}
		stepTypes = append(stepTypes, st.Type)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan transcript: %v", err)
	}
	// 1 USER_INPUT + 4 PLANNER_RESPONSE + 3 TOOL_RESULT = 8 lines.
	if len(stepTypes) != 8 {
		t.Fatalf("expected 8 transcript steps, got %d (%v)", len(stepTypes), stepTypes)
	}
	if stepTypes[0] != StepTypeUserInput || stepTypes[1] != StepTypePlannerResponse || stepTypes[2] != StepTypeToolResult {
		t.Fatalf("unexpected transcript sequence: %v", stepTypes)
	}
}

func TestBuiltinWorkspaceTools(t *testing.T) {
	ws := t.TempDir()
	reg := NewDefaultRegistry()
	guard := NewPermissionGuard(ws, ModeAgent)
	tc := ToolContext{WorkspaceRoot: ws, Guard: guard, Mode: ModeAgent}
	ctx := context.Background()

	// write_file: create nested file
	wf, _ := reg.Get("write_file")
	res, err := wf.Execute(ctx, tc, map[string]any{
		"Path":    "sub/notes.txt",
		"Content": "alpha\nbeta\n",
	})
	if err != nil {
		t.Fatalf("write_file create: %v", err)
	}
	if !res.IsDiff || res.NewText != "alpha\nbeta\n" {
		t.Fatalf("unexpected write_file result: %+v", res)
	}

	// write_file: reject existing without Overwrite/Append
	if _, err := wf.Execute(ctx, tc, map[string]any{
		"Path":    "sub/notes.txt",
		"Content": "gamma\n",
	}); err == nil {
		t.Fatal("expected write_file to fail on existing file without Overwrite or Append")
	}

	// write_file: append
	res, err = wf.Execute(ctx, tc, map[string]any{
		"Path":    "sub/notes.txt",
		"Content": "gamma\n",
		"Append":  true,
	})
	if err != nil {
		t.Fatalf("write_file append: %v", err)
	}
	if res.OldText != "alpha\nbeta\n" || res.NewText != "alpha\nbeta\ngamma\n" {
		t.Fatalf("unexpected append diff: old=%q new=%q", res.OldText, res.NewText)
	}

	// read_file: line range
	rf, _ := reg.Get("read_file")
	res, err = rf.Execute(ctx, tc, map[string]any{
		"Path":      "sub/notes.txt",
		"StartLine": 2,
		"EndLine":   3,
	})
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if res.Text != "2: beta\n3: gamma\n" {
		t.Fatalf("unexpected read_file output: %q", res.Text)
	}

	// list_dir
	ld, _ := reg.Get("list_dir")
	res, err = ld.Execute(ctx, tc, map[string]any{"Path": "."})
	if err != nil {
		t.Fatalf("list_dir: %v", err)
	}
	if !strings.Contains(res.Text, "dir\tsub\t0") {
		t.Fatalf("expected sub dir in list_dir output, got: %q", res.Text)
	}

	// Create .git and node_modules to verify find_by_name and grep_search skip them.
	for _, skipDir := range []string{".git", "node_modules"} {
		if err := os.MkdirAll(filepath.Join(ws, skipDir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(ws, skipDir, "ignored.txt"), []byte("beta needle\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// find_by_name
	fbn, _ := reg.Get("find_by_name")
	res, err = fbn.Execute(ctx, tc, map[string]any{
		"SearchDirectory": ".",
		"Pattern":         "*.txt",
	})
	if err != nil {
		t.Fatalf("find_by_name: %v", err)
	}
	if strings.TrimSpace(res.Text) != filepath.Join("sub", "notes.txt") {
		t.Fatalf("unexpected find_by_name output: %q", res.Text)
	}

	// grep_search: literal & regex & 50-match cap
	gs, _ := reg.Get("grep_search")
	res, err = gs.Execute(ctx, tc, map[string]any{
		"SearchPath":      ".",
		"Query":           "BETA",
		"CaseInsensitive": true,
	})
	if err != nil {
		t.Fatalf("grep_search: %v", err)
	}
	if !strings.Contains(res.Text, "notes.txt:2: beta") || strings.Contains(res.Text, "ignored.txt") {
		t.Fatalf("unexpected grep_search output: %q", res.Text)
	}

	var manyLines strings.Builder
	for i := 1; i <= 75; i++ {
		fmt.Fprintf(&manyLines, "match-line-%d\n", i)
	}
	if err := os.WriteFile(filepath.Join(ws, "many.txt"), []byte(manyLines.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = gs.Execute(ctx, tc, map[string]any{
		"SearchPath": "many.txt",
		"Query":      `^match-line-\d+$`,
		"IsRegex":    true,
	})
	if err != nil {
		t.Fatalf("grep_search regex: %v", err)
	}
	if count, _ := res.Output["count"].(int); count != 50 {
		t.Fatalf("expected grep_search cap at 50 matches, got %v", res.Output["count"])
	}
}

func TestReadOnlyModesAndPathTraversal(t *testing.T) {
	ws := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "local.txt"), []byte("local"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Symlink inside workspace pointing outside.
	symlinkPath := filepath.Join(ws, "escape-link.txt")
	if err := os.Symlink(outsideFile, symlinkPath); err != nil {
		t.Fatal(err)
	}

	reg := NewDefaultRegistry()
	roTools := reg.Tools(true)
	if len(roTools) != 1 || len(roTools[0].FunctionDeclarations) != 4 {
		t.Fatalf("expected 4 read-only tools, got %+v", roTools)
	}

	for _, mode := range []Mode{ModePlan, ModeAsk} {
		guard := NewPermissionGuard(ws, mode)
		for _, mutating := range []string{"write_file", "edit_file", "run_command"} {
			tool, ok := reg.Get(mutating)
			if !ok {
				t.Fatalf("missing tool %q", mutating)
			}
			if err := guard.CheckTool(tool); !errors.Is(err, ErrPermissionDenied) {
				t.Fatalf("mode %q: expected ErrPermissionDenied for %q, got %v", mode, mutating, err)
			}
		}

		// Path traversal via .. and symlink must be rejected.
		if _, err := guard.ResolvePath("../outside.txt"); !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("expected .. traversal to be denied, got %v", err)
		}
		if _, err := guard.ResolvePath("escape-link.txt"); !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("expected symlink escape to be denied, got %v", err)
		}

		// Engine.RunTurn in plan/ask mode must deny a model-emitted mutating tool call.
		streamer := &scriptedStreamer{
			turns: [][]*aicode.SSEChunk{
				{
					{Response: &aicode.InnerGenerateContentResponse{
						Candidates: []*aicode.Candidate{{
							Content: &aicode.Content{
								Role: "model",
								Parts: []*aicode.Part{{
									FunctionCall: &aicode.FunctionCall{
										ID:   "bad-1",
										Name: "edit_file",
										Args: map[string]any{"Path": "local.txt", "Target": "local", "Replacement": "hacked"},
									},
								}},
							},
						}},
					}},
				},
				{
					{Response: &aicode.InnerGenerateContentResponse{
						Candidates: []*aicode.Candidate{{
							FinishReason: "STOP",
							Content: &aicode.Content{
								Role:  "model",
								Parts: []*aicode.Part{{Text: "Cannot edit in read-only mode."}},
							},
						}},
					}},
				},
			},
		}
		engine := &Engine{
			Streamer: streamer,
			Registry: reg,
			Guard:    guard,
		}
		history, summary, err := engine.RunTurn(context.Background(), nil, "Edit local.txt")
		if err != nil {
			t.Fatalf("mode %q RunTurn: %v", mode, err)
		}
		if summary.FinalText != "Cannot edit in read-only mode." {
			t.Fatalf("unexpected final text: %q", summary.FinalText)
		}
		// Verify the FunctionResponse reported permission denied and local.txt is untouched.
		respMap := history[2].Parts[0].FunctionResponse.Response
		errStr, _ := respMap["error"].(string)
		if !strings.Contains(errStr, "permission denied") {
			t.Fatalf("expected permission denied in FunctionResponse, got %v", respMap)
		}
		data, _ := os.ReadFile(filepath.Join(ws, "local.txt"))
		if string(data) != "local" {
			t.Fatalf("local.txt was modified in %q mode: %q", mode, string(data))
		}
	}

	// AdditionalDirs allows access to outside dir.
	guardWithExtra := NewPermissionGuard(ws, ModeAgent, outside)
	if _, err := guardWithExtra.ResolvePath(outsideFile); err != nil {
		t.Fatalf("expected AdditionalDirs to allow %q, got %v", outsideFile, err)
	}
}
