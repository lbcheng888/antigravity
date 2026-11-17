package cortex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"antigravity.dev/agy-sdk/pkg/aicode"
)

const (
	readFileMaxLines = 800
	readFileMaxBytes = 46080
	grepMaxMatches   = 50
	defaultCmdTimeout = 30 * time.Second
)

func resolveToolPath(tc ToolContext, rawPath string) (string, error) {
	if tc.Guard != nil {
		return tc.Guard.ResolvePath(rawPath)
	}
	if strings.TrimSpace(rawPath) == "" {
		return "", fmt.Errorf("path must not be empty")
	}
	if !filepath.IsAbs(rawPath) && tc.WorkspaceRoot != "" {
		return filepath.Abs(filepath.Join(tc.WorkspaceRoot, rawPath))
	}
	return filepath.Abs(rawPath)
}

func argString(args map[string]any, key string) (string, bool) {
	if args == nil {
		return "", false
	}
	v, ok := args[key]
	if !ok || v == nil {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func argBool(args map[string]any, key string) (bool, bool) {
	if args == nil {
		return false, false
	}
	v, ok := args[key]
	if !ok || v == nil {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

func argInt(args map[string]any, key string) (int, bool) {
	if args == nil {
		return 0, false
	}
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
	case json.Number:
		i, err := n.Int64()
		if err == nil {
			return int(i), true
		}
	case string:
		i, err := strconv.Atoi(n)
		if err == nil {
			return i, true
		}
	}
	return 0, false
}

// --- read_file ---

type readFileTool struct{}

func (t *readFileTool) Name() string { return "read_file" }
func (t *readFileTool) Description() string {
	return "Read a file's contents with 1-based line numbers, capped at 800 lines and 46080 bytes."
}
func (t *readFileTool) ReadOnly() bool { return true }

func (t *readFileTool) Declaration() *aicode.FunctionDeclaration {
	return &aicode.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"Path":      map[string]any{"type": "string", "description": "Path to the file to read."},
				"StartLine": map[string]any{"type": "integer", "description": "Optional 1-based start line."},
				"EndLine":   map[string]any{"type": "integer", "description": "Optional 1-based inclusive end line."},
			},
			"required": []string{"Path"},
		},
	}
}

func (t *readFileTool) Execute(_ context.Context, tc ToolContext, args map[string]any) (ToolResult, error) {
	rawPath, ok := argString(args, "Path")
	if !ok || strings.TrimSpace(rawPath) == "" {
		return ToolResult{}, fmt.Errorf("read_file: Path is required")
	}
	resolved, err := resolveToolPath(tc, rawPath)
	if err != nil {
		return ToolResult{}, err
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return ToolResult{}, fmt.Errorf("read_file: %w", err)
	}

	content := string(data)
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	totalLines := len(lines)

	startLine, hasStart := argInt(args, "StartLine")
	if !hasStart || startLine < 1 {
		startLine = 1
	}
	endLine, hasEnd := argInt(args, "EndLine")
	if !hasEnd || endLine < 1 || endLine > totalLines {
		endLine = totalLines
	}

	var b strings.Builder
	truncated := false
	linesWritten := 0

	if totalLines > 0 && startLine <= totalLines && startLine <= endLine {
		for lineNum := startLine; lineNum <= endLine; lineNum++ {
			if linesWritten >= readFileMaxLines {
				truncated = true
				break
			}
			formatted := fmt.Sprintf("%d: %s\n", lineNum, lines[lineNum-1])
			if b.Len()+len(formatted) > readFileMaxBytes {
				if b.Len() == 0 {
					maxContent := max(0, readFileMaxBytes-16)
					formatted = fmt.Sprintf("%d: %s\n", lineNum, lines[lineNum-1][:min(len(lines[lineNum-1]), maxContent)])
					b.WriteString(formatted)
					linesWritten++
				}
				truncated = true
				break
			}
			b.WriteString(formatted)
			linesWritten++
		}
	}

	text := b.String()
	if truncated {
		text += fmt.Sprintf("... [truncated after %d lines / %d bytes]\n", linesWritten, b.Len())
	}

	return ToolResult{
		Path: resolved,
		Text: text,
		Output: map[string]any{
			"path":        resolved,
			"content":     text,
			"start_line":  startLine,
			"end_line":    startLine + max(0, linesWritten-1),
			"total_lines": totalLines,
			"truncated":   truncated,
		},
	}, nil
}

// --- write_file ---

type writeFileTool struct{}

func (t *writeFileTool) Name() string { return "write_file" }
func (t *writeFileTool) Description() string {
	return "Create, overwrite, or append to a file, creating parent directories as needed."
}
func (t *writeFileTool) ReadOnly() bool { return false }

func (t *writeFileTool) Declaration() *aicode.FunctionDeclaration {
	return &aicode.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"Path":      map[string]any{"type": "string", "description": "Path to the file to write."},
				"Content":   map[string]any{"type": "string", "description": "File content to write."},
				"Overwrite": map[string]any{"type": "boolean", "description": "Allow overwriting an existing file."},
				"Append":    map[string]any{"type": "boolean", "description": "Append Content to the end of the file."},
			},
			"required": []string{"Path", "Content"},
		},
	}
}

func (t *writeFileTool) Execute(_ context.Context, tc ToolContext, args map[string]any) (ToolResult, error) {
	rawPath, ok := argString(args, "Path")
	if !ok || strings.TrimSpace(rawPath) == "" {
		return ToolResult{}, fmt.Errorf("write_file: Path is required")
	}
	content, ok := argString(args, "Content")
	if !ok {
		return ToolResult{}, fmt.Errorf("write_file: Content is required")
	}
	overwrite, _ := argBool(args, "Overwrite")
	appendMode, _ := argBool(args, "Append")
	if overwrite && appendMode {
		return ToolResult{}, fmt.Errorf("write_file: Overwrite and Append cannot both be true")
	}

	resolved, err := resolveToolPath(tc, rawPath)
	if err != nil {
		return ToolResult{}, err
	}

	var oldText string
	existed := false
	if existing, err := os.ReadFile(resolved); err == nil {
		existed = true
		oldText = string(existing)
		if !overwrite && !appendMode {
			return ToolResult{}, fmt.Errorf("write_file: %q already exists (set Overwrite or Append)", resolved)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return ToolResult{}, fmt.Errorf("write_file: stat %q: %w", resolved, err)
	}

	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return ToolResult{}, fmt.Errorf("write_file: mkdir: %w", err)
	}

	newText := content
	if appendMode && existed {
		newText = oldText + content
	}

	if err := os.WriteFile(resolved, []byte(newText), 0o644); err != nil {
		return ToolResult{}, fmt.Errorf("write_file: %w", err)
	}

	return ToolResult{
		IsDiff:  true,
		Path:    resolved,
		OldText: oldText,
		NewText: newText,
		Text:    fmt.Sprintf("Wrote %d bytes to %s", len(content), resolved),
		Output: map[string]any{
			"path":          resolved,
			"bytes_written": len(content),
			"created":       !existed,
		},
	}, nil
}

// --- edit_file ---

type editFileTool struct{}

func (t *editFileTool) Name() string { return "edit_file" }
func (t *editFileTool) Description() string {
	return "Replace exact occurrences of Target with Replacement in an existing file."
}
func (t *editFileTool) ReadOnly() bool { return false }

func (t *editFileTool) Declaration() *aicode.FunctionDeclaration {
	return &aicode.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"Path":        map[string]any{"type": "string", "description": "Path to the file to edit."},
				"Target":      map[string]any{"type": "string", "description": "Exact text to replace."},
				"Replacement": map[string]any{"type": "string", "description": "Replacement text."},
			},
			"required": []string{"Path", "Target", "Replacement"},
		},
	}
}

func (t *editFileTool) Execute(_ context.Context, tc ToolContext, args map[string]any) (ToolResult, error) {
	rawPath, ok := argString(args, "Path")
	if !ok || strings.TrimSpace(rawPath) == "" {
		return ToolResult{}, fmt.Errorf("edit_file: Path is required")
	}
	target, ok := argString(args, "Target")
	if !ok || target == "" {
		return ToolResult{}, fmt.Errorf("edit_file: Target must be non-empty")
	}
	replacement, ok := argString(args, "Replacement")
	if !ok {
		return ToolResult{}, fmt.Errorf("edit_file: Replacement is required")
	}

	resolved, err := resolveToolPath(tc, rawPath)
	if err != nil {
		return ToolResult{}, err
	}

	raw, err := os.ReadFile(resolved)
	if err != nil {
		return ToolResult{}, fmt.Errorf("edit_file: %w", err)
	}

	oldText := string(raw)
	count := strings.Count(oldText, target)
	if count == 0 {
		return ToolResult{}, fmt.Errorf("edit_file: Target not found in %s", resolved)
	}

	newText := strings.ReplaceAll(oldText, target, replacement)
	if err := os.WriteFile(resolved, []byte(newText), 0o644); err != nil {
		return ToolResult{}, fmt.Errorf("edit_file: %w", err)
	}

	return ToolResult{
		IsDiff:  true,
		Path:    resolved,
		OldText: oldText,
		NewText: newText,
		Text:    fmt.Sprintf("Replaced %d occurrence(s) in %s", count, resolved),
		Output: map[string]any{
			"path":         resolved,
			"replacements": count,
		},
	}, nil
}

// --- list_dir ---

type listDirTool struct{}

func (t *listDirTool) Name() string { return "list_dir" }
func (t *listDirTool) Description() string {
	return "List entries in a directory sorted by name with entry type and byte size."
}
func (t *listDirTool) ReadOnly() bool { return true }

func (t *listDirTool) Declaration() *aicode.FunctionDeclaration {
	return &aicode.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"Path": map[string]any{"type": "string", "description": "Directory path (defaults to workspace root)."},
			},
		},
	}
}

func (t *listDirTool) Execute(_ context.Context, tc ToolContext, args map[string]any) (ToolResult, error) {
	rawPath, _ := argString(args, "Path")
	if strings.TrimSpace(rawPath) == "" {
		rawPath = "."
	}
	resolved, err := resolveToolPath(tc, rawPath)
	if err != nil {
		return ToolResult{}, err
	}

	entries, err := os.ReadDir(resolved)
	if err != nil {
		return ToolResult{}, fmt.Errorf("list_dir: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	type dirEntryInfo struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Size int64  `json:"size"`
	}

	var b strings.Builder
	items := make([]dirEntryInfo, 0, len(entries))
	for _, entry := range entries {
		entryType := "file"
		if entry.IsDir() {
			entryType = "dir"
		} else if entry.Type()&os.ModeSymlink != 0 {
			entryType = "symlink"
		}
		var size int64
		if info, err := entry.Info(); err == nil && !entry.IsDir() {
			size = info.Size()
		}
		items = append(items, dirEntryInfo{
			Name: entry.Name(),
			Type: entryType,
			Size: size,
		})
		fmt.Fprintf(&b, "%s\t%s\t%d\n", entryType, entry.Name(), size)
	}

	return ToolResult{
		Path: resolved,
		Text: b.String(),
		Output: map[string]any{
			"path":    resolved,
			"entries": items,
		},
	}, nil
}

// --- find_by_name ---

type findByNameTool struct{}

func (t *findByNameTool) Name() string { return "find_by_name" }
func (t *findByNameTool) Description() string {
	return "Find files and directories matching an optional glob pattern, skipping .git and node_modules."
}
func (t *findByNameTool) ReadOnly() bool { return true }

func (t *findByNameTool) Declaration() *aicode.FunctionDeclaration {
	return &aicode.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"SearchDirectory": map[string]any{"type": "string", "description": "Root directory to search (defaults to workspace root)."},
				"Pattern":         map[string]any{"type": "string", "description": "Optional filename glob pattern (e.g. *.go)."},
				"MaxDepth":        map[string]any{"type": "integer", "description": "Optional maximum directory depth."},
			},
		},
	}
}

func (t *findByNameTool) Execute(_ context.Context, tc ToolContext, args map[string]any) (ToolResult, error) {
	rawDir, _ := argString(args, "SearchDirectory")
	if strings.TrimSpace(rawDir) == "" {
		rawDir = "."
	}
	pattern, _ := argString(args, "Pattern")
	maxDepth, _ := argInt(args, "MaxDepth")

	resolved, err := resolveToolPath(tc, rawDir)
	if err != nil {
		return ToolResult{}, err
	}

	var matches []string
	err = filepath.WalkDir(resolved, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if path == resolved {
			return nil
		}
		name := d.Name()
		if d.IsDir() && (name == ".git" || name == "node_modules") {
			return filepath.SkipDir
		}

		rel, err := filepath.Rel(resolved, path)
		if err != nil {
			return nil
		}
		depth := strings.Count(rel, string(filepath.Separator)) + 1
		if maxDepth > 0 && depth > maxDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if pattern == "" {
			matches = append(matches, rel)
		} else if matched, err := filepath.Match(pattern, name); err == nil && matched {
			matches = append(matches, rel)
		}

		if maxDepth > 0 && depth == maxDepth && d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return ToolResult{}, fmt.Errorf("find_by_name: %w", err)
	}

	sort.Strings(matches)
	return ToolResult{
		Path: resolved,
		Text: strings.Join(matches, "\n"),
		Output: map[string]any{
			"path":    resolved,
			"matches": matches,
			"count":   len(matches),
		},
	}, nil
}

// --- grep_search ---

type grepSearchTool struct{}

func (t *grepSearchTool) Name() string { return "grep_search" }
func (t *grepSearchTool) Description() string {
	return "Search files for lines matching a literal query or regular expression (capped at 50 matches)."
}
func (t *grepSearchTool) ReadOnly() bool { return true }

func (t *grepSearchTool) Declaration() *aicode.FunctionDeclaration {
	return &aicode.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"SearchPath":      map[string]any{"type": "string", "description": "File or directory to search (defaults to workspace root)."},
				"Query":           map[string]any{"type": "string", "description": "Search text or regular expression."},
				"IsRegex":         map[string]any{"type": "boolean", "description": "Interpret Query as a regular expression."},
				"CaseInsensitive": map[string]any{"type": "boolean", "description": "Perform case-insensitive matching."},
			},
			"required": []string{"Query"},
		},
	}
}

type grepMatch struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Content string `json:"content"`
}

func (t *grepSearchTool) Execute(_ context.Context, tc ToolContext, args map[string]any) (ToolResult, error) {
	query, ok := argString(args, "Query")
	if !ok || query == "" {
		return ToolResult{}, fmt.Errorf("grep_search: Query is required")
	}
	rawPath, _ := argString(args, "SearchPath")
	if strings.TrimSpace(rawPath) == "" {
		rawPath = "."
	}
	isRegex, _ := argBool(args, "IsRegex")
	caseInsensitive, _ := argBool(args, "CaseInsensitive")

	resolved, err := resolveToolPath(tc, rawPath)
	if err != nil {
		return ToolResult{}, err
	}

	var re *regexp.Regexp
	if isRegex {
		pat := query
		if caseInsensitive {
			pat = "(?i)" + pat
		}
		re, err = regexp.Compile(pat)
		if err != nil {
			return ToolResult{}, fmt.Errorf("grep_search: invalid regex: %w", err)
		}
	}

	lowerQuery := strings.ToLower(query)
	matchLine := func(line string) bool {
		if re != nil {
			return re.MatchString(line)
		}
		if caseInsensitive {
			return strings.Contains(strings.ToLower(line), lowerQuery)
		}
		return strings.Contains(line, query)
	}

	var matches []grepMatch
	err = filepath.WalkDir(resolved, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if path != resolved && (name == ".git" || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if len(matches) >= grepMaxMatches {
			return filepath.SkipAll
		}

		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		// Skip binary files by checking the first 512 bytes for NUL.
		var probe [512]byte
		n, _ := f.Read(probe[:])
		if bytes.IndexByte(probe[:n], 0) >= 0 {
			return nil
		}
		if _, err := f.Seek(0, 0); err != nil {
			return nil
		}

		rel, err := filepath.Rel(resolved, path)
		if err != nil || rel == "." {
			rel = path
		}

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if matchLine(line) {
				matches = append(matches, grepMatch{
					File:    rel,
					Line:    lineNum,
					Content: line,
				})
				if len(matches) >= grepMaxMatches {
					return filepath.SkipAll
				}
			}
		}
		return nil
	})
	if err != nil {
		return ToolResult{}, fmt.Errorf("grep_search: %w", err)
	}

	var b strings.Builder
	for _, m := range matches {
		fmt.Fprintf(&b, "%s:%d: %s\n", m.File, m.Line, m.Content)
	}

	return ToolResult{
		Path: resolved,
		Text: b.String(),
		Output: map[string]any{
			"path":      resolved,
			"matches":   matches,
			"count":     len(matches),
			"truncated": len(matches) >= grepMaxMatches,
		},
	}, nil
}

// --- run_command ---

type runCommandTool struct{}

func (t *runCommandTool) Name() string { return "run_command" }
func (t *runCommandTool) Description() string {
	return "Execute a shell command in the workspace via /bin/sh -c with a timeout."
}
func (t *runCommandTool) ReadOnly() bool { return false }

func (t *runCommandTool) Declaration() *aicode.FunctionDeclaration {
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

func (t *runCommandTool) Execute(ctx context.Context, tc ToolContext, args map[string]any) (ToolResult, error) {
	cmdLine, ok := argString(args, "CommandLine")
	if !ok || strings.TrimSpace(cmdLine) == "" {
		return ToolResult{}, fmt.Errorf("run_command: CommandLine is required")
	}

	rawCwd, _ := argString(args, "Cwd")
	var cwd string
	var err error
	if tc.Guard != nil {
		cwd, err = tc.Guard.CheckCommandCwd(rawCwd)
	} else if strings.TrimSpace(rawCwd) != "" {
		cwd, err = resolveToolPath(tc, rawCwd)
	} else {
		cwd = tc.WorkspaceRoot
	}
	if err != nil {
		return ToolResult{}, err
	}

	timeout := defaultCmdTimeout
	if ms, ok := argInt(args, "TimeoutMs"); ok && ms > 0 {
		timeout = time.Duration(ms) * time.Millisecond
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "/bin/sh", "-c", cmdLine)
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

	return ToolResult{
		Text:     text,
		ExitCode: &exitCode,
		Output: map[string]any{
			"command":   cmdLine,
			"cwd":       cwd,
			"output":    text,
			"exit_code": exitCode,
		},
	}, nil
}
