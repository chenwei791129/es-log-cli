package output

import (
	"bytes"
	"strings"
	"testing"
)

// TestOutputDefaults asserts search defaults to jsonl and other commands to json
// (task 4.1).
func TestOutputDefaults(t *testing.T) {
	if got := DefaultFor("search"); got != FormatJSONL {
		t.Errorf("search default = %q, want jsonl", got)
	}
	for _, cmd := range []string{"ls", "ping", "config"} {
		if got := DefaultFor(cmd); got != FormatJSON {
			t.Errorf("%s default = %q, want json", cmd, got)
		}
	}
}

// TestTableAlignment asserts the shared renderer produces aligned columns
// (task 4.1).
func TestTableAlignment(t *testing.T) {
	var buf bytes.Buffer
	headers := []string{"NAME", "TYPE"}
	rows := [][]string{{"app-logs", "alias"}, {"metrics-datastream", "datastream"}}
	if err := RenderTable(&buf, headers, rows); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines (header + 2 rows), got %d: %q", len(lines), buf.String())
	}
	// The second column must begin at the same offset on every line: at the
	// header's "TYPE" position the char is non-space and the prefix is the cell.
	off := strings.Index(lines[0], "TYPE")
	wantFirst := []string{"NAME", "app-logs", "metrics-datastream"}
	wantSecond := []string{"TYPE", "alias", "datastream"}
	for i, l := range lines {
		if off <= 0 || len(l) <= off || l[off] == ' ' || l[off-1] != ' ' {
			t.Errorf("column not aligned at offset %d: %q", off, l)
			continue
		}
		if strings.TrimRight(l[:off], " ") != wantFirst[i] {
			t.Errorf("line %d first cell = %q, want %q", i, strings.TrimRight(l[:off], " "), wantFirst[i])
		}
		if l[off:] != wantSecond[i] {
			t.Errorf("line %d second cell = %q, want %q", i, l[off:], wantSecond[i])
		}
	}
}

// TestQuietSuppressesWarning asserts Warnf goes to stderr normally and is fully
// suppressed under quiet, never reaching stdout (task 4.4).
func TestQuietSuppressesWarning(t *testing.T) {
	var out, errb bytes.Buffer
	p := &Printer{Out: &out, Err: &errb, Quiet: false}
	p.Warnf("capping to %d", 10000)
	if out.Len() != 0 {
		t.Errorf("warning leaked to stdout: %q", out.String())
	}
	if !strings.Contains(errb.String(), "capping to 10000") {
		t.Errorf("warning missing from stderr: %q", errb.String())
	}

	out.Reset()
	errb.Reset()
	pq := &Printer{Out: &out, Err: &errb, Quiet: true}
	pq.Warnf("capping to %d", 10000)
	if out.Len() != 0 || errb.Len() != 0 {
		t.Errorf("quiet should suppress warning entirely: out=%q err=%q", out.String(), errb.String())
	}
}

func TestWriteJSONAndLines(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, []string{"prod", "staging"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"prod"`) || !strings.HasSuffix(buf.String(), "\n") {
		t.Errorf("WriteJSON output = %q", buf.String())
	}

	buf.Reset()
	if err := WriteJSONLines(&buf, []any{map[string]int{"a": 1}, map[string]int{"b": 2}}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Errorf("want 2 lines, got %d: %q", len(lines), buf.String())
	}
}
