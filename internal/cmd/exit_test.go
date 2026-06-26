package cmd

import (
	"bytes"
	"errors"
	"testing"

	"github.com/chenwei791129/es-log-cli/internal/esclient"
)

// TestExitCodeMapping table-drives the layered exit codes (task 4.5).
func TestExitCodeMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"usage error", newExitError(exitUsage, "bad flag"), exitUsage},
		{"not found 404", classifyESError("app-logs", &esclient.APIError{StatusCode: 404, ErrType: "index_not_found_exception"}), exitNotFound},
		{"auth rejected 401", classifyESError("app-logs", &esclient.APIError{StatusCode: 401}), exitConn},
		{"forbidden 403", classifyESError("app-logs", &esclient.APIError{StatusCode: 403}), exitConn},
		{"server error 500", classifyESError("app-logs", &esclient.APIError{StatusCode: 500}), exitConn},
		{"transport failure", classifyESError("app-logs", errors.New("dial tcp: refused")), exitConn},
		{"unclassified error", errors.New("cobra parse error"), exitUsage},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			if got := reportError(&buf, c.err); got != c.want {
				t.Errorf("reportError = %d, want %d", got, c.want)
			}
			if buf.Len() == 0 {
				t.Error("error message not written to stderr")
			}
		})
	}
}

// TestNotFoundMentionsTarget asserts the 404 message names the target.
func TestNotFoundMentionsTarget(t *testing.T) {
	err := classifyESError("my-target", &esclient.APIError{StatusCode: 404})
	if !bytes.Contains([]byte(err.Error()), []byte("my-target")) {
		t.Errorf("404 message should name target: %q", err.Error())
	}
}
