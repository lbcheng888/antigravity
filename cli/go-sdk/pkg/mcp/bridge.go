package mcp

import (
	"context"
	"errors"
	"fmt"

	"antigravity.dev/agy-sdk/pkg/aicode"
	"antigravity.dev/agy-sdk/pkg/cortex"
)

// RegisterMCPTools discovers tools from client and registers each as mcp__<serverName>__<toolName> in reg.
func RegisterMCPTools(ctx context.Context, reg *cortex.Registry, serverName string, client *Client) (int, error) {
	if reg == nil {
		return 0, errors.New("mcp: registry is nil")
	}
	if client == nil {
		return 0, errors.New("mcp: client is nil")
	}
	if serverName == "" {
		return 0, errors.New("mcp: server name is required")
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		return 0, err
	}

	for _, info := range tools {
		reg.Register(&mcpToolAdapter{
			fullName:   fmt.Sprintf("mcp__%s__%s", serverName, info.Name),
			remoteName: info.Name,
			desc:       info.Description,
			schema:     info.InputSchema,
			client:     client,
		})
	}
	return len(tools), nil
}

type mcpToolAdapter struct {
	fullName   string
	remoteName string
	desc       string
	schema     map[string]any
	client     *Client
}

func (t *mcpToolAdapter) Name() string {
	return t.fullName
}

func (t *mcpToolAdapter) Description() string {
	return t.desc
}

func (t *mcpToolAdapter) Declaration() *aicode.FunctionDeclaration {
	return &aicode.FunctionDeclaration{
		Name:        t.fullName,
		Description: t.desc,
		Parameters:  t.schema,
	}
}

func (t *mcpToolAdapter) ReadOnly() bool {
	return false
}

func (t *mcpToolAdapter) Execute(ctx context.Context, _ cortex.ToolContext, args map[string]any) (cortex.ToolResult, error) {
	text, isErr, err := t.client.CallTool(ctx, t.remoteName, args)
	if err != nil {
		return cortex.ToolResult{}, err
	}
	if isErr {
		if text == "" {
			text = "mcp tool returned error"
		}
		return cortex.ToolResult{Text: text}, fmt.Errorf("mcp tool %s: %s", t.fullName, text)
	}
	return cortex.ToolResult{Text: text}, nil
}
