// Package sandbox provides macOS Seatbelt command sandboxing and an SSE-capable HTTP trace proxy.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"antigravity.dev/agy-sdk/pkg/aicode"
	"antigravity.dev/agy-sdk/pkg/cortex"
)

const (
	seatbeltBinary    = "/usr/bin/sandbox-exec"
	defaultCmdTimeout = 30 * time.Second
)

// BuildSeatbeltProfile generates a macOS (version 1) SBPL profile that denies file-write*
// outside /dev/null, /tmp, /private/tmp, workspaceRoot, and extraWriteDirs (with symlinks resolved).
func BuildSeatbeltProfile(workspaceRoot string, extraWriteDirs []string, allowNetwork bool) string {
	var b strings.Builder
	b.WriteString("(version 1)\n")
	b.WriteString("(allow default)\n")
	if !allowNetwork {
		b.WriteString("(deny network*)\n")
	}
	b.WriteString("(deny file-write*)\n")
	b.WriteString("(allow file-write*\n")
	b.WriteString("  (literal \"/dev/null\")\n")
	b.WriteString("  (subpath \"/tmp\")\n")
	b.WriteString("  (subpath \"/private/tmp\")\n")

	seen := map[string]struct{}{
		"/tmp":         {},
		"/private/tmp": {},
	}
	appendSubpath := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		resolved := resolvePathForSBPL(raw)
		if resolved == "" {
			return
		}
		if _, exists := seen[resolved]; exists {
			return
		}
		seen[resolved] = struct{}{}
		fmt.Fprintf(&b, "  (subpath %s)\n", strconv.Quote(resolved))
	}

	appendSubpath(workspaceRoot)
	for _, dir := range extraWriteDirs {
		appendSubpath(dir)
	}
	b.WriteString(")\n")
	return b.String()
}

// NewSandboxedRunCommandTool returns a cortex.Tool named "run_command" that executes shell commands
// inside /usr/bin/sandbox-exec on macOS when available, falling back to /bin/sh -c otherwise.
func NewSandboxedRunCommandTool(extraWriteDirs []string, allowNetwork bool) cortex.Tool {
	dirs := append([]string(nil), extraWriteDirs...)
	return &sandboxedRunCommandTool{
		extraWriteDirs: dirs,
		allowNetwork:   allowNetwork,
	}
}

type sandboxedRunCommandTool struct {
	extraWriteDirs []string
	allowNetwork   bool
}

func (t *sandboxedRunCommandTool) Name() string { return "run_command" }
func (t *sandboxedRunCommandTool) Description() string {
	return "Execute a shell command in the workspace via /bin/sh -c inside a filesystem sandbox."
}
func (t *sandboxedRunCommandTool) ReadOnly() bool { return false }

func (t *sandboxedRunCommandTool) Declaration() *aicode.FunctionDeclaration {
	return &aicode.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"CommandLine": map[string]any{"type": "string", "description": "Shell command line to execute."},
				"Cwd":         map[string]any{"type": "string", "description": "Working directory (defaults to workspace root)."},
				"TimeoutMs":   map[string]any{"type": "integer", "description": "Optional timeout in milliseconds (default 30000)."},
			},
			"required": []string{"CommandLine"},
		},
	}
}

func (t *sandboxedRunCommandTool) Execute(ctx context.Context, tc cortex.ToolContext, args map[string]any) (cortex.ToolResult, error) {
	cmdLine, _ := args["CommandLine"].(string)
	if strings.TrimSpace(cmdLine) == "" {
		return cortex.ToolResult{}, fmt.Errorf("run_command: CommandLine is required")
	}

	rawCwd, _ := args["Cwd"].(string)
	cwd, err := resolveCommandCwd(tc, rawCwd)
	if err != nil {
		return cortex.ToolResult{}, err
	}

	timeout := defaultCmdTimeout
	if ms, ok := argInt(args, "TimeoutMs"); ok && ms > 0 {
		timeout = time.Duration(ms) * time.Millisecond
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	workspaceRoot := tc.WorkspaceRoot
	if workspaceRoot == "" {
		workspaceRoot = cwd
	}

	var cmd *exec.Cmd
	sandboxed := false
	if hasSeatbelt() {
		profile := BuildSeatbeltProfile(workspaceRoot, t.extraWriteDirs, t.allowNetwork)
		cmd = exec.CommandContext(cmdCtx, seatbeltBinary, "-p", profile, "/bin/sh", "-c", cmdLine)
		sandboxed = true
	} else {
		cmd = exec.CommandContext(cmdCtx, "/bin/sh", "-c", cmdLine)
	}
	if cwd != "" {
		cmd.Dir = cwd
	}

	out, runErr := cmd.CombinedOutput()
	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	text := string(out)
	if errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
		text += fmt.Sprintf("\n[command timed out after %s]", timeout)
	}

	return cortex.ToolResult{
		Text:     text,
		ExitCode: &exitCode,
		Output: map[string]any{
			"command":   cmdLine,
			"cwd":       cwd,
			"output":    text,
			"exit_code": exitCode,
			"sandboxed": sandboxed,
		},
	}, nil
}

func hasSeatbelt() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	info, err := os.Stat(seatbeltBinary)
	return err == nil && !info.IsDir()
}

func resolveCommandCwd(tc cortex.ToolContext, rawCwd string) (string, error) {
	if tc.Guard != nil {
		return tc.Guard.CheckCommandCwd(rawCwd)
	}
	if strings.TrimSpace(rawCwd) != "" {
		if filepath.IsAbs(rawCwd) {
			return filepath.Clean(rawCwd), nil
		}
		if tc.WorkspaceRoot != "" {
			return filepath.Clean(filepath.Join(tc.WorkspaceRoot, rawCwd)), nil
		}
		return filepath.Abs(rawCwd)
	}
	if tc.WorkspaceRoot != "" {
		return filepath.Clean(tc.WorkspaceRoot), nil
	}
	return os.Getwd()
}

func resolvePathForSBPL(raw string) string {
	abs, err := filepath.Abs(raw)
	if err != nil {
		return filepath.Clean(raw)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	var missing []string
	cur := abs
	for {
		parent := filepath.Dir(cur)
		missing = append(missing, filepath.Base(cur))
		if parent == cur {
			return abs
		}
		if resolvedParent, err := filepath.EvalSymlinks(parent); err == nil {
			parts := make([]string, 0, len(missing)+1)
			parts = append(parts, resolvedParent)
			for i := len(missing) - 1; i >= 0; i-- {
				parts = append(parts, missing[i])
			}
			return filepath.Join(parts...)
		}
		cur = parent
	}
}

func argInt(args map[string]any, key string) (int, bool) {
	v, ok := args[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}
