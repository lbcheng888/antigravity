// Reconstructed from agy v1.5.10 binary evidence (disassembly + Go type table).
// Binary VA: 0x102fac4a0 | Size: 20 funcs (first func (*stringSliceFlag).String, 128 B) | Source: third_party/jetski/cli/acp/options.go:L102
package acp

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"

	"google3/base/go/flag"
	"google3/base/go/log"
	"google3/third_party/jetski/cli/acp/router"
	"google3/third_party/jetski/cli/acp/server"
	"google3/third_party/jetski/cli/analytics"
	"google3/third_party/jetski/codeium_common_pb"
	"google3/third_party/jetski/language_server"
)

// Options holds CLI flags and runtime I/O for `agy acp`. go_type 0x10aa06810, size 296.
type Options struct {
	WorkingDir                 string    // +0x0000
	IsJetski                   bool      // +0x0010
	IsGoogleEnvironment        bool      // +0x0011
	IsGoogleEnvironmentSet     bool      // +0x0012
	Version                    string    // +0x0018
	AppDataDir                 string    // +0x0028
	GeminiDir                  string    // +0x0038
	CLIDirName                 string    // +0x0048
	CloudCodeServerURL         string    // +0x0058
	UseStubbyAuth              bool      // +0x0068
	DisableTelemetry           bool      // +0x0069
	UserAgentName              string    // +0x0070
	Stdio                      bool      // +0x0080
	ProcessPerSession          bool      // +0x0081
	Worker                     bool      // +0x0082
	Agent                      string    // +0x0088
	IDEMCPPort                 int       // +0x0098
	AddDirs                    []string  // +0x00a0
	EnableTerminalSandbox      bool      // +0x00b8
	DangerouslySkipPermissions bool      // +0x00b9
	IncludeDefaultCustomizations *bool   // +0x00c0
	LogFile                    string    // +0x00c8
	Login                      bool      // +0x00d8
	WorkerPreArgs              []string  // +0x00e0
	Stdin                      io.Reader // +0x00f8
	Stdout                     io.Writer // +0x0108
	Stderr                     io.Writer // +0x0118
}

type stringSliceFlag []string

// Binary VA: 0x102fac4a0 | Size: 128 B | Source: third_party/jetski/cli/acp/options.go:L102
func (f *stringSliceFlag) String() string {
	return fmt.Sprintf("%v", []string(*f)) // L103
}

// Binary VA: 0x102fac520 | Size: 240 B | Source: third_party/jetski/cli/acp/options.go:L106
func (f *stringSliceFlag) Set(v string) error {
	*f = append(*f, v) // L107
	return nil
}

// Binary VA: 0x102fac610 | Size: 96 B | Source: third_party/jetski/cli/acp/options.go:L111
func (f *stringSliceFlag).Get() any {
	return []string(*f) // L112
}

// boolPtrFlag writes into a **bool so callers distinguish unset vs explicit false. go_type 0x10a2fbcc0.
type boolPtrFlag struct {
	target     **bool // +0x00
	defaultVal bool   // +0x08
}

// Binary VA: 0x102fac670 | Size: 16 B | Source: third_party/jetski/cli/acp/options.go:L120
func (f *boolPtrFlag) IsBoolFlag() bool { return true }

// Binary VA: 0x102fac680 | Size: 128 B | Source: third_party/jetski/cli/acp/options.go:L122
func (f *boolPtrFlag) String() string {
	if f.target != nil && *f.target != nil {
		return strconv.FormatBool(**f.target) // L124/L126
	}
	return strconv.FormatBool(f.defaultVal)
}

// Binary VA: 0x102fac700 | Size: 176 B | Source: third_party/jetski/cli/acp/options.go:L129
func (f *boolPtrFlag) Set(s string) error {
	v, err := strconv.ParseBool(s) // L130
	if err != nil {
		return err
	}
	*f.target = &v
	return nil
}

// Binary VA: 0x102fac7b0 | Size: 80 B | Source: third_party/jetski/cli/acp/options.go:L138
func (f *boolPtrFlag) Get() any {
	if f.target != nil && *f.target != nil { // L140
		return **f.target
	}
	return f.defaultVal // L142
}

// trackedBoolFlag records whether the user explicitly passed the flag. go_type 0x10a457478.
type trackedBoolFlag struct {
	target     *bool // +0x00
	set        *bool // +0x08
	defaultVal bool  // +0x10
}

// Binary VA: 0x102fac800 | Size: 16 B | Source: third_party/jetski/cli/acp/options.go:L151
func (f *trackedBoolFlag) IsBoolFlag() bool { return true }

// Binary VA: 0x102fac810 | Size: 128 B | Source: third_party/jetski/cli/acp/options.go:L153
func (f *trackedBoolFlag) String() string {
	if f.target != nil {
		return strconv.FormatBool(*f.target)
	}
	return strconv.FormatBool(f.defaultVal)
}

// Binary VA: 0x102fac890 | Size: 144 B | Source: third_party/jetski/cli/acp/options.go:L160
func (f *trackedBoolFlag) Set(s string) error {
	v, err := strconv.ParseBool(s) // L161
	if err != nil {
		return err
	}
	*f.target = v
	*f.set = true // L167
	return nil
}

// Binary VA: 0x102fac920 | Size: 80 B | Source: third_party/jetski/cli/acp/options.go:L172
func (f *trackedBoolFlag) Get() any {
	if f.target != nil { // L174
		return *f.target
	}
	return f.defaultVal // L176
}

// extractPreSubcommandGoogleEnvFlag scans pre-subcommand args before "acp" for
// --is_google3_workspace or --is_google_internal.
// Binary VA: 0x102fac970 | Size: 576 B | Source: third_party/jetski/cli/acp/options.go:L183
func extractPreSubcommandGoogleEnvFlag(args []string) (val bool, ok bool) {
	for _, arg := range args { // L184
		if !strings.HasPrefix(arg, "--") { // L185
			if arg == "acp" { // L185
				break
			}
			continue
		}
		trimmed := strings.TrimLeft(arg, "-") // L188
		name, rawVal, hasEq := strings.Cut(trimmed, "=")
		if name != "is_google3_workspace" && name != "is_google_internal" { // L193
			continue
		}
		if !hasEq { // L196
			val, ok = true, true
			continue
		}
		if parsed, err := strconv.ParseBool(rawVal); err == nil { // L200
			val, ok = parsed, true
		}
	}
	return val, ok
}

// RegisterFlags registers all `agy acp` flags onto fs and seeds Options defaults.
// Binary VA: 0x102facbb0 | Size: 1904 B | Source: third_party/jetski/cli/acp/options.go:L209
func RegisterFlags(fs *flag.FlagSet, opts *Options) {
	// L213/L215: check flag.CommandLine.Visit for pre-parsed flags (RegisterFlags.func1 @0x102faee70)
	if v, ok := extractPreSubcommandGoogleEnvFlag(os.Args[1:]); ok { // L220-221
		opts.IsGoogleEnvironment = v
		opts.IsGoogleEnvironmentSet = true
	}

	fs.BoolVar(&opts.Stdio, "stdio", true, "Use stdio JSON-RPC transport (default)")
	fs.BoolVar(&opts.ProcessPerSession, "process-per-session", false, "Serve each session from an isolated worker process")
	fs.BoolVar(&opts.Worker, "worker", false, "Serve a single session for an ACP front process (internal)")
	fs.StringVar(&opts.Agent, "agent", "", "Agent for the ACP session")
	fs.IntVar(&opts.IDEMCPPort, "ide-mcp-port", 0, "Port for host IDE tools MCP server (requires 'nc' in $PATH)")
	fs.IntVar(&opts.IDEMCPPort, "ide_mcp_port", 0, "Port for host IDE tools MCP server (requires 'nc' in $PATH)")
	fs.Var((*stringSliceFlag)(&opts.AddDirs), "add-dir", "Add a directory to the workspace (repeatable)")
	fs.BoolVar(&opts.EnableTerminalSandbox, "sandbox", false, "Run in a sandbox with terminal restrictions enabled")
	fs.BoolVar(&opts.DangerouslySkipPermissions, "dangerously-skip-permissions", false, "Auto-approve all tool permission requests")
	fs.BoolVar(&opts.Login, "login", false, "Log in with Google interactively, then exit (ACP terminal auth)")
	fs.Var(&boolPtrFlag{target: &opts.IncludeDefaultCustomizations, defaultVal: true}, "include-default-customizations", "Include default customizations") // L242
	fs.StringVar(&opts.LogFile, "log-file", "", "Override ACP log file path")
	fs.StringVar(&opts.LogFile, "cli_log_file", "", "Override ACP log file path")
	fs.StringVar(&opts.GeminiDir, "gemini_dir", ".gemini", "Path where Gemini files are stored") // L247
	fs.StringVar(&opts.GeminiDir, "gemini-dir", ".gemini", "Path where Gemini files are stored")
	fs.StringVar(&opts.AppDataDir, "app_data_dir", "", "Path where application data is stored")
	fs.StringVar(&opts.AppDataDir, "app-data-dir", "", "Path where application data is stored")
	fs.StringVar(&opts.CLIDirName, "cli_dir_name", "", "Subdirectory under app_data_dir for CLI settings and logs")
	fs.StringVar(&opts.CloudCodeServerURL, "cloud_code_endpoint", "", "CCPA API URL")
	fs.BoolVar(&opts.DisableTelemetry, "disable_telemetry", false, "If true, disable telemetry")
	fs.BoolVar(&opts.DisableTelemetry, "disable-telemetry", false, "If true, disable telemetry")
	fs.Var(&trackedBoolFlag{target: &opts.IsGoogleEnvironment, set: &opts.IsGoogleEnvironmentSet}, "is_google3_workspace", "Whether running in Google environment") // L257
	fs.Var(&trackedBoolFlag{target: &opts.IsGoogleEnvironment, set: &opts.IsGoogleEnvironmentSet}, "is_google_internal", "Deprecated: use is_google3_workspace")     // L258
	fs.BoolVar(&opts.UseStubbyAuth, "use_stubby_auth", false, "Use LOAS auth")
	fs.StringVar(&opts.UserAgentName, "override_user_agent_name", "", "Override user agent name for HTTP requests")
}

// buildWorkerArgs builds the argv slice passed to `--worker` child processes.
// Binary VA: 0x102fad320 | Size: 2688 B | Source: third_party/jetski/cli/acp/options.go:L266
func buildWorkerArgs(opts Options) []string {
	args := make([]string, 0, len(opts.WorkerPreArgs)+16) // L267
	// L271: flag.CommandLine.Visit(func(f *flag.Flag) { ... }) (buildWorkerArgs.func1 @0x102faef40)
	args = append(args, opts.WorkerPreArgs...) // L276
	args = append(args, "acp", "--worker")     // L277
	if opts.GeminiDir != "" {
		args = append(args, "--gemini_dir="+opts.GeminiDir) // L279
	}
	if opts.AppDataDir != "" {
		args = append(args, "--app_data_dir="+opts.AppDataDir) // L282
	}
	if opts.CLIDirName != "" {
		args = append(args, "--cli_dir_name="+opts.CLIDirName) // L285
	}
	if opts.CloudCodeServerURL != "" {
		args = append(args, "--cloud_code_endpoint="+opts.CloudCodeServerURL) // L288
	}
	if opts.Agent != "" {
		args = append(args, "--agent="+opts.Agent) // L291
	}
	if opts.IDEMCPPort != 0 {
		args = append(args, fmt.Sprintf("--ide-mcp-port=%d", opts.IDEMCPPort)) // L294
	}
	for _, dir := range opts.AddDirs { // L298
		args = append(args, "--add-dir="+dir)
	}
	if opts.EnableTerminalSandbox {
		args = append(args, "--sandbox") // L302
	}
	if opts.DangerouslySkipPermissions {
		args = append(args, "--dangerously-skip-permissions") // L305
	}
	if opts.IncludeDefaultCustomizations != nil {
		args = append(args, fmt.Sprintf("--include-default-customizations=%t", *opts.IncludeDefaultCustomizations)) // L308
	}
	if opts.DisableTelemetry {
		args = append(args, "--disable_telemetry") // L311
	}
	if opts.IsGoogleEnvironmentSet {
		args = append(args, fmt.Sprintf("--is_google3_workspace=%t", opts.IsGoogleEnvironment)) // L314
	}
	if opts.UseStubbyAuth {
		args = append(args, "--use_stubby_auth=%t", opts.UseStubbyAuth) /* L317: fmt.Sprintf("--use_stubby_auth=%t", opts.UseStubbyAuth) */
	}
	if opts.UserAgentName != "" {
		args = append(args, "--override_user_agent_name="+opts.UserAgentName) // L320
	}
	if opts.LogFile != "" {
		args = append(args, "--log-file="+opts.LogFile) // L323
	}
	return args
}

// applyExperimentModelOverride reads AGY_CLI_MODEL_EXPERIMENT_PROFILE and maps it
// via codeium_common_pb.Model_value.
// Binary VA: 0x102fadda0 | Size: 304 B | Source: third_party/jetski/cli/acp/options.go:L328
func applyExperimentModelOverride(ctx context.Context) {
	val := os.Getenv("AGY_CLI_MODEL_EXPERIMENT_PROFILE") // L329
	if val == "" {
		return
	}
	if _, ok := codeium_common_pb.Model_value[val]; !ok { // L333
		log.Infof(ctx, "%s=%q does not name a codeium_common.Model enum value; ignoring", "AGY_CLI_MODEL_EXPERIMENT_PROFILE", val) // L335
		return
	}
	// [unrecovered] stores resolved model override
}

// runWith is the main entry point for `agy acp`, dispatching --login, --worker/single-process Serve,
// or --process-per-session runRouter.
// Binary VA: 0x102faded0 | Size: 2192 B | Source: third_party/jetski/cli/acp/options.go:L348
func runWith(opts Options) error {
	ctx := context.Background() // L349
	if opts.WorkingDir == "" {
		wd, err := server.ResolveWorkingDir() // L356
		if err != nil {
			log.Errorf(ctx, "Failed to get working directory: %v", err) // L358
		} else {
			opts.WorkingDir = wd
		}
	}
	if opts.GeminiDir == "" {
		opts.GeminiDir = ".gemini" // L362
	}
	if opts.Version == "" {
		opts.Version = "0.2.0" // L371
	}
	if opts.CloudCodeServerURL == "" {
		if env := os.Getenv("CLOUD_CODE_URL"); env != "" { // L373
			opts.CloudCodeServerURL = env
		} else {
			opts.CloudCodeServerURL = "https://daily-cloudcode-pa.googleapis.com" // L377
		}
	}

	analytics.SetSurface("acp") // L383

	lsCfg := language_server.LanguageServerConfig{ // L385
		// [unrecovered] full field initialization
	}
	_ = codeium_common_pb.Metadata_builder{ /* L402 "antigravity", L403 "cli" */ }.Build() // L405

	if opts.Login { // L380
		return runLoginCommand(ctx, opts, lsCfg)
	}

	applyExperimentModelOverride(ctx) // L411

	in := opts.Stdin // L415
	if in == nil {
		in = os.Stdin
	}
	out := opts.Stdout // L425
	if out == nil {
		out = os.Stdout
	}
	errOut := opts.Stderr // L435
	if errOut == nil {
		errOut = os.Stderr
	}

	cleanup, redirectedOut, err := redirectOutputForACP(opts.LogFile, opts.AppDataDir, opts.CLIDirName, opts.Worker, opts.IsGoogleEnvironment) // L432
	if err != nil {
		fmt.Fprintf(errOut, "Failed to redirect ACP output to log file: %v\n", err) // L436
		return err
	}
	defer cleanup()
	if redirectedOut != nil {
		out = redirectedOut // L443
	}

	if !opts.ProcessPerSession || opts.Worker {
		cfg := server.Config{
			WorkingDir:                   opts.WorkingDir,
			AppDataDir:                   opts.AppDataDir,
			GeminiDir:                    opts.GeminiDir,
			CLIDirName:                   opts.CLIDirName,
			Version:                      opts.Version,
			Agent:                        opts.Agent,
			IsGoogleEnvironment:          opts.IsGoogleEnvironment,
			IsJetski:                     opts.IsJetski,
			EnableTerminalSandbox:        opts.EnableTerminalSandbox,
			DangerouslySkipPermissions:   opts.DangerouslySkipPermissions,
			IncludeDefaultCustomizations: opts.IncludeDefaultCustomizations,
			AddDirs:                      opts.AddDirs,
			IDEMCPPort:                   opts.IDEMCPPort,
		}
		return server.Serve(ctx, in, out, cfg, lsCfg, opts.Worker) // L463
	}
	return runRouter(ctx, opts, in, out) // L465
}

// runRouter runs the `--process-per-session` front router process.
// Binary VA: 0x102fae760 | Size: 1456 B | Source: third_party/jetski/cli/acp/options.go:L468
func runRouter(ctx context.Context, opts Options, in io.Reader, out io.Writer) error {
	log.Infof(ctx, "Starting ACP front (pid %d, isJetski=%v)", os.Getpid(), opts.IsJetski) // L469
	workerArgs := buildWorkerArgs(opts)                                                      // L470

	r := router.NewRouter(in, out, router.SpawnWorkerProcesses(workerArgs, opts.Stderr)) // inlined router.go:169-180,1030
	// Binary VA: 0x102faf150 | Size: 64 B | Source: third_party/jetski/cli/acp/options.go:L474 (runRouter.func1)
	stopSignal := server.OnSignal(func() {
		r.Shutdown() // L475
		os.Exit(0)   // L476
	}, syscall.SIGINT, syscall.SIGTERM /* inferred */)
	defer stopSignal()

	err := r.Run() // L481
	if err != nil {
		log.Errorf(ctx, "Front failed: %v", err) // L483
	} else {
		log.Infof(ctx, "Client ended the session; shutting down workers") // L483
	}
	r.Shutdown() // L486
	return err   // L487
}
