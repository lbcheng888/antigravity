// Package planexec implements dual-phase plan-and-execute orchestration over cortex.Engine.
package planexec

import (
	"context"
	"fmt"
	"strings"

	"antigravity.dev/agy-sdk/pkg/aicode"
	"antigravity.dev/agy-sdk/pkg/cortex"
)

const (
	// DefaultMaxRounds limits how many plan->exec->plan hand-backs may occur in one Run.
	DefaultMaxRounds = 3

	exitPlanToolName  = "exit_plan_mode"
	enterPlanToolName = "enter_plan_mode"
)

// Phase identifies the active stage of dual-phase execution.
type Phase string

const (
	PhasePlan Phase = "plan"
	PhaseExec Phase = "exec"
)

// Result holds the final conversation history, aggregated summary, and plan metadata from Runner.Run.
type Result struct {
	History    []*aicode.Content
	Summary    *cortex.TurnSummary
	Plan       string
	Rounds     int
	FinalPhase Phase
}

// Runner orchestrates Phase 1 (read-only planning) and Phase 2 (agent execution) using cortex.Engine.
type Runner struct {
	Streamer         cortex.Streamer
	PlanModel        string
	ExecModel        string
	Registry         *cortex.Registry
	WorkspaceRoot    string
	AdditionalDirs   []string
	Trajectory       *cortex.TrajectoryRecorder
	GenerationConfig *aicode.GenerationConfig
	MaxSteps         int
	MaxRounds        int
	ExtraPrompt      string
	OnEvent          cortex.EventCallback
	OnPhaseChange    func(phase Phase, model string)
}

// Run executes the dual-phase plan-and-execute loop for userPrompt.
func (r *Runner) Run(ctx context.Context, history []*aicode.Content, userPrompt string) (*Result, error) {
	if r == nil || r.Streamer == nil {
		return nil, fmt.Errorf("planexec: streamer is nil")
	}

	baseReg := r.Registry
	if baseReg == nil {
		baseReg = cortex.NewDefaultRegistry()
	}

	maxRounds := r.MaxRounds
	if maxRounds <= 0 {
		maxRounds = DefaultMaxRounds
	}

	planModel := r.PlanModel
	if planModel == "" {
		planModel = r.ExecModel
	}
	execModel := r.ExecModel
	if execModel == "" {
		execModel = planModel
	}

	curHistory := make([]*aicode.Content, len(history))
	copy(curHistory, history)

	totalSummary := &cortex.TurnSummary{}
	nextPrompt := userPrompt
	var latestPlan string

	for round := 1; round <= maxRounds; round++ {
		if r.OnPhaseChange != nil {
			r.OnPhaseChange(PhasePlan, planModel)
		}

		planState := &phaseState{}
		planReg := buildPlannerRegistry(baseReg, planState)
		planGuard := cortex.NewPermissionGuard(r.WorkspaceRoot, cortex.ModePlan, r.AdditionalDirs...)
		planExtra := joinPromptSections(
			r.ExtraPrompt,
			fmt.Sprintf("Planning protocol: inspect the workspace with read-only tools. When ready to execute, call %s with your concrete step-by-step plan. If the user only asked a question that needs no edits or commands, answer directly without calling %s.", exitPlanToolName, exitPlanToolName),
		)

		planEngine := &cortex.Engine{
			Streamer:         r.Streamer,
			Model:            planModel,
			Registry:         planReg,
			Guard:            planGuard,
			SystemPrompt:     cortex.BuildSystemPrompt(r.WorkspaceRoot, cortex.ModePlan, planExtra),
			Trajectory:       r.Trajectory,
			GenerationConfig: r.GenerationConfig,
			MaxSteps:         r.MaxSteps,
			OnEvent:          r.OnEvent,
		}

		var planSummary *cortex.TurnSummary
		var err error
		curHistory, planSummary, err = planEngine.RunTurn(ctx, curHistory, nextPrompt)
		mergeSummary(totalSummary, planSummary)
		if err != nil {
			return &Result{
				History:    curHistory,
				Summary:    totalSummary,
				Plan:       latestPlan,
				Rounds:     round,
				FinalPhase: PhasePlan,
			}, err
		}

		if !planState.exitCalled {
			return &Result{
				History:    curHistory,
				Summary:    totalSummary,
				Plan:       "",
				Rounds:     round,
				FinalPhase: PhasePlan,
			}, nil
		}
		latestPlan = planState.plan

		if r.OnPhaseChange != nil {
			r.OnPhaseChange(PhaseExec, execModel)
		}

		execState := &phaseState{}
		execReg := buildExecRegistry(baseReg, execState)
		execGuard := cortex.NewPermissionGuard(r.WorkspaceRoot, cortex.ModeAgent, r.AdditionalDirs...)
		execExtra := joinPromptSections(
			r.ExtraPrompt,
			fmt.Sprintf("<plan>\n%s\n</plan>\nExecution protocol: execute the approved plan above. If you discover a blocker that requires re-planning, call %s with a brief reason.", latestPlan, enterPlanToolName),
		)

		execEngine := &cortex.Engine{
			Streamer:         r.Streamer,
			Model:            execModel,
			Registry:         execReg,
			Guard:            execGuard,
			SystemPrompt:     cortex.BuildSystemPrompt(r.WorkspaceRoot, cortex.ModeAgent, execExtra),
			Trajectory:       r.Trajectory,
			GenerationConfig: r.GenerationConfig,
			MaxSteps:         r.MaxSteps,
			OnEvent:          r.OnEvent,
		}

		execPrompt := fmt.Sprintf("<plan>\n%s\n</plan>\nExecute the approved plan.", latestPlan)
		var execSummary *cortex.TurnSummary
		curHistory, execSummary, err = execEngine.RunTurn(ctx, curHistory, execPrompt)
		mergeSummary(totalSummary, execSummary)
		if err != nil {
			return &Result{
				History:    curHistory,
				Summary:    totalSummary,
				Plan:       latestPlan,
				Rounds:     round,
				FinalPhase: PhaseExec,
			}, err
		}

		if !execState.enterCalled || round == maxRounds {
			return &Result{
				History:    curHistory,
				Summary:    totalSummary,
				Plan:       latestPlan,
				Rounds:     round,
				FinalPhase: PhaseExec,
			}, nil
		}

		if strings.TrimSpace(execState.reason) != "" {
			nextPrompt = fmt.Sprintf("Re-entering planning mode: %s", execState.reason)
		} else {
			nextPrompt = "Re-entering planning mode to revise the plan."
		}
	}

	return &Result{
		History:    curHistory,
		Summary:    totalSummary,
		Plan:       latestPlan,
		Rounds:     maxRounds,
		FinalPhase: PhaseExec,
	}, nil
}

type phaseState struct {
	exitCalled  bool
	plan        string
	enterCalled bool
	reason      string
}

func buildPlannerRegistry(base *cortex.Registry, state *phaseState) *cortex.Registry {
	reg := copyRegistry(base, true)
	reg.Register(&exitPlanModeTool{state: state})
	return reg
}

func buildExecRegistry(base *cortex.Registry, state *phaseState) *cortex.Registry {
	reg := copyRegistry(base, false)
	reg.Register(&enterPlanModeTool{state: state})
	return reg
}

func copyRegistry(base *cortex.Registry, readOnlyOnly bool) *cortex.Registry {
	reg := cortex.NewRegistry()
	if base == nil {
		return reg
	}
	for _, toolSet := range base.Tools(readOnlyOnly) {
		for _, decl := range toolSet.FunctionDeclarations {
			if t, ok := base.Get(decl.Name); ok {
				reg.Register(t)
			}
		}
	}
	return reg
}

type exitPlanModeTool struct {
	state *phaseState
}

func (t *exitPlanModeTool) Name() string { return exitPlanToolName }
func (t *exitPlanModeTool) Description() string {
	return "Finish planning and hand off the approved plan to the execution phase."
}
func (t *exitPlanModeTool) ReadOnly() bool { return true }

func (t *exitPlanModeTool) Declaration() *aicode.FunctionDeclaration {
	return &aicode.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan": map[string]any{
					"type":        "string",
					"description": "Concrete step-by-step implementation plan to execute.",
				},
			},
			"required": []string{"plan"},
		},
	}
}

func (t *exitPlanModeTool) Execute(_ context.Context, _ cortex.ToolContext, args map[string]any) (cortex.ToolResult, error) {
	plan, _ := args["plan"].(string)
	if strings.TrimSpace(plan) == "" {
		return cortex.ToolResult{}, fmt.Errorf("%s: plan must not be empty", exitPlanToolName)
	}
	t.state.exitCalled = true
	t.state.plan = plan
	return cortex.ToolResult{
		Text: "Plan accepted; transitioning to execution phase.",
		Output: map[string]any{
			"status":        "ok",
			"plan_accepted": true,
		},
	}, nil
}

type enterPlanModeTool struct {
	state *phaseState
}

func (t *enterPlanModeTool) Name() string { return enterPlanToolName }
func (t *enterPlanModeTool) Description() string {
	return "Hand control back to the planning phase when re-planning is required."
}
func (t *enterPlanModeTool) ReadOnly() bool { return false }

func (t *enterPlanModeTool) Declaration() *aicode.FunctionDeclaration {
	return &aicode.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"reason": map[string]any{
					"type":        "string",
					"description": "Why execution needs to return to planning mode.",
				},
			},
		},
	}
}

func (t *enterPlanModeTool) Execute(_ context.Context, _ cortex.ToolContext, args map[string]any) (cortex.ToolResult, error) {
	reason, _ := args["reason"].(string)
	t.state.enterCalled = true
	t.state.reason = reason
	return cortex.ToolResult{
		Text: "Returning to planning phase.",
		Output: map[string]any{
			"status": "ok",
			"reason": reason,
		},
	}, nil
}

func joinPromptSections(parts ...string) string {
	var out []string
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return strings.Join(out, "\n\n")
}

func mergeSummary(acc, next *cortex.TurnSummary) {
	if acc == nil || next == nil {
		return
	}
	acc.Steps += next.Steps
	acc.ToolCalls += next.ToolCalls
	acc.FinishReason = next.FinishReason
	if next.FinalText != "" {
		acc.FinalText = next.FinalText
	}
	if next.ThoughtText != "" {
		if acc.ThoughtText != "" {
			acc.ThoughtText += "\n"
		}
		acc.ThoughtText += next.ThoughtText
	}
	if next.Usage != nil {
		if acc.Usage == nil {
			cp := *next.Usage
			acc.Usage = &cp
		} else {
			acc.Usage.PromptTokenCount += next.Usage.PromptTokenCount
			acc.Usage.CandidatesTokenCount += next.Usage.CandidatesTokenCount
			acc.Usage.TotalTokenCount += next.Usage.TotalTokenCount
			acc.Usage.ThoughtsTokenCount += next.Usage.ThoughtsTokenCount
			acc.Usage.CachedContentTokenCount += next.Usage.CachedContentTokenCount
		}
	}
}
