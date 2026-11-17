package swarmrun

import (
	"context"
	"fmt"
	"strings"

	"antigravity.dev/agy-sdk/pkg/aicode"
	"antigravity.dev/agy-sdk/pkg/cortex"
	"antigravity.dev/agy-sdk/pkg/swarm"
)

func registerSwarmTools(reg *cortex.Registry, orch *Orchestrator, agentName string) {
	reg.Register(&swarmTool{orch: orch, agentName: agentName})
	reg.Register(&postMessageTool{orch: orch, agentName: agentName})
	reg.Register(&readBoardTool{orch: orch, agentName: agentName})
	reg.Register(&swarmWaitTool{orch: orch, agentName: agentName})
}

type swarmTool struct {
	orch      *Orchestrator
	agentName string
}

func (t *swarmTool) Name() string        { return "swarm" }
func (t *swarmTool) Description() string { return "Manage swarm workers, status, models, and title." }
func (t *swarmTool) ReadOnly() bool      { return false }

func (t *swarmTool) Declaration() *aicode.FunctionDeclaration {
	return &aicode.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"enum":        []string{"status", "add", "model", "kill", "title"},
					"description": "Swarm management action to perform.",
				},
				"name": map[string]any{
					"type":        "string",
					"description": "Target worker name for add, model, or kill.",
				},
				"brief": map[string]any{
					"type":        "string",
					"description": "Worker assignment brief for add.",
				},
				"model": map[string]any{
					"type":        "string",
					"description": "Model ID for add or model.",
				},
				"title": map[string]any{
					"type":        "string",
					"description": "New swarm title for title.",
				},
			},
			"required": []string{"action"},
		},
	}
}

func (t *swarmTool) Execute(ctx context.Context, _ cortex.ToolContext, args map[string]any) (cortex.ToolResult, error) {
	action, _ := args["action"].(string)
	name, _ := args["name"].(string)
	brief, _ := args["brief"].(string)
	model, _ := args["model"].(string)
	title, _ := args["title"].(string)

	switch action {
	case "status":
		return t.orch.toolStatus()
	case "add":
		return t.orch.toolAdd(t.agentName, name, brief, model)
	case "model":
		target := name
		if target == "" {
			target = t.agentName
		}
		return t.orch.toolModel(target, model)
	case "kill":
		return t.orch.toolKill(ctx, t.agentName, name)
	case "title":
		return t.orch.toolTitle(t.agentName, title)
	default:
		return cortex.ToolResult{}, fmt.Errorf("unsupported swarm action %q", action)
	}
}

type postMessageTool struct {
	orch      *Orchestrator
	agentName string
}

func (t *postMessageTool) Name() string        { return "post_message" }
func (t *postMessageTool) Description() string { return "Post a message to the shared swarm board." }
func (t *postMessageTool) ReadOnly() bool      { return false }

func (t *postMessageTool) Declaration() *aicode.FunctionDeclaration {
	return &aicode.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{
					"type":        "string",
					"description": "Message text to post to the board.",
				},
				"channel": map[string]any{
					"type":        "string",
					"description": "Optional channel name.",
				},
			},
			"required": []string{"text"},
		},
	}
}

func (t *postMessageTool) Execute(_ context.Context, _ cortex.ToolContext, args map[string]any) (cortex.ToolResult, error) {
	text, _ := args["text"].(string)
	if strings.TrimSpace(text) == "" {
		return cortex.ToolResult{}, fmt.Errorf("text must not be empty")
	}
	channel, _ := args["channel"].(string)

	msg, err := t.orch.store.Post(swarm.PostRequest{
		Author:  t.agentName,
		Text:    text,
		Channel: channel,
	})
	if err != nil {
		return cortex.ToolResult{}, err
	}
	return cortex.ToolResult{
		Output: map[string]any{
			"seq":   msg.Seq,
			"agent": msg.Agent,
			"time":  msg.Time,
		},
	}, nil
}

type readBoardTool struct {
	orch      *Orchestrator
	agentName string
}

func (t *readBoardTool) Name() string        { return "read_board" }
func (t *readBoardTool) Description() string { return "Read messages from the swarm message board." }
func (t *readBoardTool) ReadOnly() bool      { return true }

func (t *readBoardTool) Declaration() *aicode.FunctionDeclaration {
	return &aicode.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"since": map[string]any{
					"type":        "integer",
					"description": "Return messages after this sequence number.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of messages to return (default 20).",
				},
				"seqs": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "integer"},
					"description": "Return only these specific sequence numbers.",
				},
			},
		},
	}
}

func (t *readBoardTool) Execute(_ context.Context, _ cortex.ToolContext, args map[string]any) (cortex.ToolResult, error) {
	limit := 20
	if v, ok := intArg(args, "limit"); ok && v > 0 {
		limit = v
	}

	var msgs []swarm.Message
	if rawSeqs, ok := args["seqs"].([]any); ok && len(rawSeqs) > 0 {
		seqs := make([]uint64, 0, len(rawSeqs))
		for _, item := range rawSeqs {
			if n, ok := toUint64(item); ok {
				seqs = append(seqs, n)
			}
		}
		msgs = t.orch.store.Get(seqs)
	} else if since, ok := intArg(args, "since"); ok && since >= 0 {
		msgs = t.orch.store.ReadSince(uint64(since), limit)
	} else {
		msgs = t.orch.store.Tail(limit)
	}

	if len(msgs) > 0 {
		lastSeq := msgs[len(msgs)-1].Seq
		_ = t.orch.store.UpdateMemberLocked(t.agentName, func(st *swarm.MemberState) {
			st.CatchUp(lastSeq)
		})
	}

	return cortex.ToolResult{
		Output: map[string]any{
			"head_seq": t.orch.store.Head(),
			"messages": msgs,
		},
	}, nil
}

type swarmWaitTool struct {
	orch      *Orchestrator
	agentName string
}

func (t *swarmWaitTool) Name() string        { return "swarm_wait" }
func (t *swarmWaitTool) Description() string { return "Wait until a relevant message is posted on the board." }
func (t *swarmWaitTool) ReadOnly() bool      { return true }

func (t *swarmWaitTool) Declaration() *aicode.FunctionDeclaration {
	return &aicode.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"since": map[string]any{
					"type":        "integer",
					"description": "Wait for messages after this sequence number.",
				},
				"mentions_only": map[string]any{
					"type":        "boolean",
					"description": "Wake only on @mentions or @all (default true).",
				},
			},
		},
	}
}

func (t *swarmWaitTool) Execute(ctx context.Context, _ cortex.ToolContext, args map[string]any) (cortex.ToolResult, error) {
	mentionsOnly := true
	if v, ok := args["mentions_only"].(bool); ok {
		mentionsOnly = v
	}

	var since uint64
	if v, ok := intArg(args, "since"); ok && v >= 0 {
		since = uint64(v)
	} else if snap := t.orch.store.RosterSnapshot(); snap != nil {
		if m := snap.Members[t.agentName]; m != nil {
			since = m.LastReadSeq
		}
	}

	msgs, err := t.orch.waitForMessages(ctx, t.agentName, since, mentionsOnly)
	if err != nil {
		return cortex.ToolResult{}, err
	}
	return cortex.ToolResult{
		Output: map[string]any{
			"head_seq": t.orch.store.Head(),
			"messages": msgs,
		},
	}, nil
}

func intArg(args map[string]any, key string) (int, bool) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return 0, false
	}
	switch v := raw.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case uint64:
		return int(v), true
	case float64:
		return int(v), true
	default:
		return 0, false
	}
}

func toUint64(raw any) (uint64, bool) {
	switch v := raw.(type) {
	case int:
		if v >= 0 {
			return uint64(v), true
		}
	case int64:
		if v >= 0 {
			return uint64(v), true
		}
	case uint64:
		return v, true
	case float64:
		if v >= 0 {
			return uint64(v), true
		}
	}
	return 0, false
}
