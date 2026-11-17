package cortex

import (
	"context"
	"sync"

	"antigravity.dev/agy-sdk/pkg/aicode"
)

// ToolContext carries execution environment and security policy for a tool call.
type ToolContext struct {
	WorkspaceRoot string
	Guard         *PermissionGuard
	Mode          Mode
}

// ToolResult represents the structured output of a tool invocation.
type ToolResult struct {
	Output   map[string]any `json:"output,omitempty"`
	Text     string         `json:"text,omitempty"`
	IsDiff   bool           `json:"is_diff,omitempty"`
	Path     string         `json:"path,omitempty"`
	OldText  string         `json:"old_text,omitempty"`
	NewText  string         `json:"new_text,omitempty"`
	ExitCode *int           `json:"exit_code,omitempty"`
}

// toResponseMap converts the ToolResult into a Gemini FunctionResponse payload.
func (r ToolResult) toResponseMap(err error) map[string]any {
	if err != nil {
		resp := map[string]any{
			"error": err.Error(),
		}
		if r.Text != "" {
			resp["output"] = r.Text
		}
		if r.ExitCode != nil {
			resp["exit_code"] = *r.ExitCode
		}
		return resp
	}

	resp := make(map[string]any, len(r.Output)+4)
	for k, v := range r.Output {
		resp[k] = v
	}
	if r.Text != "" {
		if _, exists := resp["output"]; !exists {
			resp["output"] = r.Text
		}
	}
	if r.Path != "" {
		if _, exists := resp["path"]; !exists {
			resp["path"] = r.Path
		}
	}
	if r.IsDiff {
		resp["is_diff"] = true
	}
	if r.ExitCode != nil {
		resp["exit_code"] = *r.ExitCode
	}
	return resp
}

// Tool defines a callable workspace or system capability exposed to the model.
type Tool interface {
	Name() string
	Description() string
	Declaration() *aicode.FunctionDeclaration
	ReadOnly() bool
	Execute(ctx context.Context, tc ToolContext, args map[string]any) (ToolResult, error)
}

// Registry stores tools by name and produces Gemini tool declarations.
type Registry struct {
	mu    sync.RWMutex
	order []string
	tools map[string]Tool
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// NewDefaultRegistry creates a Registry pre-populated with all built-in Cortex tools.
func NewDefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register(&readFileTool{})
	r.Register(&writeFileTool{})
	r.Register(&editFileTool{})
	r.Register(&listDirTool{})
	r.Register(&findByNameTool{})
	r.Register(&grepSearchTool{})
	r.Register(&runCommandTool{})
	return r
}

// Register adds or replaces a tool in the registry.
func (r *Registry) Register(t Tool) {
	if t == nil {
		return
	}
	name := t.Name()
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[name]; !exists {
		r.order = append(r.order, name)
	}
	r.tools[name] = t
}

// Get looks up a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Tools returns Gemini FunctionDeclarations wrapped in aicode.Tool slices.
// When readOnlyOnly is true, mutating tools are omitted.
func (r *Registry) Tools(readOnlyOnly bool) []*aicode.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	decls := make([]*aicode.FunctionDeclaration, 0, len(r.order))
	for _, name := range r.order {
		t := r.tools[name]
		if readOnlyOnly && !t.ReadOnly() {
			continue
		}
		if decl := t.Declaration(); decl != nil {
			decls = append(decls, decl)
		}
	}
	if len(decls) == 0 {
		return nil
	}
	return []*aicode.Tool{{FunctionDeclarations: decls}}
}
