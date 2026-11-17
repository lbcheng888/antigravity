package customizations

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SkillInfo describes a discovered skill and its SKILL.md location.
type SkillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
}

// DiscoverSkills scans each directory recursively for **/SKILL.md and parses YAML frontmatter.
// Missing directories are skipped without error.
func DiscoverSkills(dirs ...string) ([]SkillInfo, error) {
	var skills []SkillInfo
	seen := make(map[string]bool)

	for _, root := range dirs {
		if root == "" {
			continue
		}
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		}

		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() || d.Name() != "SKILL.md" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("customizations: read %s: %w", path, err)
			}
			fm, _ := parseFrontmatter(string(data))
			name := strings.TrimSpace(fm["name"])
			if name == "" {
				name = filepath.Base(filepath.Dir(path))
			}
			if seen[name] {
				return nil
			}
			seen[name] = true
			skills = append(skills, SkillInfo{
				Name:        name,
				Description: strings.TrimSpace(fm["description"]),
				Path:        path,
			})
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("customizations: walk skills %s: %w", root, err)
		}
	}
	return skills, nil
}

// FormatSkillsPrompt renders discovered skills into a <skills> prompt block.
func FormatSkillsPrompt(skills []SkillInfo) string {
	if len(skills) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<skills>\n")
	for _, s := range skills {
		if s.Description != "" {
			fmt.Fprintf(&b, "- %s (%s): %s\n", s.Name, s.Path, s.Description)
		} else {
			fmt.Fprintf(&b, "- %s (%s)\n", s.Name, s.Path)
		}
	}
	b.WriteString("</skills>")
	return b.String()
}

// parseFrontmatter extracts top-level key/value pairs from YAML frontmatter delimited by ---.
func parseFrontmatter(content string) (map[string]string, bool) {
	norm := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(norm, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return map[string]string{}, false
	}

	endIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			endIdx = i
			break
		}
	}
	if endIdx < 0 {
		return map[string]string{}, false
	}

	result := make(map[string]string)
	for i := 1; i < endIdx; i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:colon])
		val := strings.TrimSpace(line[colon+1:])

		if val == "|" || val == ">" || val == "|-" || val == ">-" {
			var block []string
			for j := i + 1; j < endIdx; j++ {
				next := lines[j]
				if len(next) > 0 && (next[0] == ' ' || next[0] == '\t') {
					block = append(block, strings.TrimSpace(next))
					i = j
				} else if strings.TrimSpace(next) == "" {
					block = append(block, "")
					i = j
				} else {
					break
				}
			}
			joiner := "\n"
			if strings.HasPrefix(val, ">") {
				joiner = " "
			}
			result[key] = strings.TrimSpace(strings.Join(block, joiner))
			continue
		}

		result[key] = unquoteYAML(val)
	}
	return result, true
}

func unquoteYAML(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
