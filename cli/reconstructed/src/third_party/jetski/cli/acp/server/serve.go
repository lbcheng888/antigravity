// Reconstructed from agy v1.5.10 binary evidence (disassembly + Go type table).
// Binary VA: 0x102f8bf00 | Size: 12 funcs (first func Serve, 1504 B) | Source: third_party/jetski/cli/acp/server/serve.go:L29
package server

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google3/base/go/log"
	"google3/third_party/jetski/cli/acp/session"
	"google3/third_party/jetski/language_server"
)

// Serve creates a Server, wires signal/worker-parent exit hooks, and runs the
// stdio JSON-RPC loop until EOF or error, then shuts down within 3s.
// Binary VA: 0x102f8bf00 | Size: 1504 B | Source: third_party/jetski/cli/acp/server/serve.go:L29
func Serve(ctx context.Context, in io.Reader, out io.Writer, cfg Config, lsCfg language_server.LanguageServerConfig, isWorker bool) error {
	log.Infof(ctx, "Starting ACP server (pid %d, worker=%v, isJetski=%v)", os.Getpid(), isWorker, cfg.IsJetski) // L30

	// Inlined NewServer (server.go:101-108)
	s := &Server{ // L31
		boot:          newBackendBooter(cfg, lsCfg), // launcher.go:129
		params:        cfg,
		sessionStates: make(map[string]*session.State), // server.go:104
		singleSession: isWorker,
	}
	s.turnSource = s.serverTurnsWithMuHeld // server.go:107
	s.catalog = backendCatalog{server: s}   // server.go:108

	// Binary VA: 0x102f988d0 | Size: 96 B | Source: third_party/jetski/cli/acp/server/serve.go:L34 (Serve.func1)
	shutdown := func() {
		shutdownWithin(3*time.Second, func() { // L34 (0xb2d05e00 ns)
			s.Shutdown()
		})
	}

	if isWorker {
		// Binary VA: 0x102f98880 | Size: 80 B | Source: third_party/jetski/cli/acp/server/serve.go:L36 (Serve.func2)
		onParentGone := func() {
			shutdown() // L37
			os.Exit(0) // L38
		}
		// Binary VA: 0x102f98840 | Size: 64 B | Source: third_party/jetski/cli/acp/server/serve.go:L41 (Serve.gowrap1)
		go watchParent(ctx, onParentGone) // L41
	}

	// Binary VA: 0x102f987f0 | Size: 80 B | Source: third_party/jetski/cli/acp/server/serve.go:L44 (Serve.func3)
	stopSig := OnSignal(func() {
		shutdown() // L45
		os.Exit(0) // L46
	}, syscall.SIGINT, syscall.SIGTERM /* inferred */)
	defer stopSig()

	err := s.runWithContext(ctx, in, out) // L51
	if err != nil {
		log.Errorf(ctx, "ACP server run error: %v", err) // L53
	}
	shutdown() // L56
	return err
}

// OnSignal registers a goroutine that calls onSignal on SIGINT/SIGTERM and returns a stop func.
// Binary VA: 0x102f8c4e0 | Size: 336 B | Source: third_party/jetski/cli/acp/server/serve.go:L61
func OnSignal(onSignal func(), sigs ...os.Signal) func() {
	sigCh := make(chan os.Signal, 1) // L62
	signal.Notify(sigCh, sigs...)     // L63
	done := make(chan struct{})       // L64

	// Binary VA: 0x102f98980 | Size: 288 B | Source: third_party/jetski/cli/acp/server/serve.go:L65 (OnSignal.func1)
	go func() {
		select { // L66
		case sig := <-sigCh:
			log.Infof(context.Background(), "Received signal %v, shutting down", sig) // L68
			onSignal()                                                                // L69
		case <-done:
		}
	}()

	// Binary VA: 0x102f98930 | Size: 80 B | Source: third_party/jetski/cli/acp/server/serve.go:L73 (OnSignal.func2)
	return func() {
		signal.Stop(sigCh) // L74
		close(done)        // L75
	}
}

// shutdownWithin runs fn and forces os.Exit(0) if it does not finish within timeout.
// Binary VA: 0x102f8c630 | Size: 224 B | Source: third_party/jetski/cli/acp/server/serve.go:L79
func shutdownWithin(timeout time.Duration, fn func()) {
	// Binary VA: 0x102f98ae0 | Size: 192 B | Source: third_party/jetski/cli/acp/server/serve.go:L82 (shutdownWithin.func1)
	timer := time.AfterFunc(timeout, func() {
		log.Infof(context.Background(), "Shutdown timed out after %s, forcing exit.", timeout) // L83
		os.Exit(0)                                                                            // L84
	})
	// Binary VA: 0x102f98aa0 | Size: 64 B | Source: third_party/jetski/cli/acp/server/serve.go:L86 (shutdownWithin.deferwrap1)
	defer timer.Stop() // L86
	fn()               // L88
}

// watchParent polls os.Getppid every 2s and calls onGone if the front process dies (ppid changes or becomes 1).
// Binary VA: 0x102f8c710 | Size: 256 B | Source: third_party/jetski/cli/acp/server/serve.go:L91
func watchParent(ctx context.Context, onGone func()) {
	origPPID := os.Getppid() // L92
	for {
		time.Sleep(2 * time.Second) // L94 (0x77359400 ns)
		if ppid := os.Getppid(); ppid != origPPID || ppid <= 1 {
			log.Infof(ctx, "Front process %d is gone, exiting", origPPID) // L96
			onGone()                                                      // L97
			return
		}
	}
}
