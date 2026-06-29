package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"syscall"

	"github.com/chenwei791129/es-log-cli/internal/esclient"
)

// Layered exit codes (see design "Errors on stderr with layered exit codes").
const (
	exitOK       = 0
	exitUsage    = 2 // argument/config errors (missing context, conflicting flags)
	exitConn     = 3 // connection or authentication failure
	exitNotFound = 4 // target index/datastream not found (404)
	exitPartial  = 5 // query responded but the result is incomplete (partial shard failure)
)

// exitError carries an explicit process exit code alongside an error message.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) ExitCode() int { return e.code }
func (e *exitError) Unwrap() error { return e.err }

// newExitError builds an exitError with a formatted message.
func newExitError(code int, format string, args ...any) *exitError {
	return &exitError{code: code, err: fmt.Errorf(format, args...)}
}

// newPartialExitError builds the exitPartial exit error describing a partial
// shard failure as "incomplete results: N of M shards failed (<reason>)". An
// empty reason (none could be parsed) is reported as "unknown reason".
func newPartialExitError(failed, total int, reason string) *exitError {
	if reason == "" {
		reason = "unknown reason"
	}
	return newExitError(exitPartial, "incomplete results: %d of %d shards failed (%s)", failed, total, reason)
}

// SignalExitCode maps a signal to its conventional exit code (128 + signum),
// e.g. SIGINT -> 130, SIGTERM -> 143.
func SignalExitCode(sig syscall.Signal) int {
	return 128 + int(sig)
}

// reportError prints err to stderr as plain text and returns the process exit
// code. Unclassified errors (e.g. cobra flag-parse failures) map to exitUsage.
func reportError(stderr io.Writer, err error) int {
	var ee *exitError
	if errors.As(err, &ee) {
		_, _ = fmt.Fprintln(stderr, "error:", ee.Error())
		return ee.ExitCode()
	}
	_, _ = fmt.Fprintln(stderr, "error:", err)
	return exitUsage
}

// classifyESError translates an esclient error into an exitError with the
// correct layered exit code for the given target.
func classifyESError(target string, err error) error {
	// A cancelled context (e.g. SIGINT mid-search) is an interruption, not a
	// connection failure. The process exit code is overridden to 128+signum by
	// the signal handler in main; keep the message accurate regardless.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return newExitError(exitConn, "canceled: %v", err)
	}
	var apiErr *esclient.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 404:
			// Commands without a target (e.g. ls) get a generic error rather than
			// a misleading `target "" not found`.
			if target == "" {
				return newExitError(exitConn, "elasticsearch returned 404 (%s)", apiErr.Summary())
			}
			return newExitError(exitNotFound, "target %q not found", target)
		case 401, 403:
			return newExitError(exitConn, "authentication failed (%s)", apiErr.Summary())
		default:
			// Diagnostic carries the root-cause reason when one was extracted, and
			// falls back to the status/type-only Summary when it is empty.
			return newExitError(exitConn, "elasticsearch error (%s)", apiErr.Diagnostic())
		}
	}
	return newExitError(exitConn, "connection failed: %v", err)
}
