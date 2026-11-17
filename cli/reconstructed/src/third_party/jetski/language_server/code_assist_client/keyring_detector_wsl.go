// Binary VA: 0x101c28ec0 | Size: 16 B | Source: third_party/jetski/language_server/code_assist_client/keyring_detector_wsl.go:L10
package codeassistclient

import (
	"os"
	"strings"
)

type wslDetector struct{}

// Binary VA: 0x101c28ec0 | Size: 16 B | Source: third_party/jetski/language_server/code_assist_client/keyring_detector_wsl.go:L10
func (wslDetector) Name() string {
	return "WSL environment"
}

// Binary VA: 0x101c28ed0 | Size: 224 B | Source: third_party/jetski/language_server/code_assist_client/keyring_detector_wsl.go:L12
func (wslDetector) Detect() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		return true
	}
	data, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return false
	}
	lower := strings.ToLower(string(data))
	return strings.Contains(lower, "microsoft") || strings.Contains(lower, "wsl")
}
