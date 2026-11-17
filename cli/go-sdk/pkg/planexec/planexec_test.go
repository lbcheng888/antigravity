package planexec

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"antigravity.dev/agy-sdk/pkg/aicode"
	"antigravity.dev/agy-sdk/pkg/cortex"
)

type streamStep struct {
	wantModel string
	checkReq  func(*aicode.GeminiRequest) error
	parts     []*aicode.Part
}

type mockStreamer struct {
	t     *testing.T
	steps []streamStep
	idx   int
}

func (m *mockStreamer) Stream(_ context.Context, model string, req *aicode.GeminiRequest, onChunk func(*aicode.SSEChunk) error) error {
	m.t.Helper()
	if m.idx >= len(m.steps) {
		return fmt.Errorf("unexpected stream call #%d (model=%s)", m.idx+1, model)
	}
	st := m.steps[m.idx]
	m.idx++

	if st.wantModel != "" && model != st.wantModel {
		return fmt.Errorf("step %d: expected model %q, got %q", m.idx, st.wantModel, model)
	}
	if st.checkReq != nil {
		if err := st.checkReq(req); err != nil {
			return fmt.Errorf("step %d req check: %w", m.idx, err)
		}
	}

	return onChunk(&aicode.SSEChunk{
		Response: &aicode.InnerGenerateContentResponse{
			Candidates: []*aicode.Candidate{
				{
					FinishReason: "STOP",
					Content: &aicode.Content{
						Role:  "model",
						Parts: st.parts,
					},
				},
			},
		},
	})
}

func hasTool(req *aicode.GeminiRequest, name string) bool {
	for _, t := range req.Tools {
		for _, fd := range t.FunctionDeclarations {
			if fd.Name == name {
				return true
			}
		}
	}
	return false
}

func TestRunnerDirectAnswerSkipsPhase2(t *testing.T) {
	ws := t.TempDir()
	streamer := &mockStreamer{
		t: t,
		steps: []streamStep{
			{
				wantModel: "plan-model",
				checkReq: func(req *aicode.GeminiRequest) error {
					if !hasTool(req, exitPlanToolName) {
						return fmt.Errorf("expected %s in planner tools", exitPlanToolName)
					}
					if hasTool(req, "write_file") {
						return fmt.Errorf("did not expect mutating write_file in planner tools")
					}
					return nil
				},
				parts: []*aicode.Part{{Text: "This is just a question; no edits needed."}},
			},
		},
	}

	runner := &Runner{
		Streamer:      streamer,
		PlanModel:     "plan-model",
		ExecModel:     "exec-model",
		WorkspaceRoot: ws,
	}

	res, err := runner.Run(context.Background(), nil, "Explain what this repo does")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if res.FinalPhase != PhasePlan {
		t.Fatalf("expected FinalPhase %q, got %q", PhasePlan, res.FinalPhase)
	}
	if res.Plan != "" {
		t.Fatalf("expected empty plan, got %q", res.Plan)
	}
	if res.Rounds != 1 {
		t.Fatalf("expected 1 round, got %d", res.Rounds)
	}
	if res.Summary.FinalText != "This is just a question; no edits needed." {
		t.Fatalf("unexpected FinalText: %q", res.Summary.FinalText)
	}
}

func TestRunnerPlanThenExecAndHandBack(t *testing.T) {
	ws := t.TempDir()
	target := filepath.Join(ws, "hello.txt")

	var phases []string
	streamer := &mockStreamer{
		t: t,
		steps: []streamStep{
			// Round 1, Phase 1: calls exit_plan_mode
			{
				wantModel: "plan-model",
				parts: []*aicode.Part{{
					FunctionCall: &aicode.FunctionCall{
						ID:   "call-1",
						Name: exitPlanToolName,
						Args: map[string]any{"plan": "1. Create hello.txt"},
					},
				}},
			},
			{
				wantModel: "plan-model",
				parts:     []*aicode.Part{{Text: "Handing off to execution."}},
			},
			// Round 1, Phase 2: receives <plan>, calls enter_plan_mode to revise
			{
				wantModel: "exec-model",
				checkReq: func(req *aicode.GeminiRequest) error {
					if !hasTool(req, enterPlanToolName) || !hasTool(req, "write_file") {
						return fmt.Errorf("expected enter_plan_mode and write_file in exec tools")
					}
					if req.SystemInstruction == nil || !strings.Contains(req.SystemInstruction.Parts[0].Text, "<plan>\n1. Create hello.txt\n</plan>") {
						return fmt.Errorf("expected <plan> in exec system instruction")
					}
					return nil
				},
				parts: []*aicode.Part{{
					FunctionCall: &aicode.FunctionCall{
						ID:   "call-2",
						Name: enterPlanToolName,
						Args: map[string]any{"reason": "need exact content"},
					},
				}},
			},
			{
				wantModel: "exec-model",
				parts:     []*aicode.Part{{Text: "Returning to planner."}},
			},
			// Round 2, Phase 1: revised plan
			{
				wantModel: "plan-model",
				parts: []*aicode.Part{{
					FunctionCall: &aicode.FunctionCall{
						ID:   "call-3",
						Name: exitPlanToolName,
						Args: map[string]any{"plan": "1. Write 'world' to hello.txt"},
					},
				}},
			},
			{
				wantModel: "plan-model",
				parts:     []*aicode.Part{{Text: "Revised plan ready."}},
			},
			// Round 2, Phase 2: writes file and finishes
			{
				wantModel: "exec-model",
				parts: []*aicode.Part{{
					FunctionCall: &aicode.FunctionCall{
						ID:   "call-4",
						Name: "write_file",
						Args: map[string]any{"Path": "hello.txt", "Content": "world"},
					},
				}},
			},
			{
				wantModel: "exec-model",
				parts:     []*aicode.Part{{Text: "Wrote hello.txt."}},
			},
		},
	}

	runner := &Runner{
		Streamer:      streamer,
		PlanModel:     "plan-model",
		ExecModel:     "exec-model",
		WorkspaceRoot: ws,
		MaxRounds:     3,
		OnPhaseChange: func(phase Phase, model string) {
			phases = append(phases, string(phase)+":"+model)
		},
	}

	res, err := runner.Run(context.Background(), nil, "Create hello.txt")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if res.Rounds != 2 {
		t.Fatalf("expected 2 rounds, got %d", res.Rounds)
	}
	if res.FinalPhase != PhaseExec {
		t.Fatalf("expected FinalPhase %q, got %q", PhaseExec, res.FinalPhase)
	}
	if res.Plan != "1. Write 'world' to hello.txt" {
		t.Fatalf("unexpected final plan: %q", res.Plan)
	}
	if got := strings.Join(phases, ","); got != "plan:plan-model,exec:exec-model,plan:plan-model,exec:exec-model" {
		t.Fatalf("unexpected phase transitions: %s", got)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("expected hello.txt to be written: %v", err)
	}
	if string(data) != "world" {
		t.Fatalf("expected hello.txt content %q, got %q", "world", string(data))
	}
	if res.Summary.ToolCalls != 4 {
		t.Fatalf("expected 4 total tool calls, got %d", res.Summary.ToolCalls)
	}
	_ = cortex.ModeAgent
}
