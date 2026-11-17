package acp

import (
	"fmt"
	"strings"
)

// KindTitleAndLocations derives ACP ToolCall kind, human title, and source file locations
// from a function call name and argument map (mirroring acp/stepview semantics).
func KindTitleAndLocations(toolName string, args map[string]any) (kind string, title string, locs []ToolCallLocation) {
	lower := strings.ToLower(strings.TrimSpace(toolName))
	path := firstStringArg(args, "path", "absolute_path", "file_path", "target_file", "search_path")
	line := firstIntArg(args, "line", "start_line")
	if path != "" {
		loc := ToolCallLocation{Path: strings.TrimPrefix(path, "file://")}
		if line > 0 {
			l := line
			loc.Line = &l
		}
		locs = []ToolCallLocation{loc}
	}

	switch {
	case strings.Contains(lower, "read") || strings.Contains(lower, "view"):
		kind = "read"
		if path != "" {
			title = fmt.Sprintf("Read %s", path)
		} else {
			title = toolName
		}
	case strings.Contains(lower, "edit") || strings.Contains(lower, "write") || strings.Contains(lower, "replace"):
		kind = "edit"
		if path != "" {
			title = fmt.Sprintf("Edit %s", path)
		} else {
			title = toolName
		}
	case strings.Contains(lower, "command") || strings.Contains(lower, "bash") || strings.Contains(lower, "exec") || strings.Contains(lower, "run"):
		kind = "execute"
		if cmd := firstStringArg(args, "command", "command_line"); cmd != "" {
			title = cmd
		} else {
			title = toolName
		}
	case strings.Contains(lower, "grep") || strings.Contains(lower, "search") || strings.Contains(lower, "find") || strings.Contains(lower, "list"):
		kind = "search"
		if q := firstStringArg(args, "query", "pattern"); q != "" {
			title = fmt.Sprintf("%s %q", toolName, q)
		} else {
			title = toolName
		}
	default:
		kind = "other"
		title = toolName
	}
	return kind, title, locs
}

func firstStringArg(args map[string]any, keys ...string) string {
	for _, want := range keys {
		for k, v := range args {
			if strings.EqualFold(k, want) {
				if s, ok := v.(string); ok && s != "" {
					return s
				}
			}
		}
	}
	return ""
}

func firstIntArg(args map[string]any, keys ...string) int {
	for _, want := range keys {
		for k, v := range args {
			if strings.EqualFold(k, want) {
				switch n := v.(type) {
				case float64:
					return int(n)
				case int:
					return n
				case int64:
					return int(n)
				}
			}
		}
	}
	return 0
}
