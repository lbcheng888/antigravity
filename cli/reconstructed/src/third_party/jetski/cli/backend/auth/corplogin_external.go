// Binary VA: 0x101d42c20 | Size: 80 B (5 funcs) | Source: third_party/jetski/cli/backend/auth/corplogin_external.go
package auth

import "context"

// Binary VA: 0x10a0bf9d8 | Size: 0 B | Type: auth.externalCorpLoginAuth
type externalCorpLoginAuth struct{}

// Binary VA: 0x101d42c20 | Size: 16 B | Source: third_party/jetski/cli/backend/auth/corplogin_external.go:L13
func (*externalCorpLoginAuth) TryAuth(context.Context) *AuthResult {
	return nil
}

// Binary VA: 0x101d42c30 | Size: 16 B | Source: third_party/jetski/cli/backend/auth/corplogin_external.go:L14
func (*externalCorpLoginAuth) StartInteractive(context.Context, func(*AuthResult, error)) string {
	return ""
}

// Binary VA: 0x101d42c40 | Size: 16 B | Source: third_party/jetski/cli/backend/auth/corplogin_external.go:L17
func (*externalCorpLoginAuth) Logout() {}

// Binary VA: 0x101d42c50 | Size: 16 B | Source: third_party/jetski/cli/backend/auth/corplogin_external.go:L18
func (*externalCorpLoginAuth) Name() string {
	return "cxf"
}

// Binary VA: 0x101d42c60 | Size: 16 B | Source: third_party/jetski/cli/backend/auth/corplogin_external.go:L19
func (*externalCorpLoginAuth) IsSupported() bool {
	return false
}
