// Binary VA: 0x101c28dd0 | Size: 16 B | Source: third_party/jetski/language_server/code_assist_client/keyring_detector_ssh.go:L7
package codeassistclient

import "os"

type sshDetector struct{}

// Binary VA: 0x101c28dd0 | Size: 16 B | Source: third_party/jetski/language_server/code_assist_client/keyring_detector_ssh.go:L7
func (sshDetector) Name() string {
	return "SSH session"
}

// Binary VA: 0x101c28de0 | Size: 128 B | Source: third_party/jetski/language_server/code_assist_client/keyring_detector_ssh.go:L9
func (sshDetector) Detect() bool {
	if os.Getenv("SSH_TTY") != "" || os.Getenv("SSH_CLIENT") != "" {
		return true
	}
	return os.Getenv("SSH_CONNECTION") != ""
}
