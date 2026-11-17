// Binary VA: 0x101c28db0 | Size: 16 B | Source: third_party/jetski/language_server/code_assist_client/keyring_detector_dbus.go:L25
package codeassistclient

type dbusDetector struct{}

// Binary VA: 0x101c28db0 | Size: 16 B | Source: third_party/jetski/language_server/code_assist_client/keyring_detector_dbus.go:L25
func (dbusDetector) Name() string {
	return "no D-Bus session bus"
}

// Binary VA: 0x101c28dc0 | Size: 16 B | Source: third_party/jetski/language_server/code_assist_client/keyring_detector_dbus.go:L33
// Darwin build stub: always returns false.
func (dbusDetector) Detect() bool {
	return false
}
