package customizations

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RuleFile represents a loaded instruction/rule markdown file.
type RuleFile struct {
	Path    string `json:"path"`
	Source  string `json:"source"` // "user" | "workspace"
	Content string `json:"content"`
}

// LoadRules discovers and loads .gemini/GEMINI.md, AGENTS.md, and CLAUDE.md files
// from userGeminiDir and workspaceRoot, returning the loaded files and formatted <rules> block.
func LoadRules(workspaceRoot, userGeminiDir string) ([]RuleFile, string, error) {
	type candidate struct {
		path   string
		source string
	}

	var candidates []candidate
	if userGeminiDir != "" {
		candidates = append(candidates,
			candidate{filepath.Join(userGeminiDir, "GEMINI.md"), "user"},
			candidate{filepath.Join(userGeminiDir, "AGENTS.md"), "user"},
			candidate{filepath.Join(userGeminiDir, "CLAUDE.md"), "user"},
		)
	}
	if workspaceRoot != "" {
		candidates = append(candidates,
			candidate{filepath.Join(workspaceRoot, ".gemini", "GEMINI.md"), "workspace"},
			candidate{filepath.Join(workspaceRoot, "AGENTS.md"), "workspace"},
			candidate{filepath.Join(workspaceRoot, "CLAUDE.md"), "workspace"},
		)
	}

	seen := make(map[string]bool)
	var rules []RuleFile

	for _, c := range candidates {
		clean := filepath.Clean(c.path)
		if seen[clean] {
			continue
		}
		seen[clean] = true

		data, err := os.ReadFile(clean)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, "", fmt.Errorf("customizations: read rule %s: %w", clean, err)
		}
		text := strings.TrimSpace(string(data))
		if text == "" {
			continue
		}
		rules = append(rules, RuleFile{
			Path:    clean,
			Source:  c.source,
			Content: text,
		})
	}

	return rules, formatRulesPrompt(rules), nil
}

func formatRulesPrompt(rules []RuleFile) string {
	if len(rules) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<rules>\n")
	for _, r := range rules {
		fmt.Fprintf(&b, "<rule source=%q path=%q>\n%s\n</rule>\n", r.Source, r.Path, r.Content)
	}
	b.WriteString("</rules>")
	return b.String()
}
