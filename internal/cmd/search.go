package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/chenwei791129/es-log-cli/internal/esclient"
	"github.com/chenwei791129/es-log-cli/internal/output"
)

// defaultWindow is the Elasticsearch default max_result_window and the fallback
// used when GetMaxResultWindow is unavailable.
const defaultWindow = 10000

// sincePattern validates relative-duration values like 15m, 1h, 24h, 7d.
var sincePattern = regexp.MustCompile(`^[0-9]+[smhd]$`)

// searchParams holds parsed search flags used to build the request body.
type searchParams struct {
	target   string
	query    string
	since    string
	from     string
	to       string
	tsField  string
	sortFlag string
	fields   []string
	size     int
	includes []string
	excludes []string
	// trackTotal requests an exact hit count (track_total_hits) so json output's
	// total is accurate beyond 10000; left off for jsonl/table to avoid the cost.
	trackTotal bool
}

// newSearchCommand builds the `search` subcommand.
func newSearchCommand(opts *globalOptions) *cobra.Command {
	p := &searchParams{}
	cmd := &cobra.Command{
		Use:   "search [target]",
		Short: "Search logs in a target (alias or datastream)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && p.target == "" {
				p.target = args[0]
			}
			return runSearch(cmd, opts, p)
		},
	}
	f := cmd.Flags()
	f.StringVarP(&p.target, "target", "t", "", "target alias or datastream to search")
	f.StringVarP(&p.query, "query", "q", "", "Lucene query string (default match_all)")
	f.StringVar(&p.since, "since", "", "relative time range, e.g. 15m, 1h, 24h, 7d")
	f.StringVar(&p.from, "from", "", "absolute range start (RFC3339)")
	f.StringVar(&p.to, "to", "", "absolute range end (RFC3339)")
	f.StringVar(&p.tsField, "timestamp-field", "@timestamp", "timestamp field for range filter and default sort")
	f.StringVar(&p.sortFlag, "sort", "", "sort as field:asc|desc (default <timestamp-field>:desc)")
	f.StringSliceVar(&p.fields, "fields", nil, "comma-separated _source fields to return")
	f.IntVarP(&p.size, "size", "n", 50, "max hits to fetch; 0 fetches all via pagination")
	f.StringArrayVarP(&p.includes, "include", "i", nil, "keep hits whose _source JSON matches (repeatable)")
	f.StringArrayVarP(&p.excludes, "exclude", "e", nil, "drop hits whose _source JSON matches (repeatable)")

	// --limit is an alias for --size.
	f.SetNormalizeFunc(func(_ *pflag.FlagSet, name string) pflag.NormalizedName {
		if name == "limit" {
			name = "size"
		}
		return pflag.NormalizedName(name)
	})
	return cmd
}

// validate checks all search flags for usage errors before any work is done,
// surfacing them as exit-code-2 errors rather than opaque Elasticsearch 400s.
func (p *searchParams) validate() error {
	if p.target == "" {
		return newExitError(exitUsage, "no target: pass --target/-t or a positional argument")
	}
	if p.size < 0 {
		return newExitError(exitUsage, "invalid --size %d: must be 0 (all) or a positive number", p.size)
	}
	if p.since != "" && (p.from != "" || p.to != "") {
		return newExitError(exitUsage, "--since is mutually exclusive with --from/--to")
	}
	if p.since != "" {
		if !sincePattern.MatchString(p.since) {
			return newExitError(exitUsage, "invalid --since %q: want a value like 15m, 1h, 24h, 7d", p.since)
		}
		// Reject any zero-magnitude duration ("0s", "00m", "000h"): the numeric
		// prefix is all zeros, which would produce an empty now-to-now window.
		if magnitude := p.since[:len(p.since)-1]; strings.Trim(magnitude, "0") == "" {
			return newExitError(exitUsage, "invalid --since %q: duration must be greater than zero", p.since)
		}
	}
	from, to, err := p.parseAbsoluteRange()
	if err != nil {
		return err
	}
	if from != nil && to != nil && from.After(*to) {
		return newExitError(exitUsage, "--from %q is after --to %q", p.from, p.to)
	}
	return p.validateSort()
}

// parseAbsoluteRange validates and parses the --from/--to RFC3339 bounds.
func (p *searchParams) parseAbsoluteRange() (from, to *time.Time, err error) {
	for _, v := range []struct {
		name string
		val  string
		dst  **time.Time
	}{{"--from", p.from, &from}, {"--to", p.to, &to}} {
		if v.val == "" {
			continue
		}
		ts, perr := time.Parse(time.RFC3339, v.val)
		if perr != nil {
			return nil, nil, newExitError(exitUsage, "invalid %s %q: want an RFC3339 timestamp", v.name, v.val)
		}
		*v.dst = &ts
	}
	return from, to, nil
}

// validateSort checks the --sort flag's field and direction when set.
func (p *searchParams) validateSort() error {
	if p.sortFlag == "" {
		return nil
	}
	field, dir, hasDir := strings.Cut(p.sortFlag, ":")
	if field == "" {
		return newExitError(exitUsage, "invalid --sort %q: missing field name", p.sortFlag)
	}
	if hasDir && dir != "asc" && dir != "desc" {
		return newExitError(exitUsage, "invalid --sort direction %q: want asc or desc", dir)
	}
	return nil
}

// runSearch validates flags, executes the search (single or paginated), applies
// client-side filtering, and renders the result.
func runSearch(cmd *cobra.Command, opts *globalOptions, p *searchParams) error {
	if err := p.validate(); err != nil {
		return err
	}
	includes, err := compileRegexes(p.includes)
	if err != nil {
		return newExitError(exitUsage, "invalid --include pattern: %v", err)
	}
	excludes, err := compileRegexes(p.excludes)
	if err != nil {
		return newExitError(exitUsage, "invalid --exclude pattern: %v", err)
	}

	cfg, err := opts.loadConfig()
	if err != nil {
		return err
	}
	format, err := opts.formatFor("search")
	if err != nil {
		return err
	}
	p.trackTotal = format == output.FormatJSON
	client, _, err := opts.buildClient(cfg)
	if err != nil {
		return err
	}

	printer := opts.printer(cmd)
	hits, total, err := executeSearch(cmd.Context(), client, p, printer)
	if err != nil {
		return classifyESError(p.target, err)
	}

	hits = filterHits(hits, includes, excludes)
	return renderSearch(cmd, format, hits, total, p.tsField)
}

// executeSearch runs either a single bounded search or full search_after
// pagination, returning the collected hits and the Elasticsearch total.
func executeSearch(ctx context.Context, client *esclient.Client, p *searchParams, printer *output.Printer) ([]esclient.Hit, int, error) {
	switch {
	case p.size == 0:
		return paginateAll(ctx, client, p)
	case p.size > defaultWindow:
		window := lookupWindow(ctx, client, p.target)
		size := p.size
		if p.size > window {
			printer.Warnf("requested size %d exceeds max_result_window %d; capping to %d", p.size, window, window)
			size = window
		}
		return singleSearch(ctx, client, p, size)
	default:
		return singleSearch(ctx, client, p, p.size)
	}
}

// singleSearch issues one bounded search.
func singleSearch(ctx context.Context, client *esclient.Client, p *searchParams, size int) ([]esclient.Hit, int, error) {
	body, err := p.buildBody(size, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := client.Search(ctx, p.target, body)
	if err != nil {
		return nil, 0, err
	}
	return resp.Hits.Hits, resp.Hits.Total.Value, nil
}

// paginateAll retrieves every matching hit via search_after, paging with size=W.
func paginateAll(ctx context.Context, client *esclient.Client, p *searchParams) ([]esclient.Hit, int, error) {
	window := lookupWindow(ctx, client, p.target)
	var (
		all         []esclient.Hit
		searchAfter []json.RawMessage
		total       int
		firstPage   = true
	)
	for {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		body, err := p.buildBody(window, searchAfter)
		if err != nil {
			return nil, 0, err
		}
		resp, err := client.Search(ctx, p.target, body)
		if err != nil {
			return nil, 0, err
		}
		if firstPage {
			total = resp.Hits.Total.Value
			firstPage = false
		}
		n := len(resp.Hits.Hits)
		if n == 0 {
			break
		}
		all = append(all, resp.Hits.Hits...)
		searchAfter = resp.Hits.Hits[n-1].Sort
		if n < window || len(searchAfter) == 0 {
			break
		}
	}
	return all, total, nil
}

// lookupWindow resolves the max_result_window for target, falling back to the
// default of 10000 on any failure or missing value.
func lookupWindow(ctx context.Context, client *esclient.Client, target string) int {
	w, found, err := client.GetMaxResultWindow(ctx, target)
	if err != nil || !found {
		return defaultWindow
	}
	return w
}

// buildBody constructs the _search request body for the given page size and
// optional search_after cursor.
func (p searchParams) buildBody(size int, searchAfter []json.RawMessage) ([]byte, error) {
	var queryPart map[string]any
	if p.query != "" {
		queryPart = map[string]any{"query_string": map[string]any{"query": p.query}}
	} else {
		queryPart = map[string]any{"match_all": map[string]any{}}
	}

	body := map[string]any{
		"size": size,
		"sort": p.sortClause(),
	}
	if rng := p.rangeFilter(); rng != nil {
		body["query"] = map[string]any{
			"bool": map[string]any{
				"must":   []any{queryPart},
				"filter": []any{map[string]any{"range": rng}},
			},
		}
	} else {
		body["query"] = queryPart
	}
	if len(p.fields) > 0 {
		body["_source"] = p.fields
	}
	if p.trackTotal {
		body["track_total_hits"] = true
	}
	if searchAfter != nil {
		body["search_after"] = searchAfter
	}
	return json.Marshal(body)
}

// rangeFilter builds the range clause keyed by the timestamp field, or nil when
// no time bounds are set.
func (p searchParams) rangeFilter() map[string]any {
	if p.since == "" && p.from == "" && p.to == "" {
		return nil
	}
	bounds := map[string]any{}
	if p.since != "" {
		bounds["gte"] = "now-" + p.since
	} else {
		if p.from != "" {
			bounds["gte"] = p.from
		}
		if p.to != "" {
			bounds["lte"] = p.to
		}
	}
	return map[string]any{p.tsField: bounds}
}

// sortClause returns the sort array with a trailing _doc tiebreaker for stable
// search_after pagination. The default sort targets the timestamp field.
func (p searchParams) sortClause() []any {
	field, dir := p.tsField, "desc"
	if p.sortFlag != "" {
		f, d, hasDir := strings.Cut(p.sortFlag, ":")
		field = f
		if hasDir && d != "" {
			dir = d
		}
	}
	return []any{
		map[string]any{field: dir},
		map[string]any{"_doc": "asc"},
	}
}

// filterHits applies client-side include/exclude regex filtering against each
// hit's serialized _source JSON. With no patterns set (the common case) it
// returns the hits untouched, avoiding per-hit serialization.
func filterHits(hits []esclient.Hit, includes, excludes []*regexp.Regexp) []esclient.Hit {
	if len(includes) == 0 && len(excludes) == 0 {
		return hits
	}
	out := make([]esclient.Hit, 0, len(hits))
	for _, h := range hits {
		src := compactJSON(h.Source)
		if len(includes) > 0 && !matchesAny(includes, src) {
			continue
		}
		if matchesAny(excludes, src) {
			continue
		}
		out = append(out, h)
	}
	return out
}

// matchesAny reports whether s matches any of the patterns.
func matchesAny(patterns []*regexp.Regexp, s []byte) bool {
	for _, re := range patterns {
		if re.Match(s) {
			return true
		}
	}
	return false
}

// compileRegexes compiles a list of regex pattern strings.
func compileRegexes(patterns []string) ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, err
		}
		out = append(out, re)
	}
	return out, nil
}

// searchHitJSON is one hit in the json output, carrying envelope and source.
type searchHitJSON struct {
	ID     string          `json:"_id"`
	Index  string          `json:"_index"`
	Score  *float64        `json:"_score"`
	Source json.RawMessage `json:"_source"`
}

// searchJSON is the json output document for search.
type searchJSON struct {
	Total int             `json:"total"`
	Hits  []searchHitJSON `json:"hits"`
}

// renderSearch writes search results in the requested format.
func renderSearch(cmd *cobra.Command, format string, hits []esclient.Hit, total int, tsField string) error {
	out := cmd.OutOrStdout()
	switch format {
	case output.FormatJSON:
		doc := searchJSON{Total: total, Hits: make([]searchHitJSON, 0, len(hits))}
		for _, h := range hits {
			doc.Hits = append(doc.Hits, searchHitJSON{ID: h.ID, Index: h.Index, Score: h.Score, Source: h.Source})
		}
		return output.WriteJSON(out, doc)
	case output.FormatTable:
		return renderSearchTable(out, hits, tsField)
	default: // jsonl: each line is the bare _source
		sources := make([][]byte, 0, len(hits))
		for _, h := range hits {
			sources = append(sources, h.Source)
		}
		return output.WriteRawJSONLines(out, sources)
	}
}

// renderSearchTable renders hits as aligned columns of _id, _index, the
// timestamp field, and message. Absent fields render as empty strings.
func renderSearchTable(out io.Writer, hits []esclient.Hit, tsField string) error {
	rows := make([][]string, 0, len(hits))
	for _, h := range hits {
		// UseNumber keeps JSON numbers as their literal text, so epoch values do
		// not render in scientific float notation.
		dec := json.NewDecoder(bytes.NewReader(h.Source))
		dec.UseNumber()
		var src map[string]any
		_ = dec.Decode(&src)
		rows = append(rows, []string{h.ID, h.Index, sourceField(src, tsField), sourceField(src, "message")})
	}
	return output.RenderTable(out, []string{"_id", "_index", tsField, "message"}, rows)
}

// sourceField renders a _source field as a string for table output, yielding ""
// when absent (so missing fields are blank, not "<nil>"). Dotted keys traverse
// nested objects (e.g. "event.created"); non-scalar values render as compact
// JSON rather than Go map/slice syntax. RenderTable handles whitespace
// sanitization at the shared choke point.
func sourceField(src map[string]any, key string) string {
	v, ok := lookupNested(src, key)
	if !ok || v == nil {
		return ""
	}
	switch v.(type) {
	case map[string]any, []any:
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
	}
	return fmt.Sprintf("%v", v)
}

// lookupNested resolves a possibly-dotted key against nested JSON objects,
// falling back to a flat lookup of the literal key when present.
func lookupNested(src map[string]any, key string) (any, bool) {
	if v, ok := src[key]; ok {
		return v, true
	}
	parts := strings.Split(key, ".")
	if len(parts) == 1 {
		return nil, false
	}
	var cur any = src
	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[p]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// compactJSON returns the compact form of a JSON value, or the raw bytes if it
// cannot be compacted.
func compactJSON(raw json.RawMessage) []byte {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return raw
	}
	return buf.Bytes()
}
