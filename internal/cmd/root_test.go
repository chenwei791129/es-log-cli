package cmd

import (
	"syscall"
	"testing"
)

// TestCommandTree verifies the root command mounts every subcommand and exposes
// the documented global persistent flags (task 1.1).
func TestCommandTree(t *testing.T) {
	root := NewRootCommand()

	wantSub := []string{"config", "ls", "search", "ping", "version"}
	for _, name := range wantSub {
		if _, _, err := root.Find([]string{name}); err != nil {
			t.Errorf("subcommand %q not mounted: %v", name, err)
		}
	}

	pf := root.PersistentFlags()
	for _, f := range []string{"context", "output", "quiet", "config"} {
		if pf.Lookup(f) == nil {
			t.Errorf("global flag --%s not registered", f)
		}
	}
	if pf.ShorthandLookup("c") == nil {
		t.Error("global flag shorthand -c not registered")
	}
	if pf.ShorthandLookup("o") == nil {
		t.Error("global flag shorthand -o not registered")
	}
}

// TestSignalExitCode verifies signal-to-exit-code mapping follows 128+signum
// (task 1.3): SIGINT(2) -> 130, SIGTERM(15) -> 143.
func TestSignalExitCode(t *testing.T) {
	cases := []struct {
		sig  syscall.Signal
		want int
	}{
		{syscall.SIGINT, 130},
		{syscall.SIGTERM, 143},
	}
	for _, c := range cases {
		if got := SignalExitCode(c.sig); got != c.want {
			t.Errorf("SignalExitCode(%v) = %d, want %d", c.sig, got, c.want)
		}
	}
}
