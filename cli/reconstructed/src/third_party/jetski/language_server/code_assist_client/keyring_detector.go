// Binary VA: 0x101c28a20 | Size: 656 B | Source: third_party/jetski/language_server/code_assist_client/keyring_detector.go:L26
package codeassistclient

import (
	"runtime"
	"time"
)

type environmentDetector interface {
	Detect() bool
	Name() string
}

type detectResult struct {
	detected bool
	name     string
}

/* inferred: package-level detector list at 0x10d3d8cb0 (len=5) */
var keyringDetectors = []environmentDetector{
	androidDetector{goos: runtime.GOOS},
	sshDetector{},
	wslDetector{},
	containerDetector{},
	dbusDetector{},
}

// Binary VA: 0x101c28a20 | Size: 656 B | Source: third_party/jetski/language_server/code_assist_client/keyring_detector.go:L26
func shouldBypassKeyring() (bool, string) {
	ch := make(chan detectResult, len(keyringDetectors))
	for _, d := range keyringDetectors {
		// Binary VA: 0x101c4dcc0 | Size: 128 B | Source: third_party/jetski/language_server/code_assist_client/keyring_detector.go:L30
		// (wrapped via shouldBypassKeyring.gowrap1 VA: 0x101c4dc80 | Size: 64 B)
		go func(det environmentDetector /* inferred */) {
			ch <- detectResult{
				detected: det.Detect(),
				name:     det.Name(),
			}
		}(d)
	}

	timer := time.NewTimer(500 * time.Millisecond)
	// Binary VA: 0x101c4dc40 | Size: 64 B | Source: third_party/jetski/language_server/code_assist_client/keyring_detector.go:L41
	defer timer.Stop()

	for i := 0; i < len(keyringDetectors); i++ {
		select {
		case res := <-ch:
			if res.detected {
				return true, res.name
			}
		case <-timer.C:
			return false, "detection timeout"
		}
	}
	return false, ""
}
