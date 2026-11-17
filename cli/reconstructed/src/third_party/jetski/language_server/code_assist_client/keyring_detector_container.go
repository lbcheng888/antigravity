// Binary VA: 0x101c28cb0 | Size: 16 B | Source: third_party/jetski/language_server/code_assist_client/keyring_detector_container.go:L10
package codeassistclient

import (
	"os"
	"strings"
)

type containerDetector struct{}

// Binary VA: 0x101c28cb0 | Size: 16 B | Source: third_party/jetski/language_server/code_assist_client/keyring_detector_container.go:L10
func (containerDetector) Name() string {
	return "container environment"
}

// Binary VA: 0x101c28cc0 | Size: 240 B | Source: third_party/jetski/language_server/code_assist_client/keyring_detector_container.go:L12
func (containerDetector) Detect() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	data, err := os.ReadFile("/proc/1/cgroup")
	if err != nil {
		return false
	}
	content := string(data)
	return strings.Contains(content, "docker") ||
		strings.Contains(content, "containerd") ||
		strings.Contains(content, "kubepods")
}
