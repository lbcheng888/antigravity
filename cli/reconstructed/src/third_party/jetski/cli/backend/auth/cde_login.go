// Binary VA: 0x101d41da0 | Size: 4896 B (8 funcs) | Source: third_party/jetski/cli/backend/auth/cde_login.go
package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google3/third_party/jetski/language_server/auth_client/authclient"
	"google3/third_party/jetski/language_server/code_assist_client/codeassistclient"
	"google3/third_party/jetski/language_server/constants/constants"
)

/* inferred: options struct for RunLogin / RunAuthActionWithOptions */
type LoginOptions struct {
	Out            io.Writer
	Storage        codeassistclient.TokenStorage
	Acquirer       authclient.TokenAcquirer
	CheckOnly      bool
	EnterpriseAuth *bool
	Project        string
	Location       string
}

// Binary VA: 0x101d41da0 | Size: 240 B | Source: third_party/jetski/cli/backend/auth/cde_login.go:L32
func defaultTokenStorage() (codeassistclient.TokenStorage, error) {
	fileStorage, err := codeassistclient.NewFileTokenStorage()
	if err != nil {
		return nil, err
	}
	keyringStorage := &codeassistclient.KeyringTokenStorage{
		/* inferred: user "antigravity" */
	}
	markerPath := codeassistclient.DefaultKeyringMarkerPath("antigravity")
	return codeassistclient.NewCompositeTokenStorage(keyringStorage, fileStorage, false, markerPath, nil), nil
}

// Binary VA: 0x101d41e90 | Size: 224 B | Source: third_party/jetski/cli/backend/auth/cde_login.go:L44
func isEnterpriseAuth(override *bool, authMethod string) bool {
	if override != nil {
		return *override
	}
	if authMethod != "" {
		return authMethod == "gcp"
	}
	return os.Getenv("AGY_CDE_ENTERPRISE_AUTH") != "false"
}

// Binary VA: 0x101d41f70 | Size: 624 B | Source: third_party/jetski/cli/backend/auth/cde_login.go:L57
func refreshAndSaveStoredToken(ctx context.Context, storage codeassistclient.TokenStorage, stored *codeassistclient.StoredToken) error {
	cfg, err := oauthConfigFor(stored.AuthMethod, stored.WIFProvider)
	if err != nil {
		return err
	}
	var saveErr error
	ts := &savingTokenSource{
		base: codeassistclient.RefreshingTokenSource(ctx, cfg, stored.Token),
		// Binary VA: 0x101d45f80 | Size: 416 B | Source: third_party/jetski/cli/backend/auth/cde_login.go:L65
		saveFunc: func(tok *oauth2.Token) error {
			var opts []codeassistclient.SaveTokenOption
			if stored.WIFProvider != "" {
				opts = append(opts, codeassistclient.WithWIFProvider(stored.WIFProvider))
			}
			saveErr = storage.SaveToken(tok, stored.AuthMethod, opts...)
			return saveErr
		},
	}
	newTok, err := ts.Token()
	if err != nil {
		return err
	}
	if saveErr != nil {
		return fmt.Errorf("failed to persist refreshed token: %w", saveErr)
	}
	stored.Token = newTok
	stored.RememberIDToken()
	return nil
}

// Binary VA: 0x101d421e0 | Size: 2112 B | Source: third_party/jetski/cli/backend/auth/cde_login.go:L88
func RunLogin(ctx context.Context, opts LoginOptions) error {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	if os.Getenv("AGY_CDE_SEAMLESS_AUTH") == "true" || os.Getenv("AGY_ADC_AUTH") == "true" {
		if creds, err := google.FindDefaultCredentialsWithParams(ctx, google.CredentialsParams{
			Scopes: defaultScopes,
		}); err == nil && creds != nil {
			if _, err := creds.TokenSource.Token(); err == nil {
				fmt.Fprintln(out, "Already authenticated via Application Default Credentials (ADC).")
				return nil
			}
		}
	}

	storage := opts.Storage
	if storage == nil {
		var err error
		storage, err = defaultTokenStorage()
		if err != nil {
			return fmt.Errorf("failed to initialize token storage: %w", err)
		}
	}

	if opts.CheckOnly {
		// Binary VA: 0x101d46120 | Size: 672 B | Source: third_party/jetski/cli/backend/auth/cde_login.go:L121
		checkFn := func(ctx context.Context) (string, error) {
			stored, err := storage.LoadStoredToken()
			if err != nil {
				return "", fmt.Errorf("failed to load stored credentials: %w", err)
			}
			if stored == nil || stored.Token == nil || stored.Token.RefreshToken == "" {
				return "", errors.New("not authenticated: no stored credentials found")
			}
			checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			// Binary VA: 0x101d463c0 | Size: 96 B | Source: third_party/jetski/cli/backend/auth/cde_login.go:L116
			if err := refreshAndSaveStoredToken(checkCtx, storage, stored); err != nil {
				return "", fmt.Errorf("stored credentials are invalid or expired: %w", err)
			}
			return stored.IDTokenEmail, nil
		}
		email, err := checkFn(ctx)
		if err != nil {
			return err
		}
		if email != "" {
			fmt.Fprintf(out, "Already authenticated as %s.\n", email)
		} else {
			fmt.Fprintln(out, "Already authenticated.")
		}
		return nil
	}

	acquirer := opts.Acquirer
	if acquirer == nil {
		acquirer = &authclient.TerminalTokenAcquirer{}
	}
	authMethod := "consumer"
	if isEnterpriseAuth(opts.EnterpriseAuth, "") {
		authMethod = "gcp"
	}
	tok, err := acquirer.AcquireToken(ctx /* inferred args */)
	if err != nil {
		return err
	}
	if tok == nil {
		return errors.New("empty token returned")
	}
	if err := storage.SaveToken(tok, authMethod); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}
	if authMethod == "gcp" {
		project := opts.Project
		if project == "" {
			project = constants.GetEnvProject()
		}
		if project != "" {
			loc := opts.Location
			if loc == "" {
				loc = "global"
			}
			if err := storage.SaveProjectAndRegion(project, loc); err != nil {
				return fmt.Errorf("failed to save project and region: %w", err)
			}
			fmt.Fprintf(out, "Configured Google Cloud Project: %s (location: %s)\n", project, loc)
		} else {
			if err := storage.SaveProjectAndRegion("", ""); err != nil {
				return fmt.Errorf("failed to clear project and region: %w", err)
			}
		}
		if err := storage.SaveUserTier("", ""); err != nil {
			return fmt.Errorf("failed to clear user tier: %w", err)
		}
	}

	if claims, err := codeassistclient.ExtractOIDCClaims(tok); err == nil && claims.Email != "" {
		fmt.Fprintf(out, "Successfully authenticated as %s.\n", claims.Email)
	} else {
		fmt.Fprintln(out, "Successfully authenticated.")
	}
	return nil
}

// Binary VA: 0x101d42a20 | Size: 512 B | Source: third_party/jetski/cli/backend/auth/cde_login.go:L219
func RunAuthActionWithOptions(ctx context.Context, action string, opts LoginOptions) int {
	out := opts.Out
	if out == nil {
		out = os.Stderr
	}
	switch action {
	case "check":
		opts.CheckOnly = true
		if err := RunLogin(ctx, opts); err != nil {
			return 1
		}
		return 0
	case "login":
		if err := RunLogin(ctx, opts); err != nil {
			fmt.Fprintf(out, "Authentication failed: %v\n", err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(out, "Unknown AGY_CLI_CDE_AUTH_ACTION: %q (supported: check, login)\n", action)
		return 2
	}
}
