package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPingReportsVersion asserts ping hits GET / and appends the server
// version (or "version unknown") to the ok line.
func TestPingReportsVersion(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"elasticsearch", `{"version":{"number":"9.5.5"}}`, "(Elasticsearch 9.5.5)"},
		{"distribution", `{"version":{"number":"2.11.0","distribution":"opensearch"}}`, "(opensearch 2.11.0)"},
		{"unknown", `{"tagline":"no version here"}`, "(version unknown)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := newESStub(t, func(r recordedReq) (int, string) {
				if r.method == http.MethodGet && r.path == "/" {
					return 200, tc.body
				}
				return 404, `{}`
			})
			cfg := writeTestConfig(t, stub.url())
			res := runCLI(t, context.Background(), "ping", "-c", "test", "--config", cfg)
			if res.code != 0 {
				t.Fatalf("healthy ping exit %d: %s", res.code, res.stderr)
			}
			want := "ok: " + stub.url() + " " + tc.want + "\n"
			if res.stdout != want {
				t.Errorf("stdout = %q, want %q", res.stdout, want)
			}
			if len(stub.reqs) != 1 {
				t.Errorf("ping issued %d requests, want 1", len(stub.reqs))
			}
		})
	}
}

// TestPingQuiet asserts --quiet keeps stdout empty on success.
func TestPingQuiet(t *testing.T) {
	stub := newESStub(t, func(recordedReq) (int, string) { return 200, `{"version":{"number":"9.5.5"}}` })
	cfg := writeTestConfig(t, stub.url())
	res := runCLI(t, context.Background(), "ping", "-c", "test", "--config", cfg, "--quiet")
	if res.code != 0 || res.stdout != "" {
		t.Errorf("quiet ping: exit %d stdout %q", res.code, res.stdout)
	}
}

// TestPingFailures asserts exit 3 when authentication is rejected or the
// cluster is unreachable, with stdout kept clean.
func TestPingFailures(t *testing.T) {
	stub := newESStub(t, func(recordedReq) (int, string) {
		return 401, `{"error":{"type":"security_exception"}}`
	})
	res := runCLI(t, context.Background(), "ping", "-c", "test", "--config", writeTestConfig(t, stub.url()))
	if res.code != 3 || res.stdout != "" {
		t.Errorf("auth-rejected ping: exit %d stdout %q, want 3 and empty", res.code, res.stdout)
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
