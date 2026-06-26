// Package output renders command results in the jsonl/json/table formats and
// centralizes quiet-mode noise handling. All commands share one aligned-column
// table renderer rather than bespoke per-command layouts.
package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// Output format identifiers.
const (
	FormatJSONL = "jsonl"
	FormatJSON  = "json"
	FormatTable = "table"
)

// DefaultFor returns the default output format for a command: jsonl for search,
// json for everything else.
func DefaultFor(command string) string {
	if command == "search" {
		return FormatJSONL
	}
	return FormatJSON
}

// Valid reports whether format is a recognized output format.
func Valid(format string) bool {
	switch format {
	case FormatJSONL, FormatJSON, FormatTable:
		return true
	default:
		return false
	}
}

// Printer routes result output and suppressible noise. Warnings go to Err unless
// Quiet is set, in which case they are suppressed entirely. Results always go to
// Out.
type Printer struct {
	Out   io.Writer
	Err   io.Writer
	Quiet bool
}

// Warnf prints a warning to stderr unless quiet mode suppresses it. It never
// writes to stdout, keeping result output clean.
func (p *Printer) Warnf(format string, args ...any) {
	if p.Quiet {
		return
	}
	_, _ = fmt.Fprintf(p.Err, format+"\n", args...)
}

// WriteJSON writes v as an indented JSON document followed by a newline.
func WriteJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// WriteJSONLines writes each item as a compact JSON value on its own line.
func WriteJSONLines[T any](w io.Writer, items []T) error {
	enc := json.NewEncoder(w)
	for _, item := range items {
		if err := enc.Encode(item); err != nil {
			return err
		}
	}
	return nil
}

// WriteRawJSONLines writes each pre-serialized JSON value on its own line,
// compacting it to guarantee one record per line. An empty value (e.g. a hit
// with no _source) is emitted as a literal null rather than aborting the stream.
func WriteRawJSONLines(w io.Writer, items [][]byte) error {
	for _, item := range items {
		var buf bytes.Buffer
		if len(item) == 0 {
			buf.WriteString("null")
		} else if err := json.Compact(&buf, item); err != nil {
			return err
		}
		buf.WriteByte('\n')
		if _, err := w.Write(buf.Bytes()); err != nil {
			return err
		}
	}
	return nil
}

// cellSanitizer collapses tab/newline/carriage-return within a cell into spaces
// so a value cannot break tabwriter's column alignment. Applied to every cell at
// this shared choke point so all commands' tables are protected.
var cellSanitizer = strings.NewReplacer("\t", " ", "\n", " ", "\r", " ")

// RenderTable writes headers and rows as aligned columns using a shared
// tabwriter configuration, sanitizing each cell against alignment-breaking
// whitespace.
func RenderTable(w io.Writer, headers []string, rows [][]string) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, joinCells(headers)); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintln(tw, joinCells(row)); err != nil {
			return err
		}
	}
	return tw.Flush()
}

// joinCells sanitizes each cell and joins them with tab separators.
func joinCells(cells []string) string {
	out := make([]string, len(cells))
	for i, c := range cells {
		out[i] = cellSanitizer.Replace(c)
	}
	return strings.Join(out, "\t")
}
