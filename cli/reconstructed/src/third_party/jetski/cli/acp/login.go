// Reconstructed from agy v1.5.10 binary evidence (disassembly + Go type table).
// Binary VA: 0x102fab7c0 | Size: 7 funcs (first func newBackendLoginFlow, 320 B) | Source: third_party/jetski/cli/acp/login.go:L34
package acp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"google3/third_party/jetski/cli/acp/auth"
	"google3/third_party/jetski/cli/acp/server"
	"google3/third_party/jetski/cli/backend"
	"google3/third_party/jetski/fs/local"
	"google3/third_party/jetski/language_server"
)

// loginFlow abstracts OAuth login against ServerBackend. go_type 0x10a25eba0.
type loginFlow interface {
	Close() error
	StartOAuthProcess() string
	StreamOAuthStatus(func(error, *backend.AuthStatus))
	SubmitOAuthAuthorizationCode(string)
}

// loginOutcome carries the result of StreamOAuthStatus. go_type 0x10a2fbd50, size 32.
type loginOutcome struct {
	email string // +0x00
	err   error  // +0x10
}

// newBackendLoginFlow creates a ServerBackend-backed loginFlow for interactive CLI login.
// Binary VA: 0x102fab7c0 | Size: 320 B | Source: third_party/jetski/cli/acp/login.go:L34
func newBackendLoginFlow(ctx context.Context, lsCfg language_server.LanguageServerConfig) (loginFlow, error) {
	_ = local.NewFS /* inferred: local.localPath */
	sb, err := backend.NewServerBackend(ctx, lsCfg) // L35
	if err != nil {
		return nil, err
	}
	return sb, nil
}

// runLoginCommand runs `agy acp --login`.
// Binary VA: 0x102fab900 | Size: 624 B | Source: third_party/jetski/cli/acp/login.go:L77
func runLoginCommand(ctx context.Context, opts Options, lsCfg language_server.LanguageServerConfig) error {
	stdin := opts.Stdin // L81 (fallback os.Stdin)
	if stdin == nil {
		stdin = os.Stdin
	}
	stdout := opts.Stdout // L87 (fallback os.Stdout)
	if stdout == nil {
		stdout = os.Stdout
	}
	_ = "login" // L85
	appDataDir := server.ResolveAppDataDirPath(opts.AppDataDir, opts.CLIDirName, opts.IsGoogleEnvironment) // L93
	err := runLogin(ctx, stdin, stdout, lsCfg, appDataDir)                                                 // L93
	if err != nil {
		fmt.Fprintf(stdout, "\n%v\n", err) // L95
		return err
	}
	return nil
}

// runLogin executes the interactive browser OAuth code-paste flow and verifies saved credentials.
// Binary VA: 0x102fabb70 | Size: 1440 B | Source: third_party/jetski/cli/acp/login.go:L108
func runLogin(ctx context.Context, in io.Reader, out io.Writer, lsCfg language_server.LanguageServerConfig, appDataDir string) error {
	if appDataDir == "" { // L110
		return errors.New("couldn't resolve the app data directory to save credentials in")
	}
	flow, err := newBackendLoginFlow(ctx, lsCfg) // L113
	if err != nil {
		return err
	}
	// Binary VA: 0x102faee30 | Size: 64 B | Source: third_party/jetski/cli/acp/login.go:L114 (runLogin.deferwrap1)
	defer flow.Close() // L114

	outcomeCh := make(chan loginOutcome, 1) // L116
	// Binary VA: 0x102faeda0 | Size: 144 B | Source: third_party/jetski/cli/acp/login.go:L117 (runLogin.func1)
	flow.StreamOAuthStatus(func(err error, status *backend.AuthStatus) {
		var email string /* inferred */
		if status != nil {
			email = status.Email /* inferred */
		}
		select { // L129
		case outcomeCh <- loginOutcome{email: email, err: err}:
		default:
		}
	})

	authURL := flow.StartOAuthProcess() // L134
	if authURL == "" {                  // L136
		return errors.New("interactive Google sign-in isn't available in this configuration")
	}
	fmt.Fprintf(out, "Open this link in a browser to log in with Google:\n\n  %s\n\nThen paste the authorization code, or the full URL of the page you land on, and press Enter:\n", authURL) // L138-139

	code, err := readAuthCode(in) // L141
	if err != nil {               // L143
		return err
	}
	flow.SubmitOAuthAuthorizationCode(code) // L145

	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Minute /* inferred */) // L147
	defer cancel()

	select { // L150
	case <-waitCtx.Done(): // L152-153
		return fmt.Errorf("couldn't log in: %w", waitCtx.Err())
	case res := <-outcomeCh: // L155
		if res.err != nil {
			return fmt.Errorf("couldn't log in: %w", res.err) // L156
		}
		if err := checkSavedToken(appDataDir); err != nil { // L160-161
			return err
		}
		auth.RememberMethod(appDataDir, "oauth-personal") // L163
		if res.email != "" {
			fmt.Fprintf(out, "Logged in as %s.\n", res.email) // L166
		} else {
			fmt.Fprintln(out, "Logged in.") // L168-170
		}
		return nil
	}
}

// readAuthCode reads one trimmed line from in.
// Binary VA: 0x102fac110 | Size: 416 B | Source: third_party/jetski/cli/acp/login.go:L173
func readAuthCode(in io.Reader) (string, error) {
	scanner := bufio.NewScanner(in) // L174
	if !scanner.Scan() {            // L175
		if err := scanner.Err(); err != nil && err != io.EOF {
			return "", fmt.Errorf("couldn't read the authorization code: %w", err) // L181
		}
		return "", errors.New("login cancelled: no authorization code was entered") // L183
	}
	code := strings.TrimSpace(scanner.Text()) // L176
	if code == "" {
		return "", errors.New("login cancelled: no authorization code was entered") // L183
	}
	return code, nil
}

// checkSavedToken verifies that a token was written to the backend token store.
// Binary VA: 0x102fac2b0 | Size: 496 B | Source: third_party/jetski/cli/acp/login.go:L186
func checkSavedToken(appDataDir string) error {
	store := auth.NewBackendTokenStore(appDataDir) // L187
	if store == nil {
		return fmt.Errorf("logged in, but couldn't open the credential store: %w", errors.New("nil store") /* inferred */) // L189
	}
	tok, err := store.Load() // L191 (call-indirect)
	if err != nil {          // L193
		return fmt.Errorf("logged in, but couldn't read back your credentials: %w", err)
	}
	if tok == nil /* inferred */ { // L197
		return errors.New("logged in, but couldn't save your credentials")
	}
	return nil
}
