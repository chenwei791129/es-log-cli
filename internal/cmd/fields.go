package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/chenwei791129/es-log-cli/internal/esclient"
	"github.com/chenwei791129/es-log-cli/internal/output"
)

// fieldRow is one row of the `fields` output: a flattened field path, its
// distinct type set, whether those types diverge across indices, and (only on a
// conflict) the per-index type breakdown.
type fieldRow struct {
	Name     string            `json:"name"`
	Types    []string          `json:"types"`
	Conflict bool              `json:"conflict"`
	Indices  map[string]string `json:"indices,omitempty"`
}

// newFieldsCommand builds the `fields <target>` command, which inspects a
// target's mapping as flattened field paths and types.
func newFieldsCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "fields <target>",
		Short: "Inspect a target's mapping as flattened field paths and types",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFields(cmd, opts, args[0])
		},
	}
}

// runFields resolves the mapping for target and renders the flattened fields,
// mirroring runLs: loadConfig -> formatFor -> buildClient -> GetMapping -> render.
func runFields(cmd *cobra.Command, opts *globalOptions, target string) error {
	// ExactArgs(1) checks arg count but not content; reject an empty target as a
	// usage error rather than issuing GET //_mapping, mirroring search's guard.
	if target == "" {
		return newExitError(exitUsage, "no target: provide a non-empty target argument")
	}
	cfg, err := opts.loadConfig()
	if err != nil {
		return err
	}
	format, err := opts.formatFor("fields")
	if err != nil {
		return err
	}
	client, _, err := opts.buildClient(cfg)
	if err != nil {
		return err
	}

	fields, err := client.GetMapping(cmd.Context(), target)
	if err != nil {
		return classifyESError(target, err)
	}
	return renderFields(cmd, format, toFieldRows(fields))
}

// toFieldRows maps client FieldTypes to output rows. The client already owns the
// conflict decision — it populates ByIndex only on a divergent type set — so a
// row is a conflict exactly when that per-index breakdown is present, rather than
// re-deriving the rule here.
func toFieldRows(fields []esclient.FieldType) []fieldRow {
	rows := make([]fieldRow, 0, len(fields))
	for _, f := range fields {
		rows = append(rows, fieldRow{
			Name:     f.Name,
			Types:    f.Types,
			Conflict: f.ByIndex != nil,
			Indices:  f.ByIndex,
		})
	}
	return rows
}

// renderFields writes field rows in the requested format.
func renderFields(cmd *cobra.Command, format string, rows []fieldRow) error {
	out := cmd.OutOrStdout()
	switch format {
	case output.FormatTable:
		tableRows := make([][]string, 0, len(rows))
		for _, r := range rows {
			tableRows = append(tableRows, []string{r.Name, fieldTypeCell(r)})
		}
		return output.RenderTable(out, []string{"FIELD", "TYPE"}, tableRows)
	case output.FormatJSONL:
		return output.WriteJSONLines(out, rows)
	default:
		if rows == nil {
			rows = []fieldRow{}
		}
		return output.WriteJSON(out, rows)
	}
}

// fieldTypeCell renders the TYPE column for table output: the comma-joined type
// set, with the conflict marker appended on a divergent field.
func fieldTypeCell(r fieldRow) string {
	cell := strings.Join(r.Types, ", ")
	if r.Conflict {
		cell += "  ⚠ conflict"
	}
	return cell
}
