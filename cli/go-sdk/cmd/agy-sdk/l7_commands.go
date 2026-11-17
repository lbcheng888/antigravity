package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"antigravity.dev/agy-sdk/pkg/aicode"
	"antigravity.dev/agy-sdk/pkg/cortex"
	"antigravity.dev/agy-sdk/pkg/customizations"
	"antigravity.dev/agy-sdk/pkg/mcp"
	"antigravity.dev/agy-sdk/pkg/planexec"
	"antigravity.dev/agy-sdk/pkg/sandbox"
	"antigravity.dev/agy-sdk/pkg/sessionstore"
	"antigravity.dev/agy-sdk/pkg/swarmrun"
)

type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringSliceFlag) Set(val string) error {
	*s = append(*s, val)
	return nil
}

func defaultGeminiDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".", ".gemini", "antigravity")
	}
	return filepath.Join(home, ".gemini", "antigravity")
}

func resolveDataRoot(dataDir string) string {
	if strings.TrimSpace(dataDir) != "" {
		return dataDir
	}
	return defaultGeminiDir()
}

func mcpConfigPath(dataDir string) string {
	return filepath.Join(resolveDataRoot(dataDir), "mcp_config.json")
}

func sessionsDirPath(dataDir string) string {
	return filepath.Join(resolveDataRoot(dataDir), "sessions")
}

func defaultPluginsDir() string {
	return filepath.Join(defaultGeminiDir(), "plugins")
}

func defaultSkillDirs(cwd string) []string {
	gDir := defaultGeminiDir()
	dirs := []string{
		filepath.Join(gDir, "builtin", "skills"),
		filepath.Join(gDir, "skills"),
	}
	if strings.TrimSpace(cwd) != "" {
		dirs = append(dirs, filepath.Join(cwd, ".gemini", "skills"))
	}
	return dirs
}

// --- mcp subcommand ---

func runMCP(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printMCPUsage(stderr)
		return 2
	}
	switch args[0] {
	case "list":
		return runMCPList(args[1:], stdout, stderr)
	case "add":
		return runMCPAdd(args[1:], stdout, stderr)
	case "remove", "rm":
		return runMCPRemove(args[1:], stdout, stderr)
	case "enable":
		return runMCPSetEnabled(args[1:], true, stdout, stderr)
	case "disable":
		return runMCPSetEnabled(args[1:], false, stdout, stderr)
	case "tools":
		return runMCPTools(ctx, args[1:], stdout, stderr)
	case "-h", "--help", "help":
		printMCPUsage(stdout)
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "unknown mcp subcommand %q\n", args[0])
		printMCPUsage(stderr)
		return 2
	}
}

func printMCPUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, `Usage:
  agy-sdk mcp list    [--data-dir <dir>] [--json]
  agy-sdk mcp add     <name> [--type stdio|http] [--env KEY=VAL] [--header KEY:VAL] [--data-dir <dir>] [--json] <command|url> [args...]
  agy-sdk mcp remove  <name> [--data-dir <dir>] [--json]
  agy-sdk mcp enable  <name> [--data-dir <dir>] [--json]
  agy-sdk mcp disable <name> [--data-dir <dir>] [--json]
  agy-sdk mcp tools   [<name>] [--data-dir <dir>] [--json]`)
}

func runMCPList(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mcp list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var dataDir string
	var jsonOut bool
	fs.StringVar(&dataDir, "data-dir", "", "override config directory")
	fs.BoolVar(&jsonOut, "json", false, "output JSON")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}

	store := mcp.NewConfigStore(mcpConfigPath(dataDir))
	if err := store.Load(); err != nil {
		_, _ = fmt.Fprintf(stderr, "mcp load failed: %v\n", err)
		return 1
	}
	servers := store.List()
	if jsonOut {
		type mcpServerJSON struct {
			Name string `json:"name"`
			mcp.ServerConfig
		}
		out := make([]mcpServerJSON, len(servers))
		for i, s := range servers {
			out[i] = mcpServerJSON{Name: s.Name, ServerConfig: s}
		}
		return writeJSON(stdout, stderr, out)
	}
	if len(servers) == 0 {
		_, _ = fmt.Fprintln(stdout, "No MCP servers configured.")
		return 0
	}
	for _, s := range servers {
		status := "enabled"
		if s.Disabled {
			status = "disabled"
		}
		target := s.Command
		if len(s.Args) > 0 {
			target += " " + strings.Join(s.Args, " ")
		}
		if s.Type == "http" {
			target = s.URL
		}
		_, _ = fmt.Fprintf(stdout, "%-20s %-6s %-9s %s\n", s.Name, s.Type, status, target)
	}
	return 0
}

func runMCPAdd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mcp add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var srvType string
	var envs stringSliceFlag
	var headers stringSliceFlag
	var dataDir string
	var jsonOut bool
	fs.StringVar(&srvType, "type", "", "server transport type: stdio or http")
	fs.Var(&envs, "env", "environment variable KEY=VAL (repeatable)")
	fs.Var(&headers, "header", "HTTP header KEY:VAL (repeatable)")
	fs.StringVar(&dataDir, "data-dir", "", "override config directory")
	fs.BoolVar(&jsonOut, "json", false, "output JSON")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() < 2 {
		_, _ = fmt.Fprintln(stderr, "usage: agy-sdk mcp add <name> [--type stdio|http] [--env KEY=VAL] [--header KEY:VAL] <command|url> [args...]")
		return 2
	}

	name := fs.Arg(0)
	target := fs.Arg(1)
	extraArgs := fs.Args()[2:]

	if srvType == "" {
		if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
			srvType = "http"
		} else {
			srvType = "stdio"
		}
	}

	cfg := mcp.ServerConfig{
		Name: name,
		Type: srvType,
	}
	switch srvType {
	case "http":
		cfg.URL = target
	case "stdio":
		cfg.Command = target
		cfg.Args = extraArgs
	default:
		_, _ = fmt.Fprintf(stderr, "invalid --type %q: must be stdio or http\n", srvType)
		return 2
	}

	if len(envs) > 0 {
		cfg.Env = make(map[string]string, len(envs))
		for _, item := range envs {
			k, v, ok := strings.Cut(item, "=")
			if !ok || strings.TrimSpace(k) == "" {
				_, _ = fmt.Fprintf(stderr, "invalid --env %q: expected KEY=VAL\n", item)
				return 2
			}
			cfg.Env[strings.TrimSpace(k)] = v
		}
	}
	if len(headers) > 0 {
		cfg.Headers = make(map[string]string, len(headers))
		for _, item := range headers {
			k, v, ok := strings.Cut(item, ":")
			if !ok || strings.TrimSpace(k) == "" {
				_, _ = fmt.Fprintf(stderr, "invalid --header %q: expected KEY:VAL\n", item)
				return 2
			}
			cfg.Headers[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}

	store := mcp.NewConfigStore(mcpConfigPath(dataDir))
	if err := store.Add(cfg); err != nil {
		_, _ = fmt.Fprintf(stderr, "mcp add failed: %v\n", err)
		return 1
	}
	if jsonOut {
		return writeJSON(stdout, stderr, cfg)
	}
	_, _ = fmt.Fprintf(stdout, "Added MCP server %q (%s)\n", cfg.Name, cfg.Type)
	return 0
}

func runMCPRemove(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mcp remove", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var dataDir string
	var jsonOut bool
	fs.StringVar(&dataDir, "data-dir", "", "override config directory")
	fs.BoolVar(&jsonOut, "json", false, "output JSON")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		_, _ = fmt.Fprintln(stderr, "usage: agy-sdk mcp remove <name> [--data-dir <dir>] [--json]")
		return 2
	}
	name := fs.Arg(0)
	store := mcp.NewConfigStore(mcpConfigPath(dataDir))
	if err := store.Remove(name); err != nil {
		_, _ = fmt.Fprintf(stderr, "mcp remove failed: %v\n", err)
		return 1
	}
	if jsonOut {
		return writeJSON(stdout, stderr, map[string]any{"removed": name})
	}
	_, _ = fmt.Fprintf(stdout, "Removed MCP server %q\n", name)
	return 0
}

func runMCPSetEnabled(args []string, enabled bool, stdout, stderr io.Writer) int {
	verb := "enable"
	if !enabled {
		verb = "disable"
	}
	fs := flag.NewFlagSet("mcp "+verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var dataDir string
	var jsonOut bool
	fs.StringVar(&dataDir, "data-dir", "", "override config directory")
	fs.BoolVar(&jsonOut, "json", false, "output JSON")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		_, _ = fmt.Fprintf(stderr, "usage: agy-sdk mcp %s <name> [--data-dir <dir>] [--json]\n", verb)
		return 2
	}
	name := fs.Arg(0)
	store := mcp.NewConfigStore(mcpConfigPath(dataDir))
	if err := store.SetEnabled(name, enabled); err != nil {
		_, _ = fmt.Fprintf(stderr, "mcp %s failed: %v\n", verb, err)
		return 1
	}
	if jsonOut {
		return writeJSON(stdout, stderr, map[string]any{"name": name, "enabled": enabled})
	}
	_, _ = fmt.Fprintf(stdout, "MCP server %q %sd\n", name, verb)
	return 0
}

func runMCPTools(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mcp tools", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var dataDir string
	var jsonOut bool
	fs.StringVar(&dataDir, "data-dir", "", "override config directory")
	fs.BoolVar(&jsonOut, "json", false, "output JSON")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}

	store := mcp.NewConfigStore(mcpConfigPath(dataDir))
	if err := store.Load(); err != nil {
		_, _ = fmt.Fprintf(stderr, "mcp load failed: %v\n", err)
		return 1
	}
	servers := store.List()
	var targetName string
	if fs.NArg() > 0 {
		targetName = fs.Arg(0)
	}

	type serverTools struct {
		Server string            `json:"server"`
		Tools  []mcp.MCPToolInfo `json:"tools"`
	}
	var results []serverTools
	matched := false

	for _, s := range servers {
		if targetName != "" && s.Name != targetName {
			continue
		}
		if targetName == "" && s.Disabled {
			continue
		}
		matched = true
		client, err := connectMCPClient(ctx, s)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "mcp connect %q failed: %v\n", s.Name, err)
			return 1
		}
		tools, listErr := client.ListTools(ctx)
		_ = client.Close()
		if listErr != nil {
			_, _ = fmt.Fprintf(stderr, "mcp list tools %q failed: %v\n", s.Name, listErr)
			return 1
		}
		results = append(results, serverTools{Server: s.Name, Tools: tools})
	}

	if targetName != "" && !matched {
		_, _ = fmt.Fprintf(stderr, "mcp: server %q not found\n", targetName)
		return 1
	}
	if jsonOut {
		return writeJSON(stdout, stderr, results)
	}
	if len(results) == 0 {
		_, _ = fmt.Fprintln(stdout, "No enabled MCP servers configured.")
		return 0
	}
	for _, r := range results {
		_, _ = fmt.Fprintf(stdout, "Server: %s (%d tools)\n", r.Server, len(r.Tools))
		for _, t := range r.Tools {
			_, _ = fmt.Fprintf(stdout, "  - %-28s %s\n", t.Name, t.Description)
		}
	}
	return 0
}

func connectMCPClient(ctx context.Context, cfg mcp.ServerConfig) (*mcp.Client, error) {
	var (
		client *mcp.Client
		err    error
	)
	switch cfg.Type {
	case "http":
		client, err = mcp.NewHTTPClient(cfg.URL, cfg.Headers)
	default:
		client, err = mcp.NewStdioClient(cfg.Command, cfg.Args, cfg.Env)
	}
	if err != nil {
		return nil, err
	}
	if err := client.Initialize(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

// --- plugin subcommand ---

func runPlugin(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: agy-sdk plugin <list|validate> [flags]")
		return 2
	}
	switch args[0] {
	case "list":
		return runPluginList(args[1:], stdout, stderr)
	case "validate":
		return runPluginValidate(args[1:], stdout, stderr)
	case "-h", "--help", "help":
		_, _ = fmt.Fprintln(stdout, "usage:\n  agy-sdk plugin list [--plugins-dir <dir>] [--json]\n  agy-sdk plugin validate [path] [--strict] [--json]")
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "unknown plugin subcommand %q\n", args[0])
		return 2
	}
}

func runPluginList(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("plugin list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var pluginsDir string
	var jsonOut bool
	fs.StringVar(&pluginsDir, "plugins-dir", defaultPluginsDir(), "plugins root directory")
	fs.BoolVar(&jsonOut, "json", false, "output JSON")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}

	plugins, err := customizations.ListInstalledPlugins(pluginsDir)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "plugin list failed: %v\n", err)
		return 1
	}
	if jsonOut {
		return writeJSON(stdout, stderr, plugins)
	}
	if len(plugins) == 0 {
		_, _ = fmt.Fprintf(stdout, "No plugins installed under %s.\n", pluginsDir)
		return 0
	}
	for _, p := range plugins {
		_, _ = fmt.Fprintf(stdout, "%-24s version=%-10s skills=%d mcp=%d path=%s\n",
			p.Name, p.Version, len(p.Skills), len(p.McpServers), p.Path)
	}
	return 0
}

func runPluginValidate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("plugin validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var strict bool
	var jsonOut bool
	fs.BoolVar(&strict, "strict", false, "treat warnings as validation failures")
	fs.BoolVar(&jsonOut, "json", false, "output JSON")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	pluginDir := "."
	if fs.NArg() > 0 {
		pluginDir = fs.Arg(0)
	}

	report, err := customizations.ValidatePlugin(pluginDir, strict)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "plugin validate failed: %v\n", err)
		return 1
	}
	if jsonOut {
		code := writeJSON(stdout, stderr, report)
		if code != 0 {
			return code
		}
		if report.Failed(strict) {
			return 1
		}
		return 0
	}

	_, _ = fmt.Fprintf(stdout, "Plugin:   %s (dir=%s)\n", report.Name, report.Dir)
	_, _ = fmt.Fprintf(stdout, "Valid:    %v (strict=%v, errors=%d, warnings=%d)\n",
		report.Valid, report.Strict, report.Count(customizations.SeverityError), report.Count(customizations.SeverityWarning))
	for _, f := range report.Findings {
		_, _ = fmt.Fprintf(stdout, "  [%s] %s: %s\n", f.Severity, f.File, f.Message)
	}
	if report.Failed(strict) {
		return 1
	}
	return 0
}

// --- skills subcommand ---

func runSkills(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("skills", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var cwd string
	var skillsDir string
	var jsonOut bool
	fs.StringVar(&cwd, "cwd", "", "workspace directory to scan for .gemini/skills")
	fs.StringVar(&skillsDir, "skills-dir", "", "override skill scan directory")
	fs.BoolVar(&jsonOut, "json", false, "output JSON")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}

	var dirs []string
	if strings.TrimSpace(skillsDir) != "" {
		dirs = []string{skillsDir}
	} else {
		if cwd == "" {
			if wd, err := os.Getwd(); err == nil {
				cwd = wd
			}
		}
		dirs = defaultSkillDirs(cwd)
	}

	skills, err := customizations.DiscoverSkills(dirs...)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "discover skills failed: %v\n", err)
		return 1
	}
	if jsonOut {
		return writeJSON(stdout, stderr, skills)
	}
	if len(skills) == 0 {
		_, _ = fmt.Fprintln(stdout, "No skills found.")
		return 0
	}
	for _, s := range skills {
		if s.Description != "" {
			_, _ = fmt.Fprintf(stdout, "%-24s %s (%s)\n", s.Name, s.Description, s.Path)
		} else {
			_, _ = fmt.Fprintf(stdout, "%-24s (%s)\n", s.Name, s.Path)
		}
	}
	return 0
}

// --- sessions subcommand ---

func runSessions(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: agy-sdk sessions <list|show> [flags]")
		return 2
	}
	switch args[0] {
	case "list":
		return runSessionsList(args[1:], stdout, stderr)
	case "show":
		return runSessionsShow(args[1:], stdout, stderr)
	case "-h", "--help", "help":
		_, _ = fmt.Fprintln(stdout, "usage:\n  agy-sdk sessions list [--cwd <dir>] [--data-dir <dir>] [--json]\n  agy-sdk sessions show <id> [--data-dir <dir>] [--json]")
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "unknown sessions subcommand %q\n", args[0])
		return 2
	}
}

func runSessionsList(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sessions list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var cwd string
	var dataDir string
	var jsonOut bool
	fs.StringVar(&cwd, "cwd", "", "filter sessions by workspace directory")
	fs.StringVar(&dataDir, "data-dir", "", "override data directory")
	fs.BoolVar(&jsonOut, "json", false, "output JSON")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}

	store := sessionstore.NewStore(sessionsDirPath(dataDir))
	list, err := store.List(cwd)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "sessions list failed: %v\n", err)
		return 1
	}
	if jsonOut {
		return writeJSON(stdout, stderr, list)
	}
	if len(list) == 0 {
		_, _ = fmt.Fprintln(stdout, "No sessions found.")
		return 0
	}
	for _, s := range list {
		_, _ = fmt.Fprintf(stdout, "%-28s msgs=%-3d mode=%-6s model=%-26s updated=%s cwd=%s\n",
			s.ID, s.MessageCount, s.Mode, s.Model, s.UpdatedAt.Format(time.RFC3339), s.Cwd)
	}
	return 0
}

func runSessionsShow(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sessions show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var dataDir string
	var jsonOut bool
	fs.StringVar(&dataDir, "data-dir", "", "override data directory")
	fs.BoolVar(&jsonOut, "json", false, "output JSON")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		_, _ = fmt.Fprintln(stderr, "usage: agy-sdk sessions show <id> [--data-dir <dir>] [--json]")
		return 2
	}
	id := fs.Arg(0)

	store := sessionstore.NewStore(sessionsDirPath(dataDir))
	rec, err := store.Load(id)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "sessions show failed: %v\n", err)
		return 1
	}
	if jsonOut {
		return writeJSON(stdout, stderr, rec)
	}
	_, _ = fmt.Fprintf(stdout, "ID:      %s\n", rec.ID)
	_, _ = fmt.Fprintf(stdout, "Cwd:     %s\n", rec.Cwd)
	_, _ = fmt.Fprintf(stdout, "Model:   %s\n", rec.Model)
	_, _ = fmt.Fprintf(stdout, "Mode:    %s\n", rec.Mode)
	_, _ = fmt.Fprintf(stdout, "Updated: %s\n", rec.UpdatedAt.Format(time.RFC3339))
	_, _ = fmt.Fprintf(stdout, "Turns:   %d\n", len(rec.History))
	for i, c := range rec.History {
		if c == nil {
			continue
		}
		var parts []string
		for _, p := range c.Parts {
			if p == nil {
				continue
			}
			if p.Text != "" {
				parts = append(parts, p.Text)
			} else if p.FunctionCall != nil {
				parts = append(parts, "[call:"+p.FunctionCall.Name+"]")
			} else if p.FunctionResponse != nil {
				parts = append(parts, "[resp:"+p.FunctionResponse.Name+"]")
			}
		}
		text := strings.ReplaceAll(strings.Join(parts, " "), "\n", " ")
		if len(text) > 120 {
			text = text[:117] + "..."
		}
		_, _ = fmt.Fprintf(stdout, "  [%d] %s: %s\n", i+1, c.Role, text)
	}
	return 0
}

// --- swarm run subcommand ---

func runSwarmRun(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("swarm run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var af authFlags
	af.register(fs)
	var goal string
	var model string
	var cwd string
	var swarmDir string
	var title string
	var maxSteps int
	var jsonOut bool
	fs.StringVar(&goal, "p", "", "swarm coordinator goal prompt")
	fs.StringVar(&model, "m", "gemini-4-hydrogen-medium", "default model ID")
	fs.StringVar(&cwd, "cwd", "", "workspace root directory (default: current directory)")
	fs.StringVar(&swarmDir, "swarm-dir", "", "swarm state directory (default: temporary directory)")
	fs.StringVar(&title, "title", "agy-sdk swarm", "swarm title")
	fs.IntVar(&maxSteps, "max-steps", cortex.DefaultMaxSteps, "maximum steps per agent turn")
	fs.BoolVar(&jsonOut, "json", false, "output JSON")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}

	if goal == "" && fs.NArg() > 0 {
		goal = strings.Join(fs.Args(), " ")
	}
	if strings.TrimSpace(goal) == "" {
		_, _ = fmt.Fprintln(stderr, "usage: agy-sdk swarm run -p \"<goal>\" [-m <model>] [--cwd <dir>] [--swarm-dir <dir>]")
		return 2
	}
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "failed to get working directory: %v\n", err)
			return 1
		}
	}
	if swarmDir == "" {
		var err error
		swarmDir, err = os.MkdirTemp("", "agy-swarm-*")
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "failed to create swarm dir: %v\n", err)
			return 1
		}
	}

	ca := af.newChainedAuth()
	client := aicode.NewClient(ca)

	orch, err := swarmrun.NewOrchestrator(swarmrun.Config{
		Dir:           swarmDir,
		WorkspaceRoot: cwd,
		Title:         title,
		Model:         model,
		Streamer:      cortex.NewClientStreamer(client),
		Guard:         cortex.NewPermissionGuard(cwd, cortex.ModeAgent),
		MaxSteps:      maxSteps,
		OnEvent: func(ev swarmrun.AgentEvent) {
			if jsonOut {
				return
			}
			switch ev.Event.Type {
			case cortex.EventToolStart:
				if ev.Event.ToolCall != nil {
					_, _ = fmt.Fprintf(stderr, "[%s] tool %s...\n", ev.Agent, ev.Event.ToolCall.Name)
				}
			}
		},
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "swarm init failed: %v\n", err)
		return 1
	}

	res, err := orch.Run(ctx, goal)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "swarm run failed: %v\n", err)
		return 1
	}
	if jsonOut {
		return writeJSON(stdout, stderr, res)
	}
	_, _ = fmt.Fprintf(stdout, "Swarm Dir: %s\n", swarmDir)
	_, _ = fmt.Fprintf(stdout, "HeadSeq:   %d\n", res.HeadSeq)
	if res.FinalText != "" {
		_, _ = fmt.Fprintln(stdout, res.FinalText)
	}
	return 0
}

// --- trace-proxy subcommand ---

func runTraceProxy(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("trace-proxy", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var listenAddr string
	var upstreamRaw string
	var outPath string
	fs.StringVar(&listenAddr, "listen", "127.0.0.1:8899", "listen address host:port")
	fs.StringVar(&upstreamRaw, "upstream", "https://daily-cloudcode-pa.googleapis.com", "upstream base URL")
	fs.StringVar(&outPath, "out", "trace.jsonl", "JSONL trace output path")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}

	u, err := url.Parse(upstreamRaw)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "invalid --upstream URL %q: %v\n", upstreamRaw, err)
		return 2
	}
	proxy, err := sandbox.NewTraceProxyFile(u, outPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "trace-proxy init failed: %v\n", err)
		return 1
	}
	defer func() { _ = proxy.Close() }()

	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "trace-proxy listen %s failed: %v\n", listenAddr, err)
		return 1
	}

	srv := &http.Server{Handler: proxy}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	_, _ = fmt.Fprintf(stdout, "Trace proxy listening on http://%s -> %s (out=%s)\n", ln.Addr().String(), u.String(), outPath)

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		return 0
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			_, _ = fmt.Fprintf(stderr, "trace-proxy server error: %v\n", err)
			return 1
		}
		return 0
	}
}

// --- agent L7 helpers ---

func registerConfiguredMCPTools(ctx context.Context, reg *cortex.Registry, dataDir string, stderr io.Writer) []io.Closer {
	store := mcp.NewConfigStore(mcpConfigPath(dataDir))
	if err := store.Load(); err != nil {
		_, _ = fmt.Fprintf(stderr, "warning: failed to load mcp_config.json: %v\n", err)
		return nil
	}
	var closers []io.Closer
	for _, cfg := range store.List() {
		if cfg.Disabled {
			continue
		}
		client, err := connectMCPClient(ctx, cfg)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "warning: mcp server %q connect failed: %v\n", cfg.Name, err)
			continue
		}
		if _, err := mcp.RegisterMCPTools(ctx, reg, cfg.Name, client); err != nil {
			_, _ = fmt.Fprintf(stderr, "warning: mcp server %q list tools failed: %v\n", cfg.Name, err)
			_ = client.Close()
			continue
		}
		closers = append(closers, client)
	}
	return closers
}

func buildSkillsAndRulesPrompt(cwd, dataDir string, stderr io.Writer) string {
	var sections []string
	skills, err := customizations.DiscoverSkills(defaultSkillDirs(cwd)...)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "warning: skill discovery failed: %v\n", err)
	} else if block := customizations.FormatSkillsPrompt(skills); block != "" {
		sections = append(sections, block)
	}

	_, rulesBlock, err := customizations.LoadRules(cwd, resolveDataRoot(dataDir))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "warning: rules load failed: %v\n", err)
	} else if rulesBlock != "" {
		sections = append(sections, rulesBlock)
	}
	return strings.Join(sections, "\n\n")
}

func loadJSONSchemaBytes(spec string) ([]byte, error) {
	trimmed := strings.TrimSpace(spec)
	if trimmed == "" {
		return nil, nil
	}
	if strings.HasPrefix(trimmed, "{") {
		return []byte(trimmed), nil
	}
	data, err := os.ReadFile(trimmed)
	if err != nil {
		return nil, fmt.Errorf("read --json-schema %q: %w", spec, err)
	}
	return data, nil
}

func executeAgentTurn(
	ctx context.Context,
	engine *cortex.Engine,
	planModel string,
	cwd string,
	extraPrompt string,
	history []*aicode.Content,
	prompt string,
) ([]*aicode.Content, *cortex.TurnSummary, error) {
	if strings.TrimSpace(planModel) == "" {
		return engine.RunTurn(ctx, history, prompt)
	}
	runner := &planexec.Runner{
		Streamer:         engine.Streamer,
		PlanModel:        planModel,
		ExecModel:        engine.Model,
		Registry:         engine.Registry,
		WorkspaceRoot:    cwd,
		Trajectory:       engine.Trajectory,
		GenerationConfig: engine.GenerationConfig,
		MaxSteps:         engine.MaxSteps,
		ExtraPrompt:      extraPrompt,
		OnEvent:          engine.OnEvent,
	}
	res, err := runner.Run(ctx, history, prompt)
	if res == nil {
		return history, nil, err
	}
	return res.History, res.Summary, err
}
