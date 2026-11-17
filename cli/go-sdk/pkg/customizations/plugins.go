package customizations

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Severity indicates the severity of a plugin validation finding.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Finding describes a single validation issue discovered in a plugin directory.
type Finding struct {
	Severity Severity `json:"severity"`
	File     string   `json:"file"`
	Message  string   `json:"message"`
}

// ValidationReport summarizes plugin manifest, component counts, and findings.
type ValidationReport struct {
	Dir        string         `json:"dir"`
	Manifest   string         `json:"manifest,omitempty"`
	Name       string         `json:"name,omitempty"`
	Strict     bool           `json:"strict"`
	Valid      bool           `json:"valid"`
	Components map[string]int `json:"components"`
	Findings   []Finding      `json:"findings"`
}

// Count returns the number of findings matching sev.
func (r *ValidationReport) Count(sev Severity) int {
	n := 0
	for _, f := range r.Findings {
		if f.Severity == sev {
			n++
		}
	}
	return n
}

// Failed reports whether validation failed under the given strictness setting.
func (r *ValidationReport) Failed(strict bool) bool {
	if r.Count(SeverityError) > 0 {
		return true
	}
	return strict && r.Count(SeverityWarning) > 0
}

// PluginSummary describes an installed plugin discovered under a plugins root.
type PluginSummary struct {
	Name       string   `json:"name"`
	Version    string   `json:"version,omitempty"`
	Path       string   `json:"path"`
	Skills     []string `json:"skills,omitempty"`
	McpServers []string `json:"mcpServers,omitempty"`
}

type pluginManifest struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

// ValidatePlugin inspects plugin.json / .gemini-plugin/plugin.json, skills/*/SKILL.md, and mcp.json.
func ValidatePlugin(pluginDir string, strict bool) (*ValidationReport, error) {
	info, err := os.Stat(pluginDir)
	if err != nil {
		return nil, fmt.Errorf("customizations: stat plugin dir %s: %w", pluginDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("customizations: %s is not a directory", pluginDir)
	}

	report := &ValidationReport{
		Dir:        pluginDir,
		Strict:     strict,
		Components: make(map[string]int),
	}

	manifestRel, manifestPath := findPluginManifest(pluginDir)
	if manifestPath == "" {
		report.Findings = append(report.Findings, Finding{
			Severity: SeverityError,
			File:     "plugin.json",
			Message:  "missing plugin manifest (expected plugin.json or .gemini-plugin/plugin.json)",
		})
	} else {
		report.Manifest = manifestRel
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			report.Findings = append(report.Findings, Finding{
				Severity: SeverityError,
				File:     manifestRel,
				Message:  fmt.Sprintf("failed to read manifest: %v", err),
			})
		} else {
			var mf pluginManifest
			if err := json.Unmarshal(data, &mf); err != nil {
				report.Findings = append(report.Findings, Finding{
					Severity: SeverityError,
					File:     manifestRel,
					Message:  fmt.Sprintf("invalid JSON: %v", err),
				})
			} else {
				report.Name = strings.TrimSpace(mf.Name)
				if report.Name == "" {
					report.Findings = append(report.Findings, Finding{
						Severity: SeverityError,
						File:     manifestRel,
						Message:  "manifest missing required \"name\" field",
					})
				}
				if strings.TrimSpace(mf.Version) == "" {
					report.Findings = append(report.Findings, Finding{
						Severity: SeverityWarning,
						File:     manifestRel,
						Message:  "manifest missing \"version\" field",
					})
				}
				if strings.TrimSpace(mf.Description) == "" {
					report.Findings = append(report.Findings, Finding{
						Severity: SeverityWarning,
						File:     manifestRel,
						Message:  "manifest missing \"description\" field",
					})
				}
			}
		}
	}

	validatePluginSkills(pluginDir, report)
	validatePluginMCP(pluginDir, report)

	report.Valid = !report.Failed(strict)
	return report, nil
}

// ListInstalledPlugins lists plugins found as subdirectories of pluginsRoot.
func ListInstalledPlugins(pluginsRoot string) ([]PluginSummary, error) {
	entries, err := os.ReadDir(pluginsRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("customizations: read plugins root %s: %w", pluginsRoot, err)
	}

	var out []PluginSummary
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(pluginsRoot, entry.Name())
		summary := PluginSummary{
			Name: entry.Name(),
			Path: dir,
		}

		if _, manifestPath := findPluginManifest(dir); manifestPath != "" {
			if data, err := os.ReadFile(manifestPath); err == nil {
				var mf pluginManifest
				if json.Unmarshal(data, &mf) == nil {
					if strings.TrimSpace(mf.Name) != "" {
						summary.Name = strings.TrimSpace(mf.Name)
					}
					summary.Version = strings.TrimSpace(mf.Version)
				}
			}
		}

		if skills, err := DiscoverSkills(filepath.Join(dir, "skills")); err == nil {
			for _, s := range skills {
				summary.Skills = append(summary.Skills, s.Name)
			}
		}
		if servers, err := readMCPServerNames(filepath.Join(dir, "mcp.json")); err == nil {
			summary.McpServers = servers
		}
		out = append(out, summary)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func findPluginManifest(pluginDir string) (rel string, full string) {
	candidates := []string{
		filepath.Join(".gemini-plugin", "plugin.json"),
		"plugin.json",
	}
	for _, c := range candidates {
		p := filepath.Join(pluginDir, c)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return filepath.ToSlash(c), p
		}
	}
	return "", ""
}

func validatePluginSkills(pluginDir string, report *ValidationReport) {
	skillsDir := filepath.Join(pluginDir, "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		rel := filepath.ToSlash(filepath.Join("skills", entry.Name(), "SKILL.md"))
		skillFile := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
		data, err := os.ReadFile(skillFile)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				report.Findings = append(report.Findings, Finding{
					Severity: SeverityWarning,
					File:     rel,
					Message:  "skill directory missing SKILL.md",
				})
				continue
			}
			report.Findings = append(report.Findings, Finding{
				Severity: SeverityError,
				File:     rel,
				Message:  fmt.Sprintf("failed to read SKILL.md: %v", err),
			})
			continue
		}

		fm, ok := parseFrontmatter(string(data))
		if !ok {
			report.Findings = append(report.Findings, Finding{
				Severity: SeverityError,
				File:     rel,
				Message:  "SKILL.md missing YAML frontmatter",
			})
			continue
		}
		if strings.TrimSpace(fm["name"]) == "" {
			report.Findings = append(report.Findings, Finding{
				Severity: SeverityWarning,
				File:     rel,
				Message:  "SKILL.md frontmatter missing \"name\"",
			})
		}
		if strings.TrimSpace(fm["description"]) == "" {
			report.Findings = append(report.Findings, Finding{
				Severity: SeverityWarning,
				File:     rel,
				Message:  "SKILL.md frontmatter missing \"description\"",
			})
		}
		report.Components["skills"]++
	}
}

func validatePluginMCP(pluginDir string, report *ValidationReport) {
	mcpPath := filepath.Join(pluginDir, "mcp.json")
	data, err := os.ReadFile(mcpPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		report.Findings = append(report.Findings, Finding{
			Severity: SeverityError,
			File:     "mcp.json",
			Message:  fmt.Sprintf("failed to read mcp.json: %v", err),
		})
		return
	}

	servers, err := parseMCPServersMap(data)
	if err != nil {
		report.Findings = append(report.Findings, Finding{
			Severity: SeverityError,
			File:     "mcp.json",
			Message:  fmt.Sprintf("invalid mcp.json: %v", err),
		})
		return
	}

	for name, cfg := range servers {
		if strings.TrimSpace(cfg.Command) == "" && strings.TrimSpace(cfg.URL) == "" {
			report.Findings = append(report.Findings, Finding{
				Severity: SeverityError,
				File:     "mcp.json",
				Message:  fmt.Sprintf("mcp server %q must specify \"command\" or \"url\"", name),
			})
			continue
		}
		report.Components["mcpServers"]++
	}
}

type mcpServerEntry struct {
	Command string `json:"command"`
	URL     string `json:"url"`
}

func parseMCPServersMap(data []byte) (map[string]mcpServerEntry, error) {
	var wrapped struct {
		MCPServers map[string]mcpServerEntry `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &wrapped); err != nil {
		return nil, err
	}
	if wrapped.MCPServers != nil {
		return wrapped.MCPServers, nil
	}
	var flat map[string]mcpServerEntry
	if err := json.Unmarshal(data, &flat); err != nil {
		return nil, err
	}
	return flat, nil
}

func readMCPServerNames(mcpPath string) ([]string, error) {
	data, err := os.ReadFile(mcpPath)
	if err != nil {
		return nil, err
	}
	servers, err := parseMCPServersMap(data)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
