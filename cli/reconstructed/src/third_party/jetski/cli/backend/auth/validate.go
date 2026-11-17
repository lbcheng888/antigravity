// Binary VA: 0x101d442f0 | Size: 1360 B (2 funcs) | Source: third_party/jetski/cli/backend/auth/validate.go
package auth

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google3/third_party/jetski/language_server/code_assist_client/codeassistclient"
)

var errCredentialsRejected /* inferred */ = errors.New("credentials rejected")

// Binary VA: 0x101d442f0 | Size: 560 B | Source: third_party/jetski/cli/backend/auth/validate.go:L20
func oauthConfigFor(authMethod, wifProvider string) (*oauth2.Config, error) {
	if wifProvider != "" {
		return &oauth2.Config{
			ClientID: wifClientID,
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://auth.cloud.google/authorize",
				TokenURL: "https://sts.googleapis.com/v1/oauthtoken",
			},
			Scopes: wifScopes,
		}, nil
	}
	clientID, clientSecret, err := getOauthParams(authMethod)
	if err != nil {
		return nil, err
	}
	scopes := getScopesByAuthMethod(authMethod)
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     google.Endpoint,
		Scopes:       scopes,
	}, nil
}

// Binary VA: 0x101d44520 | Size: 800 B | Source: third_party/jetski/cli/backend/auth/validate.go:L40
func ValidateStoredToken(ctx context.Context, stored *codeassistclient.StoredToken) (*oauth2.Token, error) {
	if stored == nil || stored.Token == nil || stored.Token.RefreshToken == "" {
		return nil, fmt.Errorf("%w: no refresh token", errCredentialsRejected)
	}
	cfg, err := oauthConfigFor(stored.AuthMethod, stored.WIFProvider)
	if err != nil {
		return nil, err
	}
	refreshOnly := &oauth2.Token{
		RefreshToken: stored.Token.RefreshToken,
	}
	tok, err := cfg.TokenSource(ctx, refreshOnly).Token()
	if err != nil {
		var retrieveErr *oauth2.RetrieveError
		if errors.As(err, &retrieveErr) && retrieveErr.Response != nil &&
			retrieveErr.Response.StatusCode >= 400 &&
			retrieveErr.Response.StatusCode < 500 &&
			retrieveErr.Response.StatusCode != 429 {
			return nil, fmt.Errorf("%w: %v", errCredentialsRejected, err)
		}
		return nil, err
	}
	return tok, nil
}
