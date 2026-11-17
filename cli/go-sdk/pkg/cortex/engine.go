package cortex

import (
	"context"
	"fmt"
	"strings"

	"antigravity.dev/agy-sdk/pkg/aicode"
)

const DefaultMaxSteps = 25

// Streamer streams a GeminiRequest and delivers SSE chunks to onChunk.
type Streamer interface {
	Stream(ctx context.Context, model string, req *aicode.GeminiRequest, onChunk func(*aicode.SSEChunk) error) error
}

// ClientStreamer adapts *aicode.Client to the Streamer interface.
type ClientStreamer struct {
	Client *aicode.Client
}

// NewClientStreamer wraps an *aicode.Client as a Streamer.
func NewClientStreamer(client *aicode.Client) Streamer {
	return &ClientStreamer{Client: client}
}

// Stream delegates to Client.StreamGenerateContent.
func (s *ClientStreamer) Stream(ctx context.Context, model string, req *aicode.GeminiRequest, onChunk func(*aicode.SSEChunk) error) error {
	if s == nil || s.Client == nil {
		return fmt.Errorf("aicode client is nil")
	}
	return s.Client.StreamGenerateContent(ctx, model, req, onChunk)
}

// EventType identifies lifecycle events emitted during Engine.RunTurn.
type EventType string

const (
	EventThought   EventType = "thought"
	EventText      EventType = "text"
	EventToolStart EventType = "tool_start"
	EventToolDone  EventType = "tool_done"
	EventUsage     EventType = "usage"
)

// Event carries a single streaming or tool-execution notification.
type Event struct {
	Type       EventType
	Text       string
	ToolCall   *aicode.FunctionCall
	ToolResult *ToolResult
	Err        error
	Usage      *aicode.UsageMetadata
}

// EventCallback receives real-time events from Engine.RunTurn.
type EventCallback func(Event)

// TurnSummary aggregates metrics and final text produced by Engine.RunTurn.
type TurnSummary struct {
	Steps        int
	FinalText    string
	ThoughtText  string
	ToolCalls    int
	Usage        *aicode.UsageMetadata
	FinishReason string
}

// Engine orchestrates the multi-turn ReAct function-calling loop.
type Engine struct {
	Streamer         Streamer
	Model            string
	Registry         *Registry
	Guard            *PermissionGuard
	SystemPrompt     *aicode.Content
	Trajectory       *TrajectoryRecorder
	GenerationConfig *aicode.GenerationConfig
	MaxSteps         int
	OnEvent          EventCallback
}

// RunTurn executes a user prompt against history, running tool calls until the model produces
// a final answer or MaxSteps is reached.
func (e *Engine) RunTurn(ctx context.Context, history []*aicode.Content, userPrompt string) ([]*aicode.Content, *TurnSummary, error) {
	if e == nil || e.Streamer == nil {
		return history, nil, fmt.Errorf("engine streamer is nil")
	}

	reg := e.Registry
	if reg == nil {
		reg = NewDefaultRegistry()
	}

	maxSteps := e.MaxSteps
	if maxSteps <= 0 {
		maxSteps = DefaultMaxSteps
	}

	contents := make([]*aicode.Content, len(history))
	copy(contents, history)

	if userPrompt != "" {
		contents = append(contents, &aicode.Content{
			Role:  "user",
			Parts: []*aicode.Part{{Text: userPrompt}},
		})
		if e.Trajectory != nil {
			if err := e.Trajectory.RecordUserInput(userPrompt); err != nil {
				return contents, nil, err
			}
		}
	}

	summary := &TurnSummary{}
	readOnlyOnly := e.Guard != nil && e.Guard.IsReadOnlyMode()
	tools := reg.Tools(readOnlyOnly)

	for step := 1; step <= maxSteps; step++ {
		summary.Steps = step

		req := &aicode.GeminiRequest{
			Contents:          contents,
			SystemInstruction: e.SystemPrompt,
			Tools:             tools,
			GenerationConfig:  e.GenerationConfig,
		}

		var modelParts []*aicode.Part
		var stepText strings.Builder
		var stepThought strings.Builder
		var functionCalls []*aicode.FunctionCall
		var stepFinishReason string

		err := e.Streamer.Stream(ctx, e.Model, req, func(ch *aicode.SSEChunk) error {
			if ch == nil || ch.Response == nil {
				return nil
			}
			if ch.Response.UsageMetadata != nil {
				summary.Usage = mergeUsage(summary.Usage, ch.Response.UsageMetadata)
				e.emit(Event{Type: EventUsage, Usage: ch.Response.UsageMetadata})
			}
			for _, cand := range ch.Response.Candidates {
				if cand.FinishReason != "" {
					stepFinishReason = cand.FinishReason
				}
				if cand.Content == nil {
					continue
				}
				for _, part := range cand.Content.Parts {
					if part == nil {
						continue
					}
					modelParts = appendModelPart(modelParts, part)
					switch {
					case part.FunctionCall != nil:
						functionCalls = append(functionCalls, part.FunctionCall)
					case part.Thought:
						if part.Text != "" {
							stepThought.WriteString(part.Text)
							e.emit(Event{Type: EventThought, Text: part.Text})
						}
					case part.Text != "":
						stepText.WriteString(part.Text)
						e.emit(Event{Type: EventText, Text: part.Text})
					}
				}
			}
			return nil
		})
		if err != nil {
			return contents, summary, err
		}

		summary.FinishReason = stepFinishReason
		if stepThought.Len() > 0 {
			if summary.ThoughtText != "" {
				summary.ThoughtText += "\n"
			}
			summary.ThoughtText += stepThought.String()
		}
		summary.FinalText = stepText.String()

		if len(modelParts) > 0 {
			contents = append(contents, &aicode.Content{
				Role:  "model",
				Parts: modelParts,
			})
		}

		if e.Trajectory != nil {
			if err := e.Trajectory.RecordPlannerResponse(stepThought.String(), stepText.String(), functionCalls); err != nil {
				return contents, summary, err
			}
		}

		if len(functionCalls) == 0 {
			return contents, summary, nil
		}

		tc := ToolContext{Guard: e.Guard}
		if e.Guard != nil {
			tc.WorkspaceRoot = e.Guard.WorkspaceRoot
			tc.Mode = e.Guard.Mode
		}

		respParts := make([]*aicode.Part, 0, len(functionCalls))
		for _, fc := range functionCalls {
			summary.ToolCalls++
			e.emit(Event{Type: EventToolStart, ToolCall: fc})

			result, execErr := e.executeToolCall(ctx, reg, tc, fc)
			resCopy := result
			e.emit(Event{
				Type:       EventToolDone,
				ToolCall:   fc,
				ToolResult: &resCopy,
				Err:        execErr,
			})

			if e.Trajectory != nil {
				if err := e.Trajectory.RecordToolResult(fc.Name, result, execErr); err != nil {
					return contents, summary, err
				}
			}

			respParts = append(respParts, &aicode.Part{
				FunctionResponse: &aicode.FunctionResponse{
					ID:       fc.ID,
					Name:     fc.Name,
					Response: result.toResponseMap(execErr),
				},
			})
		}

		contents = append(contents, &aicode.Content{
			Role:  "user",
			Parts: respParts,
		})
	}

	return contents, summary, fmt.Errorf("exceeded max steps (%d)", maxSteps)
}

func (e *Engine) executeToolCall(ctx context.Context, reg *Registry, tc ToolContext, fc *aicode.FunctionCall) (ToolResult, error) {
	if fc == nil {
		return ToolResult{}, fmt.Errorf("nil function call")
	}
	tool, ok := reg.Get(fc.Name)
	if !ok {
		return ToolResult{}, fmt.Errorf("unknown tool %q", fc.Name)
	}
	if e.Guard != nil {
		if err := e.Guard.CheckTool(tool); err != nil {
			return ToolResult{}, err
		}
	}
	return tool.Execute(ctx, tc, fc.Args)
}

func (e *Engine) emit(ev Event) {
	if e != nil && e.OnEvent != nil {
		e.OnEvent(ev)
	}
}

func appendModelPart(parts []*aicode.Part, incoming *aicode.Part) []*aicode.Part {
	if incoming.FunctionCall != nil {
		return append(parts, &aicode.Part{
			ThoughtSignature: incoming.ThoughtSignature,
			FunctionCall:     incoming.FunctionCall,
		})
	}
	if len(parts) > 0 {
		last := parts[len(parts)-1]
		if last.FunctionCall == nil && last.Thought == incoming.Thought {
			if incoming.ThoughtSignature == "" || last.ThoughtSignature == "" {
				last.Text += incoming.Text
				if last.ThoughtSignature == "" {
					last.ThoughtSignature = incoming.ThoughtSignature
				}
				return parts
			}
		}
	}
	return append(parts, &aicode.Part{
		Text:             incoming.Text,
		Thought:          incoming.Thought,
		ThoughtSignature: incoming.ThoughtSignature,
		InlineData:       incoming.InlineData,
	})
}

func mergeUsage(acc, next *aicode.UsageMetadata) *aicode.UsageMetadata {
	if next == nil {
		return acc
	}
	if acc == nil {
		cp := *next
		return &cp
	}
	return &aicode.UsageMetadata{
		PromptTokenCount:        next.PromptTokenCount,
		CandidatesTokenCount:    next.CandidatesTokenCount,
		TotalTokenCount:         next.TotalTokenCount,
		ThoughtsTokenCount:      next.ThoughtsTokenCount,
		CachedContentTokenCount: next.CachedContentTokenCount,
	}
}
