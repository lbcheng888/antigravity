// Reconstructed from agy v1.5.10 binary evidence (disassembly + Go type table).
// Binary VA: 0x102fab130 | Size: 4 funcs (first func redirectOutputForACP, 1552 B) | Source: third_party/jetski/cli/acp/cli_helpers.go:L21
package acp

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google3/base/go/log" // inferred: logsink.Text
	"google3/third_party/jetski/cli/acp/server"
	"google3/third_party/jetski/cli/termio"
)

// writerSink adapts an io.Writer to logsink.Text. go_type 0x10a1abeb8, size 16.
type writerSink struct {
	w io.Writer // +0x00
}

// redirectOutputForACP redirects stdout/stderr to a timestamped ACP log file
// (or logFile if non-empty) and tees log output into it, returning a cleanup
// func and the original stdout writer for JSON-RPC transport.
// Binary VA: 0x102fab130 | Size: 1552 B | Source: third_party/jetski/cli/acp/cli_helpers.go:L21
func redirectOutputForACP(logFile, appDataDir, cliDirName string, isWorker bool, isGoogleEnv bool) (func(), io.Writer, error) {
	role := "front" /* inferred */
	if isWorker {   // L25
		role = fmt.Sprintf("%s.%d", "worker", os.Getpid()) // L26
	}
	if logFile == "" { // L28
		logDir, err := server.ResolveLogDir(appDataDir, cliDirName, isGoogleEnv) // L29
		if err != nil {                                                          // L30
			return nil, nil, fmt.Errorf("resolving log directory: %w", err) // L31
		}
		ts := time.Now().Format("20060102_150405") // L33
		logFile = filepath.Join(logDir, "log", fmt.Sprintf("acp-%s-%s-%d.log", role, ts, os.Getpid())) // L34
	}
	if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil { // L37 (0x1ed)
		return nil, nil, fmt.Errorf("creating log file directory: %w", err) // L38
	}
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644) // L40 (0x601, 0x1a4)
	if err != nil {                                                           // L41
		return nil, nil, fmt.Errorf("opening log file: %w", err) // L42
	}

	sink := &writerSink{w: f} // L46
	// L45/L48: termio.Redirect + logsink registration
	_ = sink
	saved, err := termio.Redirect(f, f) // L48
	if err != nil {                     // L49
		_ = f.Close() // L50
		return nil, nil, fmt.Errorf("redirecting output to log file: %w", err) // L52
	}
	_ = log.Info /* inferred: register/unregister [1]logsink.Text */

	// Binary VA: 0x102faed10 | Size: 144 B | Source: third_party/jetski/cli/acp/cli_helpers.go:L55 (redirectOutputForACP.func1)
	cleanup := func() {
		// L56: unregister logsink
		saved.Close() // L57
		_ = f.Close()
	}
	_ = strings.TrimSpace /* inferred */
	return cleanup, os.Stdout, nil // L60 (saved stdout / os.Stdout)
}

// Enabled reports whether writerSink is enabled.
// Binary VA: 0x102fab740 | Size: 16 B | Source: third_party/jetski/cli/acp/cli_helpers.go:L67
func (s *writerSink) Enabled() bool {
	return s != nil && s.w != nil /* inferred */
}

// Emit writes a formatted log record to w.
// Binary VA: 0x102fab750 | Size: 112 B | Source: third_party/jetski/cli/acp/cli_helpers.go:L69
func (s *writerSink) Emit(rec any /* inferred */) {
	// L70: call-indirect on s.w.Write(...)
}
