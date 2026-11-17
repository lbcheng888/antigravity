package customizations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverSkillsAndFormatPrompt(t *testing.T) {
	root := t.TempDir()
	skillA := filepath.Join(root, "alpha")
	skillB := filepath.Join(root, "nested", "beta")
	if err := os.MkdirAll(skillA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(skillB, 0o755); err != nil {
		t.Fatal(err)
	}

	contentA := "---\nname: \"alpha-skill\"\ndescription: 'First test skill'\n---\n# Alpha\n"
	if err := os.WriteFile(filepath.Join(skillA, "SKILL.md"), []byte(contentA), 0o644); err != nil {
		t.Fatal(err)
	}

	contentB := "---\ndescription: |\n  Multi-line\n  description\n---\n# Beta\n"
	if err := os.WriteFile(filepath.Join(skillB, "SKILL.md"), []byte(contentB), 0o644); err != nil {
		t.Fatal(err)
	}

	missingDir := filepath.Join(root, "does-not-exist")
	skills, err := DiscoverSkills(root, missingDir)
	if err != nil {
		t.Fatalf("DiscoverSkills failed: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d: %+v", len(skills), skills)
	}
	if skills[0].Name != "alpha-skill" || skills[0].Description != "First test skill" {
		t.Fatalf("unexpected first skill: %+v", skills[0])
	}
	if skills[1].Name != "beta" || skills[1].Description != "Multi-line\ndescription" {
		t.Fatalf("unexpected second skill: %+v", skills[1])
	}

	prompt := FormatSkillsPrompt(skills)
	if !strings.HasPrefix(prompt, "<skills>\n") || !strings.HasSuffix(prompt, "</skills>") {
		t.Fatalf("unexpected prompt wrapper:\n%s", prompt)
	}
	if !strings.Contains(prompt, "alpha-skill") || !strings.Contains(prompt, "First test skill") {
		t.Fatalf("prompt missing skill info:\n%s", prompt)
	}
	if empty := FormatSkillsPrompt(nil); empty != "" {
		t.Fatalf("expected empty string for no skills, got %q", empty)
	}
}

func TestLoadRules(t *testing.T) {
	base := t.TempDir()
	userGemini := filepath.Join(base, "user-gemini")
	workspace := filepath.Join(base, "workspace")

	if err := os.MkdirAll(userGemini, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, ".gemini"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(userGemini, "GEMINI.md"), []byte("User global rule\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".gemini", "GEMINI.md"), []byte("Workspace gemini rule\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("Agents rule\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "CLAUDE.md"), []byte("   \n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rules, prompt, err := LoadRules(workspace, userGemini)
	if err != nil {
		t.Fatalf("LoadRules failed: %v", err)
	}
	if len(rules) != 3 {
		t.Fatalf("expected 3 non-empty rules, got %d: %+v", len(rules), rules)
	}
	if !strings.HasPrefix(prompt, "<rules>\n") || !strings.HasSuffix(prompt, "</rules>") {
		t.Fatalf("unexpected rules prompt:\n%s", prompt)
	}
	if !strings.Contains(prompt, "User global rule") || !strings.Contains(prompt, "Workspace gemini rule") || !strings.Contains(prompt, "Agents rule") {
		t.Fatalf("rules prompt missing expected content:\n%s", prompt)
	}
}

func TestValidatePluginStrictAndNonStrictAndList(t *testing.T) {
	pluginsRoot := t.TempDir()

	// 1. Valid complete plugin under .gemini-plugin/plugin.json.
	goodDir := filepath.Join(pluginsRoot, "good-plugin")
	if err := os.MkdirAll(filepath.Join(goodDir, ".gemini-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(goodDir, "skills", "lint"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(goodDir, ".gemini-plugin", "plugin.json"), []byte(`{"name":"good-plugin","version":"1.0.0","description":"All good"}`), 0o644)
	_ = os.WriteFile(filepath.Join(goodDir, "skills", "lint", "SKILL.md"), []byte("---\nname: lint\ndescription: Run linter\n---\n"), 0o644)
	_ = os.WriteFile(filepath.Join(goodDir, "mcp.json"), []byte(`{"mcpServers":{"srv":{"command":"echo"}}}`), 0o644)

	rep, err := ValidatePlugin(goodDir, true)
	if err != nil {
		t.Fatalf("ValidatePlugin(good, strict) error: %v", err)
	}
	if !rep.Valid || rep.Failed(true) {
		t.Fatalf("expected good plugin to pass strict validation, findings: %+v", rep.Findings)
	}
	if rep.Components["skills"] != 1 || rep.Components["mcpServers"] != 1 {
		t.Fatalf("unexpected component counts: %+v", rep.Components)
	}

	// 2. Plugin with warnings only (missing version/description): passes non-strict, fails strict.
	warnDir := filepath.Join(pluginsRoot, "warn-plugin")
	if err := os.MkdirAll(warnDir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(warnDir, "plugin.json"), []byte(`{"name":"warn-plugin"}`), 0o644)

	nonStrictRep, err := ValidatePlugin(warnDir, false)
	if err != nil {
		t.Fatalf("ValidatePlugin(warn, non-strict) error: %v", err)
	}
	if !nonStrictRep.Valid || nonStrictRep.Failed(false) {
		t.Fatalf("expected warn-plugin to pass non-strict, findings: %+v", nonStrictRep.Findings)
	}
	if nonStrictRep.Count(SeverityWarning) == 0 {
		t.Fatal("expected warnings on warn-plugin")
	}

	strictRep, err := ValidatePlugin(warnDir, true)
	if err != nil {
		t.Fatalf("ValidatePlugin(warn, strict) error: %v", err)
	}
	if strictRep.Valid || !strictRep.Failed(true) {
		t.Fatalf("expected warn-plugin to fail strict validation")
	}

	// 3. Plugin with missing manifest + invalid mcp.json: fails even non-strict.
	badDir := filepath.Join(pluginsRoot, "bad-plugin")
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(badDir, "mcp.json"), []byte(`{"mcpServers":{"broken":{}}}`), 0o644)

	badRep, err := ValidatePlugin(badDir, false)
	if err != nil {
		t.Fatalf("ValidatePlugin(bad) error: %v", err)
	}
	if badRep.Valid || badRep.Count(SeverityError) < 2 {
		t.Fatalf("expected >=2 errors on bad-plugin, got %+v", badRep.Findings)
	}

	// 4. ListInstalledPlugins.
	installed, err := ListInstalledPlugins(pluginsRoot)
	if err != nil {
		t.Fatalf("ListInstalledPlugins failed: %v", err)
	}
	if len(installed) != 3 {
		t.Fatalf("expected 3 installed plugins, got %d", len(installed))
	}
	if installed[1].Name != "good-plugin" || installed[1].Version != "1.0.0" {
		t.Fatalf("unexpected good-plugin summary: %+v", installed[1])
	}
	if len(installed[1].Skills) != 1 || installed[1].Skills[0] != "lint" {
		t.Fatalf("unexpected good-plugin skills: %+v", installed[1].Skills)
	}
	if len(installed[1].McpServers) != 1 || installed[1].McpServers[0] != "srv" {
		t.Fatalf("unexpected good-plugin mcpServers: %+v", installed[1].McpServers)
	}
}
