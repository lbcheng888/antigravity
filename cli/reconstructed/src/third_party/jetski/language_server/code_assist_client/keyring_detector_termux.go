// Binary VA: 0x101c28e60 | Size: 16 B | Source: third_party/jetski/language_server/code_assist_client/keyring_detector_termux.go:L12
package codeassistclient

type androidDetector struct {
	goos string
}

// Binary VA: 0x101c28e60 | Size: 16 B | Source: third_party/jetski/language_server/code_assist_client/keyring_detector_termux.go:L12
func (androidDetector) Name() string {
	return "Android (no OS keyring)"
}

// Binary VA: 0x101c28e70 | Size: 80 B | Source: third_party/jetski/language_server/code_assist_client/keyring_detector_termux.go:L15
func (d androidDetector) Detect() bool {
	return d.goos == "android"
}
