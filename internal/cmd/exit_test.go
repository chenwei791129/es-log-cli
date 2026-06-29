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

// TestClassifyESErrorSurfacesReason asserts the default (non-404/401/403) branch
// includes the root-cause reason, locking the spec example message, and falls
// back to the type-only message when the reason is empty.
func TestClassifyESErrorSurfacesReason(t *testing.T) {
	withReason := classifyESError("app-logs", &esclient.APIError{
		StatusCode: 400,
		ErrType:    "search_phase_execution_exception",
		Reason:     "Fielddata is disabled on [some_field]",
	})
	wantMsg := "elasticsearch error (HTTP 400: search_phase_execution_exception — Fielddata is disabled on [some_field])"
	if withReason.Error() != wantMsg {
		t.Errorf("message = %q, want %q", withReason.Error(), wantMsg)
	}

	noReason := classifyESError("app-logs", &esclient.APIError{StatusCode: 400, ErrType: "search_phase_execution_exception"})
	wantFallback := "elasticsearch error (HTTP 400: search_phase_execution_exception)"
	if noReason.Error() != wantFallback {
		t.Errorf("fallback message = %q, want %q", noReason.Error(), wantFallback)
	}
}

// TestPartialExitError asserts the partial-failure exit error carries exit code 5
// and the documented message, substituting "unknown reason" for an empty reason.
func TestPartialExitError(t *testing.T) {
	err := newPartialExitError(2, 5, "Fielddata is disabled on [some_field]")
	if err.ExitCode() != exitPartial {
		t.Errorf("exit code = %d, want %d", err.ExitCode(), exitPartial)
	}
	want := "incomplete results: 2 of 5 shards failed (Fielddata is disabled on [some_field])"
	if err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}

	empty := newPartialExitError(1, 3, "")
	wantEmpty := "incomplete results: 1 of 3 shards failed (unknown reason)"
	if empty.Error() != wantEmpty {
		t.Errorf("empty-reason message = %q, want %q", empty.Error(), wantEmpty)
	}
}
