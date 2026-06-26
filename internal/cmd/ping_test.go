package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPingCommand asserts exit 0 on a healthy cluster and exit 3 when the
// cluster is unreachable (task 3.6).
func TestPingCommand(t *testing.T) {
	stub := newESStub(t, func(r recordedReq) (int, string) {
		if r.path == "/_cluster/health" {
			return 200, `{"status":"green"}`
		}
		return 404, `{}`
	})
	cfg := writeTestConfig(t, stub.url())
	res := runCLI(t, context.Background(), "ping", "-c", "test", "--config", cfg)
	if res.code != 0 {
		t.Errorf("healthy ping exit %d: %s", res.code, res.stderr)
	}

	// Unreachable cluster -> exit 3.
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()
	cfg2 := writeTestConfig(t, deadURL)
	res = runCLI(t, context.Background(), "ping", "-c", "test", "--config", cfg2)
	if res.code != 3 {
		t.Errorf("unreachable ping exit %d, want 3 (%s)", res.code, res.stderr)
	}
	if res.stdout != "" {
		t.Errorf("stdout should be clean on failure: %q", res.stdout)
	}
}
