package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// cliResult captures the outcome of running the CLI in-process.
type cliResult struct {
	stdout string
	stderr string
	code   int
}

// runCLI executes the root command with args and returns captured output and the
// exit code.
func runCLI(t *testing.T, ctx context.Context, args ...string) cliResult {
	t.Helper()
	var out, errb bytes.Buffer
	code := Execute(ctx, args, &out, &errb)
	return cliResult{stdout: out.String(), stderr: errb.String(), code: code}
}

// writeTestConfig writes a single-context config named "test" pointing at server
// and returns the config file path.
func writeTestConfig(t *testing.T, server string) string {
	t.Helper()
	body := fmt.Sprintf("contexts:\n  - name: test\n    server: %s\n", server)
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// recordedReq captures one request hitting the stub.
type recordedReq struct {
	method string
	path   string
	query  string
	body   []byte
}

// esStub is a configurable Elasticsearch test double.
type esStub struct {
	server *httptest.Server
	reqs   []recordedReq
	// handler returns (status, body) for a recorded request.
	handler func(r recordedReq) (int, string)
}

// newESStub starts a stub server delegating responses to handler.
func newESStub(t *testing.T, handler func(r recordedReq) (int, string)) *esStub {
	t.Helper()
	s := &esStub{handler: handler}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		rec := recordedReq{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, body: data}
		s.reqs = append(s.reqs, rec)
		status, body := s.handler(rec)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.server.Close)
	return s
}

// url returns the stub's base URL.
func (s *esStub) url() string { return s.server.URL }

// searchRequests returns only the captured _search requests.
func (s *esStub) searchRequests() []recordedReq {
	var out []recordedReq
	for _, r := range s.reqs {
		if r.method == http.MethodPost && filepath.Base(r.path) == "_search" {
			out = append(out, r)
		}
	}
	return out
}

// settingsRequested reports whether any _settings (max_result_window) request was
// captured.
func (s *esStub) settingsRequested() bool {
	for _, r := range s.reqs {
		if containsPath(r.path, "_settings") {
			return true
		}
	}
	return false
}

func containsPath(path, seg string) bool {
	return bytes.Contains([]byte(path), []byte(seg))
}
