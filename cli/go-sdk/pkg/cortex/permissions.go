package cortex

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Mode controls which tools are permitted during an engine turn.
type Mode string

const (
	ModeAgent Mode = "agent"
	ModePlan  Mode = "plan"
	ModeAsk   Mode = "ask"
)

// ErrPermissionDenied indicates a policy or workspace boundary violation.
var ErrPermissionDenied = errors.New("permission denied")

// PermissionGuard enforces workspace confinement and read-only mode policies.
type PermissionGuard struct {
	WorkspaceRoot              string
	AdditionalDirs             []string
	Mode                       Mode
	DangerouslySkipPermissions bool
}

// NewPermissionGuard constructs a PermissionGuard for the given workspace and mode.
func NewPermissionGuard(workspaceRoot string, mode Mode, additionalDirs ...string) *PermissionGuard {
	if mode == "" {
		mode = ModeAgent
	}
	return &PermissionGuard{
		WorkspaceRoot:  workspaceRoot,
		AdditionalDirs: additionalDirs,
		Mode:           mode,
	}
}

// IsReadOnlyMode reports whether the current mode restricts execution to read-only tools.
func (g *PermissionGuard) IsReadOnlyMode() bool {
	if g == nil {
		return false
	}
	return g.Mode == ModePlan || g.Mode == ModeAsk
}

// CheckTool verifies whether the tool is allowed under the current mode.
func (g *PermissionGuard) CheckTool(t Tool) error {
	if t == nil {
		return fmt.Errorf("%w: nil tool", ErrPermissionDenied)
	}
	if g == nil {
		return nil
	}
	if g.IsReadOnlyMode() && !t.ReadOnly() {
		return fmt.Errorf("%w: tool %q is not allowed in %q mode", ErrPermissionDenied, t.Name(), g.Mode)
	}
	return nil
}

// ResolvePath resolves rawPath relative to WorkspaceRoot and ensures it stays within
// WorkspaceRoot or AdditionalDirs after symlink resolution, unless DangerouslySkipPermissions is set.
func (g *PermissionGuard) ResolvePath(rawPath string) (string, error) {
	if strings.TrimSpace(rawPath) == "" {
		return "", fmt.Errorf("path must not be empty")
	}

	root := ""
	if g != nil {
		root = g.WorkspaceRoot
	}

	candidate := rawPath
	if !filepath.IsAbs(candidate) {
		if root == "" {
			abs, err := filepath.Abs(candidate)
			if err != nil {
				return "", fmt.Errorf("resolve path: %w", err)
			}
			candidate = abs
		} else {
			candidate = filepath.Join(root, candidate)
		}
	}
	candidate = filepath.Clean(candidate)

	resolved, err := evalSymlinksWithMissing(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve symlinks for %q: %w", rawPath, err)
	}

	if g == nil || g.DangerouslySkipPermissions {
		return resolved, nil
	}

	allowedRoots := make([]string, 0, 1+len(g.AdditionalDirs))
	if root != "" {
		allowedRoots = append(allowedRoots, root)
	}
	allowedRoots = append(allowedRoots, g.AdditionalDirs...)
	if len(allowedRoots) == 0 {
		return "", fmt.Errorf("%w: no workspace root configured", ErrPermissionDenied)
	}

	for _, allowed := range allowedRoots {
		if allowed == "" {
			continue
		}
		absAllowed, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}
		resolvedAllowed, err := evalSymlinksWithMissing(absAllowed)
		if err != nil {
			continue
		}
		if isWithinRoot(resolvedAllowed, resolved) {
			return resolved, nil
		}
	}

	return "", fmt.Errorf("%w: path %q escapes workspace root", ErrPermissionDenied, rawPath)
}

// CheckCommandCwd resolves and validates a working directory for command execution.
func (g *PermissionGuard) CheckCommandCwd(cwd string) (string, error) {
	if strings.TrimSpace(cwd) == "" {
		if g != nil && g.WorkspaceRoot != "" {
			return g.ResolvePath(g.WorkspaceRoot)
		}
		return os.Getwd()
	}
	return g.ResolvePath(cwd)
}

// isWithinRoot checks whether target is equal to or nested inside root using filepath.Rel.
func isWithinRoot(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return !filepath.IsAbs(rel)
}

// evalSymlinksWithMissing resolves symlinks for path even when trailing segments do not yet exist.
func evalSymlinksWithMissing(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	var missing []string
	cur := abs
	for {
		parent := filepath.Dir(cur)
		base := filepath.Base(cur)
		missing = append(missing, base)
		if parent == cur {
			return abs, nil
		}
		resolvedParent, err := filepath.EvalSymlinks(parent)
		if err == nil {
			parts := make([]string, 0, len(missing)+1)
			parts = append(parts, resolvedParent)
			for i := len(missing) - 1; i >= 0; i-- {
				parts = append(parts, missing[i])
			}
			return filepath.Join(parts...), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		cur = parent
	}
}
