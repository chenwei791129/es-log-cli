// Command es-log is a read-only Elasticsearch log query CLI for AI agents.
package main

import (
	"context"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"

	"github.com/chenwei791129/es-log-cli/internal/cmd"
)

func main() {
	// Translate SIGINT/SIGTERM into context cancellation so long-running
	// operations (search pagination) stop promptly, and exit with the
	// conventional 128+signum code.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var sigNum atomic.Int32
	go func() {
		s, ok := <-sigCh
		if !ok {
			return
		}
		if sg, ok := s.(syscall.Signal); ok {
			sigNum.Store(int32(sg))
		}
		// Restore default handling so a second Ctrl+C force-terminates a run that
		// is not responding to context cancellation.
		signal.Reset(syscall.SIGINT, syscall.SIGTERM)
		cancel()
	}()

	code := cmd.Execute(ctx, os.Args[1:], os.Stdout, os.Stderr)

	signal.Stop(sigCh)
	if n := sigNum.Load(); n != 0 {
		code = cmd.SignalExitCode(syscall.Signal(n))
	}
	os.Exit(code)
}
