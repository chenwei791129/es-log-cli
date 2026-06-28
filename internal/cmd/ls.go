package cmd

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/chenwei791129/es-log-cli/internal/esclient"
	"github.com/chenwei791129/es-log-cli/internal/output"
)

// lsRow is one row of the `ls` output. Count fields are type-specific and
// omitted when not applicable.
type lsRow struct {
	Name                string `json:"name"`
	Type                string `json:"type"`
	IndexCount          *int   `json:"index_count,omitempty"`
	BackingIndicesCount *int   `json:"backing_indices_count,omitempty"`
}

// listing mode selectors.
const (
	listAll         = "all"
	listAliases     = "alias"
	listDataStreams = "datastream"
)

// newLsCommand builds the `ls` command (combined view) with `aliases` and
// `datastreams` subcommands that filter by type.
func newLsCommand(opts *globalOptions) *cobra.Command {
	var showHidden bool
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List aliases and datastreams",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLs(cmd, opts, listAll, showHidden)
		},
	}
	// Persistent flag so `aliases` and `datastreams` subcommands inherit it.
	cmd.PersistentFlags().BoolVar(&showHidden, "show-hidden", false,
		"include targets whose name begins with a dot (hidden by default)")
	cmd.AddCommand(
		&cobra.Command{
			Use:   "aliases",
			Short: "List aliases only",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return runLs(cmd, opts, listAliases, showHidden)
			},
		},
		&cobra.Command{
			Use:   "datastreams",
			Short: "List datastreams only",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return runLs(cmd, opts, listDataStreams, showHidden)
			},
		},
	)
	return cmd
}

// runLs fetches and renders targets filtered by mode. Unless showHidden is set,
// targets whose name begins with a dot are dropped before rendering so all output
// formats list an identical target set for the same flag value.
func runLs(cmd *cobra.Command, opts *globalOptions, mode string, showHidden bool) error {
	cfg, err := opts.loadConfig()
	if err != nil {
		return err
	}
	format, err := opts.formatFor("ls")
	if err != nil {
		return err
	}
	client, _, err := opts.buildClient(cfg)
	if err != nil {
		return err
	}

	rows, err := collectTargets(cmd.Context(), client, mode)
	if err != nil {
		return classifyESError("", err)
	}
	if !showHidden {
		rows = filterHidden(rows)
	}
	return renderLs(cmd, format, rows)
}

// filterHidden drops rows whose name begins with a dot (Elasticsearch's
// convention for system/hidden objects).
func filterHidden(rows []lsRow) []lsRow {
	filtered := rows[:0]
	for _, r := range rows {
		if strings.HasPrefix(r.Name, ".") {
			continue
		}
		filtered = append(filtered, r)
	}
	return filtered
}

// collectTargets gathers alias and/or datastream rows according to mode.
func collectTargets(ctx context.Context, client *esclient.Client, mode string) ([]lsRow, error) {
	var rows []lsRow
	if mode == listAll || mode == listAliases {
		aliases, err := client.ListAliases(ctx)
		if err != nil {
			return nil, err
		}
		sort.Slice(aliases, func(i, j int) bool { return aliases[i].Name < aliases[j].Name })
		for _, a := range aliases {
			count := a.IndexCount
			rows = append(rows, lsRow{Name: a.Name, Type: listAliases, IndexCount: &count})
		}
	}
	if mode == listAll || mode == listDataStreams {
		streams, err := client.ListDataStreams(ctx)
		if err != nil {
			return nil, err
		}
		sort.Slice(streams, func(i, j int) bool { return streams[i].Name < streams[j].Name })
		for _, d := range streams {
			count := d.BackingIndicesCount
			rows = append(rows, lsRow{Name: d.Name, Type: listDataStreams, BackingIndicesCount: &count})
		}
	}
	return rows, nil
}

// renderLs writes ls rows in the requested format.
func renderLs(cmd *cobra.Command, format string, rows []lsRow) error {
	out := cmd.OutOrStdout()
	switch format {
	case output.FormatTable:
		tableRows := make([][]string, 0, len(rows))
		for _, r := range rows {
			tableRows = append(tableRows, []string{r.Name, r.Type, countString(r)})
		}
		return output.RenderTable(out, []string{"NAME", "TYPE", "COUNT"}, tableRows)
	case output.FormatJSONL:
		return output.WriteJSONLines(out, rows)
	default:
		if rows == nil {
			rows = []lsRow{}
		}
		return output.WriteJSON(out, rows)
	}
}

// countString renders the type-specific count for table output.
func countString(r lsRow) string {
	switch {
	case r.IndexCount != nil:
		return strconv.Itoa(*r.IndexCount)
	case r.BackingIndicesCount != nil:
		return strconv.Itoa(*r.BackingIndicesCount)
	default:
		return ""
	}
}
