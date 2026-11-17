package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"antigravity.dev/agy-sdk/pkg/acp"
	"antigravity.dev/agy-sdk/pkg/aicode"
	"antigravity.dev/agy-sdk/pkg/auth"
	"antigravity.dev/agy-sdk/pkg/cortex"
	"antigravity.dev/agy-sdk/pkg/printmode"
	"antigravity.dev/agy-sdk/pkg/sandbox"
	"antigravity.dev/agy-sdk/pkg/sessionstore"
	"antigravity.dev/agy-sdk/pkg/swarm"
)

const binaryName = "agy-sdk"

type authFlags struct {
	dataDir    string
	appDataDir string
	forceFile  bool
}

func (a *authFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&a.dataDir, "data-dir", "", "override token storage data directory")
	fs.StringVar(&a.appDataDir, "app-data-dir", "", "override app data directory name (default: antigravity-cli)")
	fs.BoolVar(&a.forceFile, "file", false, "force file-based token storage instead of OS keyring")
}

func (a *authFlags) newChainedAuth() *auth.ChainedAuth {
	return auth.NewChainedAuth(a.dataDir, a.appDataDir, a.forceFile)
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	os.Exit(run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stdout)
		return 0
	}

	switch args[0] {
	case "version", "--version", "-v":
		_, _ = fmt.Fprintf(stdout, "%s v%s (%s/%s)\n", binaryName, aicode.DefaultClientVersion, runtime.GOOS, runtime.GOARCH)
		return 0
	case "help", "--help", "-h":
		printUsage(stdout)
		return 0
	case "auth":
		return runAuth(ctx, args[1:], stdout, stderr)
	case "models":
		return runModels(ctx, args[1:], stdout, stderr)
	case "quota":
		return runQuota(ctx, args[1:], stdout, stderr)
	case "prompt":
		return runPrompt(ctx, args[1:], stdin, stdout, stderr)
	case "agent":
		return runAgent(ctx, args[1:], stdin, stdout, stderr)
	case "swarm":
		return runSwarm(ctx, args[1:], stdout, stderr)
	case "mcp":
		return runMCP(ctx, args[1:], stdout, stderr)
	case "plugin":
		return runPlugin(args[1:], stdout, stderr)
	case "skills":
		return runSkills(args[1:], stdout, stderr)
	case "sessions":
		return runSessions(args[1:], stdout, stderr)
	case "trace-proxy":
		return runTraceProxy(ctx, args[1:], stdout, stderr)
	case "acp":
		return runACP(ctx, args[1:], stdin, stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprintf(w, `Antigravity Go SDK CLI (%s v%s)

Usage:
  %s <command> [flags]

Commands:
  version                               Show CLI and SDK version
  help                                  Show this help message
  auth status  [flags]                  Inspect active auth chain (tokens redacted)
  auth login   [flags]                  Authenticate via PKCE S256 OAuth loopback flow
  auth logout  [flags]                  Remove stored OAuth credentials
  models       [--project ID] [--json]  List available models and per-model quota
  quota        [--project ID] [--json]  Show aggregated user quota summary
  prompt       -p <text> [-m <model>]   Stream a generation request (thoughts + text)
  agent        [-p "<task>"] [-m <model>] [--cwd <dir>] [--mode agent|plan|ask] [--max-steps 25] [--yolo] [-c|--continue] [--conversation <id>] [--plan-model <model>] [--sandbox] [--mcp] [--skills] [--input-format text|stream-json] [--output-format text|json|stream-json] [--json-schema <schema>] [--transcript <file.jsonl>] [--thoughts]
                                        Run local Cortex agent (one-shot, stream-json, or interactive REPL)
  swarm inspect <dir> [--tail N] [--json]
                                        Inspect a swarm board.jsonl + members.json directory
  swarm run    -p "<goal>" [-m <model>] [--cwd <dir>] [--swarm-dir <dir>] [--title <title>] [--json]
                                        Run a local multi-agent SwarmRun coordinator & workers
  mcp          <list|add|remove|enable|disable|tools> [flags]
                                        Manage MCP servers and inspect exposed tools
  plugin       <list|validate> [path] [--strict] [--plugins-dir <dir>] [--json]
                                        List installed plugins or validate plugin manifest/skills/mcp
  skills       [--cwd <dir>] [--skills-dir <dir>] [--json]
                                        Discover SKILL.md skills across builtin, user, and workspace dirs
  sessions     <list|show> [id] [--cwd <dir>] [--data-dir <dir>] [--json]
                                        List or inspect persisted conversation sessions
  trace-proxy  [--listen 127.0.0.1:8899] [--upstream https://daily-cloudcode-pa.googleapis.com] [--out trace.jsonl]
                                        Start an SSE-capable HTTP/JSONL trace proxy
  acp          [--stdio] [-m <model>] [--cwd <dir>] [--disable-tools] [--yolo]
                                        Run ACP JSON-RPC 2.0 server over stdio

Common Auth Flags:
  --data-dir <dir>       Override token storage directory
  --app-data-dir <name>  Override app directory name (default: antigravity-cli)
  --file                 Force file token storage (bypass OS keyring)
`, binaryName, aicode.DefaultClientVersion, binaryName)
}

func runAuth(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: agy-sdk auth <status|login|logout> [flags]")
		return 2
	}
	switch args[0] {
	case "status":
		return runAuthStatus(args[1:], stdout, stderr)
	case "login":
		return runAuthLogin(ctx, args[1:], stdout, stderr, nil, openBrowser)
	case "logout":
		return runAuthLogout(args[1:], stdout, stderr)
	case "-h", "--help", "help":
		_, _ = fmt.Fprintln(stdout, "usage: agy-sdk auth <status|login|logout> [flags]")
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "unknown auth subcommand %q\n", args[0])
		return 2
	}
}

func runAuthStatus(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("auth status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var af authFlags
	af.register(fs)
	var jsonOut bool
	fs.BoolVar(&jsonOut, "json", false, "output JSON")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}

	ca := af.newChainedAuth()
	status := ca.InspectStatus()
	if gwKey := os.Getenv("AGY_LLM_GATEWAY_API_KEY"); gwKey != "" {
		status["gateway_api_key"] = auth.MaskSecret(gwKey)
	}

	if jsonOut {
		return writeJSON(stdout, stderr, status)
	}

	keys := make([]string, 0, len(status))
	for k := range status {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		_, _ = fmt.Fprintf(stdout, "%-24s %v\n", k+":", status[k])
	}
	return 0
}

func runAuthLogin(
	ctx context.Context,
	args []string,
	stdout, stderr io.Writer,
	oauthOverride *auth.OAuthConfig,
	browserFn func(string) error,
) int {
	fs := flag.NewFlagSet("auth login", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var af authFlags
	af.register(fs)
	var enterprise bool
	var noBrowser bool
	var timeout time.Duration
	fs.BoolVar(&enterprise, "enterprise", false, "use enterprise/business OAuth client configuration")
	fs.BoolVar(&noBrowser, "no-browser", false, "do not launch browser automatically; print URL only")
	fs.DurationVar(&timeout, "timeout", 5*time.Minute, "timeout waiting for OAuth callback")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}

	cfg := auth.NewConsumerOAuthConfig()
	authMethod := string(auth.AuthKindConsumerOAuth)
	if enterprise {
		cfg = auth.NewBusinessOAuthConfig()
		authMethod = string(auth.AuthKindBusinessOAuth)
	}
	if oauthOverride != nil {
		cfg = *oauthOverride
	}

	cb, err := auth.StartLoopbackCallback()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "failed to start loopback callback server: %v\n", err)
		return 1
	}
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = cb.Close(shutCtx)
	}()

	authURL, err := cfg.BuildPKCEAuthURL(cb.RedirectURL, cb.State, cb.Verifier)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "failed to build PKCE auth URL: %v\n", err)
		return 1
	}

	_, _ = fmt.Fprintf(stdout, "Open this URL in your browser to authenticate:\n%s\n", authURL)
	if !noBrowser && browserFn != nil {
		if err := browserFn(authURL); err != nil {
			_, _ = fmt.Fprintf(stderr, "warning: could not launch browser automatically (%v); open the URL above manually\n", err)
		}
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var res auth.LoopbackResult
	select {
	case res = <-cb.ResultCh:
	case <-waitCtx.Done():
		_, _ = fmt.Fprintf(stderr, "authentication cancelled or timed out: %v\n", waitCtx.Err())
		return 1
	}
	if res.Err != nil {
		_, _ = fmt.Fprintf(stderr, "authentication callback error: %v\n", res.Err)
		return 1
	}

	ca := af.newChainedAuth()
	tok, err := cfg.ExchangeCode(waitCtx, ca.HTTPClient, res.Code, cb.RedirectURL, cb.Verifier)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "failed to exchange authorization code: %v\n", err)
		return 1
	}
	if err := ca.Storage.SaveToken(tok, authMethod); err != nil {
		_, _ = fmt.Fprintf(stderr, "failed to store token: %v\n", err)
		return 1
	}

	stored := &auth.StoredToken{Token: tok, AuthMethod: authMethod}
	stored.RememberIDToken()
	backend, _ := ca.Storage.ActiveBackendName()
	if stored.IDTokenEmail != "" {
		_, _ = fmt.Fprintf(stdout, "Logged in as %s (%s, backend=%s, token=%s)\n",
			stored.IDTokenEmail, authMethod, backend, auth.MaskSecret(tok.AccessToken))
	} else {
		_, _ = fmt.Fprintf(stdout, "Logged in (%s, backend=%s, token=%s)\n",
			authMethod, backend, auth.MaskSecret(tok.AccessToken))
	}
	return 0
}

func runAuthLogout(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("auth logout", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var af authFlags
	af.register(fs)
	if err := parseFlags(fs, args); err != nil {
		return 2
	}

	ca := af.newChainedAuth()
	if err := ca.Storage.RemoveToken(); err != nil {
		_, _ = fmt.Fprintf(stderr, "failed to remove stored token: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintln(stdout, "Logged out: stored credentials removed.")
	return 0
}

func runModels(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("models", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var af authFlags
	af.register(fs)
	var project string
	var jsonOut bool
	fs.StringVar(&project, "project", "", "override CloudCode project ID")
	fs.BoolVar(&jsonOut, "json", false, "output JSON")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}

	client := aicode.NewClient(af.newChainedAuth())
	resp, err := client.FetchAvailableModels(ctx, project)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "fetch models failed: %v\n", err)
		return 1
	}

	if jsonOut {
		return writeJSON(stdout, stderr, resp)
	}

	if resp.DefaultAgentModelID != "" {
		_, _ = fmt.Fprintf(stdout, "Default model: %s\n", resp.DefaultAgentModelID)
	}
	ids := make([]string, 0, len(resp.Models))
	for id := range resp.Models {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		m := resp.Models[id]
		if m == nil {
			continue
		}
		var flags []string
		if m.Recommended {
			flags = append(flags, "recommended")
		}
		if m.Disabled {
			flags = append(flags, "disabled")
		}
		if m.SupportsThinking {
			flags = append(flags, "thinking")
		}
		quotaStr := "-"
		if m.QuotaInfo != nil {
			quotaStr = fmt.Sprintf("%.0f%%", m.QuotaInfo.RemainingFraction*100)
			if m.QuotaInfo.ResetTime != "" {
				quotaStr += " resets " + m.QuotaInfo.ResetTime
			}
		}
		name := m.DisplayName
		if name == "" {
			name = id
		}
		_, _ = fmt.Fprintf(stdout, "%-32s %-32s quota=%-28s [%s]\n",
			id, name, quotaStr, strings.Join(flags, ","))
	}
	return 0
}

func runQuota(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("quota", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var af authFlags
	af.register(fs)
	var project string
	var jsonOut bool
	fs.StringVar(&project, "project", "", "override CloudCode project ID")
	fs.BoolVar(&jsonOut, "json", false, "output JSON")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}

	client := aicode.NewClient(af.newChainedAuth())
	resp, err := client.RetrieveUserQuotaSummary(ctx, project)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "retrieve quota failed: %v\n", err)
		return 1
	}

	if jsonOut {
		return writeJSON(stdout, stderr, resp)
	}

	if resp.Description != "" {
		_, _ = fmt.Fprintf(stdout, "%s\n", resp.Description)
	}
	for _, g := range resp.Groups {
		if g == nil {
			continue
		}
		_, _ = fmt.Fprintf(stdout, "Group: %s", g.DisplayName)
		if g.Description != "" {
			_, _ = fmt.Fprintf(stdout, " (%s)", g.Description)
		}
		_, _ = fmt.Fprintln(stdout)
		for _, b := range g.Buckets {
			printQuotaBucket(stdout, "  ", b)
		}
	}
	if len(resp.Buckets) > 0 {
		_, _ = fmt.Fprintln(stdout, "Buckets:")
		for _, b := range resp.Buckets {
			printQuotaBucket(stdout, "  ", b)
		}
	}
	return 0
}

func printQuotaBucket(w io.Writer, indent string, b *aicode.QuotaSummaryBucket) {
	if b == nil {
		return
	}
	label := b.DisplayName
	if label == "" {
		label = b.BucketID
	}
	status := fmt.Sprintf("%.0f%% remaining", b.RemainingFraction*100)
	if b.RemainingAmount > 0 {
		status += fmt.Sprintf(" (amount=%d)", b.RemainingAmount)
	}
	if b.Window != "" {
		status += " window=" + b.Window
	}
	if b.ResetTime != "" {
		status += " resets=" + b.ResetTime
	}
	if b.Disabled {
		status += " [disabled]"
	}
	_, _ = fmt.Fprintf(w, "%s%-28s %s\n", indent, label, status)
}

func runPrompt(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("prompt", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var af authFlags
	af.register(fs)
	var promptText string
	var model string
	var includeThoughts bool
	fs.StringVar(&promptText, "p", "", "prompt text to send to the model")
	fs.StringVar(&model, "m", "gemini-2.5-flash", "model ID")
	fs.BoolVar(&includeThoughts, "thoughts", true, "request thinking/thought chunks when supported")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}

	if promptText == "" && fs.NArg() > 0 {
		promptText = strings.Join(fs.Args(), " ")
	}
	if promptText == "" && stdin != nil {
		if info, err := os.Stdin.Stat(); err == nil && (info.Mode()&os.ModeCharDevice) == 0 {
			if raw, err := io.ReadAll(io.LimitReader(stdin, 1<<20)); err == nil {
				promptText = strings.TrimSpace(string(raw))
			}
		}
	}
	if promptText == "" {
		_, _ = fmt.Fprintln(stderr, "prompt text is required (-p \"<text>\")")
		return 2
	}

	req := &aicode.GeminiRequest{
		Contents: []*aicode.Content{
			{
				Role:  "user",
				Parts: []*aicode.Part{{Text: promptText}},
			},
		},
	}
	if includeThoughts {
		req.GenerationConfig = &aicode.GenerationConfig{
			ThinkingConfig: &aicode.ThinkingConfig{IncludeThoughts: true},
		}
	}

	client := aicode.NewClient(af.newChainedAuth())
	inThought := false
	wroteText := false
	lastNewline := true

	err := client.StreamGenerateContent(ctx, model, req, func(ch *aicode.SSEChunk) error {
		if ch == nil || ch.Response == nil {
			return nil
		}
		for _, cand := range ch.Response.Candidates {
			if cand == nil || cand.Content == nil {
				continue
			}
			for _, p := range cand.Content.Parts {
				if p == nil || p.Text == "" {
					continue
				}
				if p.Thought {
					if !inThought {
						if wroteText && !lastNewline {
							_, _ = fmt.Fprintln(stdout)
						}
						_, _ = fmt.Fprintln(stdout, "[thought]")
						inThought = true
					}
					_, _ = fmt.Fprint(stdout, p.Text)
					lastNewline = strings.HasSuffix(p.Text, "\n")
					wroteText = true
				} else {
					if inThought {
						if !lastNewline {
							_, _ = fmt.Fprintln(stdout)
						}
						_, _ = fmt.Fprintln(stdout, "[/thought]")
						inThought = false
					}
					_, _ = fmt.Fprint(stdout, p.Text)
					lastNewline = strings.HasSuffix(p.Text, "\n")
					wroteText = true
				}
			}
		}
		return nil
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "\nstream error: %v\n", err)
		return 1
	}
	if inThought {
		if !lastNewline {
			_, _ = fmt.Fprintln(stdout)
		}
		_, _ = fmt.Fprintln(stdout, "[/thought]")
		lastNewline = true
	}
	if wroteText && !lastNewline {
		_, _ = fmt.Fprintln(stdout)
	}
	return 0
}

func runAgent(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	for _, a := range args {
		if a == "-h" || a == "--help" || a == "help" {
			printAgentHelp(stdout)
			return 0
		}
		if a == "--" {
			break
		}
	}

	fs := flag.NewFlagSet("agent", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var af authFlags
	af.register(fs)
	var task string
	var model string
	var cwd string
	var modeStr string
	var maxSteps int
	var yolo bool
	var transcriptPath string
	var includeThoughts bool
	var continueSession bool
	var conversationID string
	var planModel string
	var useSandbox bool
	var useMCP bool
	var useSkills bool
	var inputFormat string
	var outputFormat string
	var jsonSchemaSpec string
	fs.StringVar(&task, "p", "", "one-shot task prompt (omit for interactive REPL)")
	fs.StringVar(&model, "m", "gemini-4-hydrogen-medium", "model ID")
	fs.StringVar(&cwd, "cwd", "", "workspace root directory (default: current directory)")
	fs.StringVar(&modeStr, "mode", string(cortex.ModeAgent), "agent mode: agent, plan, or ask")
	fs.IntVar(&maxSteps, "max-steps", cortex.DefaultMaxSteps, "maximum tool-execution steps per turn")
	fs.BoolVar(&yolo, "yolo", false, "bypass workspace boundary checks")
	fs.StringVar(&transcriptPath, "transcript", "", "write execution trajectory to JSONL file")
	fs.BoolVar(&includeThoughts, "thoughts", true, "stream model thinking/thought chunks")
	fs.BoolVar(&continueSession, "c", false, "continue latest persisted session for --cwd")
	fs.BoolVar(&continueSession, "continue", false, "continue latest persisted session for --cwd")
	fs.StringVar(&conversationID, "conversation", "", "resume a specific persisted session ID")
	fs.StringVar(&planModel, "plan-model", "", "run dual-phase plan-and-execute using this planner model")
	fs.BoolVar(&useSandbox, "sandbox", false, "run shell commands inside macOS Seatbelt sandbox")
	fs.BoolVar(&useMCP, "mcp", false, "connect enabled MCP servers from mcp_config.json")
	fs.BoolVar(&useSkills, "skills", false, "inject discovered skills and workspace/user rules")
	fs.StringVar(&inputFormat, "input-format", "text", "input format: text or stream-json")
	fs.StringVar(&outputFormat, "output-format", "text", "output format: text, json, or stream-json")
	fs.StringVar(&jsonSchemaSpec, "json-schema", "", "validate final response against inline JSON schema or file path")
	if err := parseFlags(fs, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	mode, ok := parseAgentMode(modeStr)
	if !ok {
		_, _ = fmt.Fprintf(stderr, "invalid --mode %q: must be agent, plan, or ask\n", modeStr)
		return 2
	}

	switch inputFormat {
	case "", "text", "stream-json":
	default:
		_, _ = fmt.Fprintf(stderr, "invalid --input-format %q: must be text or stream-json\n", inputFormat)
		return 2
	}

	usePrintMode := outputFormat != "" && outputFormat != string(printmode.FormatText)
	if inputFormat == "stream-json" && outputFormat == "" {
		outputFormat = string(printmode.FormatStreamJSON)
		usePrintMode = true
	}

	var emitter printmode.Emitter
	if usePrintMode {
		var err error
		emitter, err = printmode.NewEmitter(printmode.OutputFormat(outputFormat), stdout, stderr)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "invalid --output-format %q: %v\n", outputFormat, err)
			return 2
		}
	}

	schemaBytes, err := loadJSONSchemaBytes(jsonSchemaSpec)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%v\n", err)
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

	var traj *cortex.TrajectoryRecorder
	if transcriptPath != "" {
		var err error
		traj, err = cortex.NewTrajectoryRecorder(transcriptPath)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "failed to open transcript %q: %v\n", transcriptPath, err)
			return 1
		}
		defer func() { _ = traj.Close() }()
	}

	var sessStore *sessionstore.Store
	var sessRec *sessionstore.SessionRecord
	var history []*aicode.Content
	if continueSession || conversationID != "" || af.dataDir != "" {
		sessStore = sessionstore.NewStore(sessionsDirPath(af.dataDir))
		switch {
		case conversationID != "":
			var err error
			sessRec, err = sessStore.Load(conversationID)
			if err != nil {
				_, _ = fmt.Fprintf(stderr, "failed to load session %q: %v\n", conversationID, err)
				return 1
			}
		case continueSession:
			var err error
			sessRec, err = sessStore.Latest(cwd)
			if err != nil {
				if errors.Is(err, sessionstore.ErrSessionNotFound) {
					sessRec = sessStore.NewSession(cwd, model, mode)
				} else {
					_, _ = fmt.Fprintf(stderr, "failed to find latest session: %v\n", err)
					return 1
				}
			}
		default:
			sessRec = sessStore.NewSession(cwd, model, mode)
		}
		history = append(history, sessRec.History...)
	}

	guard := cortex.NewPermissionGuard(cwd, mode)
	guard.DangerouslySkipPermissions = yolo

	reg := cortex.NewDefaultRegistry()
	if useSandbox {
		reg.Register(sandbox.NewSandboxedRunCommandTool(nil, false))
	}
	if useMCP {
		closers := registerConfiguredMCPTools(ctx, reg, af.dataDir, stderr)
		defer func() {
			for _, c := range closers {
				_ = c.Close()
			}
		}()
	}

	var extraPrompt string
	if useSkills {
		extraPrompt = buildSkillsAndRulesPrompt(cwd, af.dataDir, stderr)
	}

	var genCfg *aicode.GenerationConfig
	if includeThoughts {
		genCfg = &aicode.GenerationConfig{
			ThinkingConfig: &aicode.ThinkingConfig{IncludeThoughts: true},
		}
	}

	ca := af.newChainedAuth()
	client := aicode.NewClient(ca)

	var onEvent cortex.EventCallback
	var flush func()
	if emitter != nil {
		onEvent = func(ev cortex.Event) {
			_ = emitter.EmitEvent(ev)
		}
		flush = func() {}
	} else {
		onEvent, flush = newAgentEventRenderer(stdout, includeThoughts)
	}

	engine := &cortex.Engine{
		Streamer:         cortex.NewClientStreamer(client),
		Model:            model,
		Registry:         reg,
		Guard:            guard,
		SystemPrompt:     cortex.BuildSystemPrompt(cwd, mode, extraPrompt),
		Trajectory:       traj,
		GenerationConfig: genCfg,
		MaxSteps:         maxSteps,
		OnEvent:          onEvent,
	}

	sessionID := ""
	if sessRec != nil {
		sessionID = sessRec.ID
	}
	initEmitted := false
	runOneTurn := func(prompt string) int {
		if emitter != nil && !initEmitted {
			initEmitted = true
			if err := emitter.EmitInit(printmode.InitPayload{
				SessionID: sessionID,
				Model:     engine.Model,
				WorkDir:   cwd,
			}); err != nil {
				_, _ = fmt.Fprintf(stderr, "emit init failed: %v\n", err)
				return 1
			}
		}

		nextHistory, summary, err := executeAgentTurn(ctx, engine, planModel, cwd, extraPrompt, history, prompt)
		flush()
		if err == nil && len(schemaBytes) > 0 {
			var finalText string
			if summary != nil {
				finalText = summary.FinalText
			}
			if schemaErr := printmode.ValidateJSONSchema(schemaBytes, []byte(finalText)); schemaErr != nil {
				err = fmt.Errorf("json-schema validation failed: %w", schemaErr)
			}
		}
		if err == nil {
			history = nextHistory
			if sessStore != nil && sessRec != nil {
				sessRec.History = history
				sessRec.Model = engine.Model
				sessRec.Mode = guard.Mode
				if saveErr := sessStore.Save(sessRec); saveErr != nil {
					_, _ = fmt.Fprintf(stderr, "warning: failed to save session: %v\n", saveErr)
				}
			}
		}
		if emitter != nil {
			_ = emitter.EmitResult(summary, err)
			if err != nil {
				return 1
			}
			return 0
		}
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "agent error: %v\n", err)
			return 1
		}
		return 0
	}

	if inputFormat == "stream-json" {
		msgCh, errCh := printmode.ReadStreamInput(stdin)
		for msg := range msgCh {
			if msg.Message == nil {
				continue
			}
			prompt, err := msg.Message.PromptText()
			if err != nil {
				_, _ = fmt.Fprintf(stderr, "stream-json input error: %v\n", err)
				return 1
			}
			if strings.TrimSpace(prompt) == "" {
				continue
			}
			if code := runOneTurn(prompt); code != 0 {
				return code
			}
		}
		if err := <-errCh; err != nil {
			_, _ = fmt.Fprintf(stderr, "stream-json read error: %v\n", err)
			return 1
		}
		return 0
	}

	if task != "" {
		return runOneTurn(task)
	}

	_, _ = fmt.Fprintf(stdout, "Antigravity Agent REPL (model=%s, mode=%s, cwd=%s)\n", engine.Model, guard.Mode, cwd)
	_, _ = fmt.Fprintln(stdout, "Type /help for commands, /exit to quit.")

	if stdin == nil {
		return 0
	}

	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for {
		_, _ = fmt.Fprint(stdout, "> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") {
			fields := strings.Fields(line)
			switch fields[0] {
			case "/exit", "/quit":
				_, _ = fmt.Fprintln(stdout, "Bye.")
				return 0
			case "/help":
				_, _ = fmt.Fprintln(stdout, "REPL commands: /help, /model <id>, /mode <agent|plan|ask>, /clear, /exit")
			case "/model":
				if len(fields) < 2 {
					_, _ = fmt.Fprintf(stdout, "Current model: %s\n", engine.Model)
				} else {
					engine.Model = fields[1]
					_, _ = fmt.Fprintf(stdout, "Model set to %s\n", engine.Model)
				}
			case "/mode":
				if len(fields) < 2 {
					_, _ = fmt.Fprintf(stdout, "Current mode: %s\n", guard.Mode)
				} else if nextMode, valid := parseAgentMode(fields[1]); !valid {
					_, _ = fmt.Fprintf(stderr, "invalid mode %q: must be agent, plan, or ask\n", fields[1])
				} else {
					guard.Mode = nextMode
					engine.SystemPrompt = cortex.BuildSystemPrompt(cwd, nextMode, extraPrompt)
					_, _ = fmt.Fprintf(stdout, "Mode set to %s\n", nextMode)
				}
			case "/clear":
				history = nil
				_, _ = fmt.Fprintln(stdout, "Conversation history cleared.")
			default:
				_, _ = fmt.Fprintf(stderr, "unknown command %q (type /help)\n", fields[0])
			}
			continue
		}

		if code := runOneTurn(line); code != 0 && ctx.Err() != nil {
			return 1
		}
	}
	if err := scanner.Err(); err != nil {
		_, _ = fmt.Fprintf(stderr, "read stdin error: %v\n", err)
		return 1
	}
	return 0
}

func parseAgentMode(raw string) (cortex.Mode, bool) {
	switch cortex.Mode(strings.ToLower(strings.TrimSpace(raw))) {
	case cortex.ModeAgent:
		return cortex.ModeAgent, true
	case cortex.ModePlan:
		return cortex.ModePlan, true
	case cortex.ModeAsk:
		return cortex.ModeAsk, true
	default:
		return "", false
	}
}

func printAgentHelp(w io.Writer) {
	_, _ = fmt.Fprintf(w, `Usage:
  %s agent [-p "<task>"] [flags]

Modes:
  One-shot:  Provide -p "<task>" to run an autonomous multi-turn tool loop and exit.
  REPL:      Omit -p to start an interactive session (/help, /model, /mode, /clear, /exit).

Flags:
  -p <task>              One-shot task prompt
  -m <model>             Model ID (default: gemini-4-hydrogen-medium)
  --cwd <dir>            Workspace root directory (default: current directory)
  --mode <mode>          Execution mode: agent | plan | ask (default: agent)
  --max-steps <N>        Maximum tool steps per turn (default: %d)
  --yolo                 Bypass workspace boundary checks
  -c, --continue         Resume latest persisted session for --cwd
  --conversation <id>    Resume a specific persisted session ID
  --plan-model <model>   Run dual-phase plan-and-execute with planner model
  --sandbox              Execute run_command inside macOS Seatbelt sandbox
  --mcp                  Connect enabled MCP servers from mcp_config.json
  --skills               Inject discovered SKILL.md skills and workspace/user rules
  --input-format <fmt>   Input format: text | stream-json (default: text)
  --output-format <fmt>  Output format: text | json | stream-json (default: text)
  --json-schema <spec>   Validate final response against JSON schema (inline or file)
  --transcript <file>    Append execution trajectory as JSONL
  --thoughts             Stream thinking chunks (default: true)
  --data-dir <dir>       Override data/session/token storage directory
  --app-data-dir <name>  Override app directory name (default: antigravity-cli)
  --file                 Force file token storage (bypass OS keyring)
`, binaryName, cortex.DefaultMaxSteps)
}

func newAgentEventRenderer(stdout io.Writer, includeThoughts bool) (func(cortex.Event), func()) {
	inThought := false
	wroteText := false
	lastNewline := true

	closeThought := func() {
		if inThought {
			if !lastNewline {
				_, _ = fmt.Fprintln(stdout)
			}
			_, _ = fmt.Fprintln(stdout, "[/thought]")
			inThought = false
			lastNewline = true
		}
	}

	onEvent := func(ev cortex.Event) {
		switch ev.Type {
		case cortex.EventThought:
			if !includeThoughts || ev.Text == "" {
				return
			}
			if !inThought {
				if wroteText && !lastNewline {
					_, _ = fmt.Fprintln(stdout)
				}
				_, _ = fmt.Fprintln(stdout, "[thought]")
				inThought = true
			}
			_, _ = fmt.Fprint(stdout, ev.Text)
			lastNewline = strings.HasSuffix(ev.Text, "\n")
			wroteText = true
		case cortex.EventText:
			if ev.Text == "" {
				return
			}
			closeThought()
			_, _ = fmt.Fprint(stdout, ev.Text)
			lastNewline = strings.HasSuffix(ev.Text, "\n")
			wroteText = true
		case cortex.EventToolStart:
			if ev.ToolCall == nil {
				return
			}
			closeThought()
			if wroteText && !lastNewline {
				_, _ = fmt.Fprintln(stdout)
			}
			_, _ = fmt.Fprintf(stdout, "[tool] %s...\n", ev.ToolCall.Name)
			lastNewline = true
			wroteText = true
		case cortex.EventToolDone:
			if ev.ToolCall == nil {
				return
			}
			if ev.Err != nil {
				_, _ = fmt.Fprintf(stdout, "[tool] %s failed: %v\n", ev.ToolCall.Name, ev.Err)
			} else {
				_, _ = fmt.Fprintf(stdout, "[tool] %s done\n", ev.ToolCall.Name)
			}
			lastNewline = true
			wroteText = true
		}
	}

	flush := func() {
		closeThought()
		if wroteText && !lastNewline {
			_, _ = fmt.Fprintln(stdout)
			lastNewline = true
		}
		inThought = false
		wroteText = false
	}

	return onEvent, flush
}

func runSwarm(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: agy-sdk swarm <inspect|run> [flags]")
		return 2
	}
	switch args[0] {
	case "inspect":
		return runSwarmInspect(args[1:], stdout, stderr)
	case "run":
		return runSwarmRun(ctx, args[1:], stdout, stderr)
	case "-h", "--help", "help":
		_, _ = fmt.Fprintln(stdout, "usage:\n  agy-sdk swarm inspect <swarm-dir> [--tail N] [--json]\n  agy-sdk swarm run -p \"<goal>\" [-m <model>] [--cwd <dir>] [--swarm-dir <dir>] [--title <title>] [--json]")
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "unknown swarm subcommand %q\n", args[0])
		return 2
	}
}

func runSwarmInspect(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("swarm inspect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var tail int
	var jsonOut bool
	fs.IntVar(&tail, "tail", -1, "show only the last N messages (-1 for all)")
	fs.BoolVar(&jsonOut, "json", false, "output JSON")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		_, _ = fmt.Fprintln(stderr, "usage: agy-sdk swarm inspect <swarm-dir> [--tail N] [--json]")
		return 2
	}
	dir := fs.Arg(0)

	snap, err := swarm.InspectDir(dir)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "inspect swarm failed: %v\n", err)
		return 1
	}
	if tail >= 0 && tail < len(snap.Messages) {
		snap.Messages = snap.Messages[len(snap.Messages)-tail:]
	}

	if jsonOut {
		return writeJSON(stdout, stderr, snap)
	}

	title := ""
	if snap.Roster != nil {
		title = snap.Roster.Title
	}
	_, _ = fmt.Fprintf(stdout, "Swarm:    %s\n", title)
	_, _ = fmt.Fprintf(stdout, "Dir:      %s\n", snap.Dir)
	_, _ = fmt.Fprintf(stdout, "HeadSeq:  %d\n", snap.HeadSeq)
	_, _ = fmt.Fprintf(stdout, "Members:  %d live, %d killed\n", snap.LiveCount, snap.KilledCount)

	if snap.Roster != nil {
		for _, name := range snap.Roster.SortedNames() {
			m := snap.Roster.Members[name]
			if m == nil {
				continue
			}
			state := string(m.Phase)
			if m.IsKilled() {
				state = string(swarm.PhaseKilled)
			}
			_, _ = fmt.Fprintf(stdout, "  - %-18s role=%-12s state=%-8s model=%-28s wake=%s\n",
				name, m.Role, state, m.EffectiveModel(), m.WakeMode)
		}
	}

	_, _ = fmt.Fprintf(stdout, "Messages (%d shown):\n", len(snap.Messages))
	for _, msg := range snap.Messages {
		kind := string(msg.Kind)
		if kind == "" {
			kind = "chat"
		}
		text := strings.ReplaceAll(strings.TrimSpace(msg.Text), "\n", " ")
		if len(text) > 160 {
			text = text[:157] + "..."
		}
		_, _ = fmt.Fprintf(stdout, "  #%d [%s] %s (%s): %s\n", msg.Seq, msg.Time, msg.Agent, kind, text)
	}
	return 0
}

func runACP(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("acp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var af authFlags
	af.register(fs)
	var stdio bool
	var defaultModel string
	var cwd string
	var disableTools bool
	var yolo bool
	fs.BoolVar(&stdio, "stdio", true, "serve ACP JSON-RPC 2.0 over stdin/stdout")
	fs.StringVar(&defaultModel, "m", "", "default model ID for new ACP sessions")
	fs.StringVar(&cwd, "cwd", "", "default workspace directory for new ACP sessions")
	fs.BoolVar(&disableTools, "disable-tools", false, "disable built-in Cortex tools (single-turn streaming only)")
	fs.BoolVar(&yolo, "yolo", false, "bypass workspace boundary checks for tools")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if !stdio {
		_, _ = fmt.Fprintln(stderr, "only --stdio transport is supported")
		return 2
	}

	var reg *cortex.Registry
	if !disableTools {
		reg = cortex.NewDefaultRegistry()
	}

	ca := af.newChainedAuth()
	client := aicode.NewClient(ca)
	srv := acp.NewServer(acp.ServerConfig{
		Version:                    aicode.DefaultClientVersion,
		DefaultModel:               defaultModel,
		DefaultCwd:                 cwd,
		Auth:                       ca,
		Streamer:                   acp.AICodeStreamer{Client: client},
		ToolRegistry:               reg,
		DangerouslySkipPermissions: yolo,
	})
	if err := srv.Serve(ctx, stdin, stdout); err != nil && !errors.Is(err, context.Canceled) {
		_, _ = fmt.Fprintf(stderr, "acp server error: %v\n", err)
		return 1
	}
	return 0
}

// parseFlags parses flags even when positional arguments precede flags
// (e.g. `swarm inspect <dir> --tail 3`), by moving flag tokens before positional args.
func parseFlags(fs *flag.FlagSet, args []string) error {
	var flagArgs []string
	var posArgs []string

	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			posArgs = append(posArgs, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			flagArgs = append(flagArgs, a)
			if !strings.Contains(a, "=") {
				name := strings.TrimLeft(a, "-")
				if f := fs.Lookup(name); f != nil {
					if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); !ok || !bf.IsBoolFlag() {
						if i+1 < len(args) {
							i++
							flagArgs = append(flagArgs, args[i])
						}
					}
				}
			}
		} else {
			posArgs = append(posArgs, a)
		}
	}
	return fs.Parse(append(flagArgs, posArgs...))
}

func writeJSON(stdout, stderr io.Writer, v any) int {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		_, _ = fmt.Fprintf(stderr, "json encode error: %v\n", err)
		return 1
	}
	return 0
}

func openBrowser(rawURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	return cmd.Start()
}

// Ensure http import is used cleanly when needed by tests/overrides.
var _ = (*http.Client)(nil)
