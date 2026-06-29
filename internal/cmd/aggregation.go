package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/chenwei791129/es-log-cli/internal/esclient"
	"github.com/chenwei791129/es-log-cli/internal/output"
)

// defaultBucketSize is the terms bucket size used when --terms omits an explicit
// size.
const defaultBucketSize = 10

// metricOps is the allowed set of --metric operations.
var metricOps = map[string]bool{
	"sum":         true,
	"avg":         true,
	"min":         true,
	"max":         true,
	"value_count": true,
}

// intervalPattern validates a date_histogram interval like 5m, 1h, 1d, 1w, 1M, 1y.
// Case matters: 'm' is minutes (fixed) while 'M' is months (calendar).
var intervalPattern = regexp.MustCompile(`^[0-9]+[smhdwMy]$`)

// hasAggregation reports whether any aggregation flag (structured or raw) is set.
func (p searchParams) hasAggregation() bool {
	return p.hasStructuredAggregation() || p.rawAggs != ""
}

// hasStructuredAggregation reports whether any structured aggregation flag is set
// (i.e. anything other than the raw --aggs passthrough).
func (p searchParams) hasStructuredAggregation() bool {
	return p.terms != "" || p.dateHistogram != "" ||
		len(p.metrics) > 0 || len(p.cardinalities) > 0
}

// bucketed reports whether a bucketing flag (--terms or --date-histogram) is set.
func (p searchParams) bucketed() bool {
	return p.terms != "" || p.dateHistogram != ""
}

// validateAggregation enforces the aggregation flags' mutual exclusion and per-
// flag format rules, surfacing every failure as an exit-2 usage error before any
// request is built. It is a no-op when no aggregation flag is set.
func (p searchParams) validateAggregation() error {
	structured := p.hasStructuredAggregation()

	if p.rawAggs != "" {
		if structured {
			return newExitError(exitUsage,
				"--aggs is mutually exclusive with --terms/--date-histogram/--metric/--cardinality")
		}
		// A null/array/scalar value unmarshals into a map either with an error or
		// as a nil map; both mean the value is not a JSON object.
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(p.rawAggs), &obj); err != nil || obj == nil {
			return newExitError(exitUsage, "invalid --aggs: want a JSON object")
		}
		return nil
	}

	if !structured {
		return nil
	}
	if p.terms != "" && p.dateHistogram != "" {
		return newExitError(exitUsage, "--terms and --date-histogram are mutually exclusive")
	}
	if p.terms != "" {
		if _, _, err := parseTermsFlag(p.terms); err != nil {
			return err
		}
	}
	if p.dateHistogram != "" {
		if _, _, _, err := parseDateHistogramFlag(p.dateHistogram); err != nil {
			return err
		}
	}
	for _, m := range p.metrics {
		if _, _, err := parseMetricFlag(m); err != nil {
			return err
		}
	}
	if slices.Contains(p.cardinalities, "") {
		return newExitError(exitUsage, "invalid --cardinality: field must not be empty")
	}
	return nil
}

// parseTermsFlag parses --terms <field>[:<size>], defaulting size to 10. The
// returned error is an exit-2 usage error.
func parseTermsFlag(v string) (field string, size int, err error) {
	field, sizeStr, hasSize := strings.Cut(v, ":")
	if field == "" {
		return "", 0, newExitError(exitUsage, "invalid --terms %q: want <field>[:<size>]", v)
	}
	size = defaultBucketSize
	if hasSize {
		n, convErr := strconv.Atoi(sizeStr)
		if convErr != nil || n <= 0 {
			return "", 0, newExitError(exitUsage, "invalid --terms size %q: want a positive integer", sizeStr)
		}
		size = n
	}
	return field, size, nil
}

// parseDateHistogramFlag parses --date-histogram <field>:<interval>, mapping the
// interval suffix to the calendar/fixed interval key. The returned error is an
// exit-2 usage error.
func parseDateHistogramFlag(v string) (field, intervalKey, interval string, err error) {
	field, interval, ok := strings.Cut(v, ":")
	if !ok || field == "" || interval == "" {
		return "", "", "", newExitError(exitUsage, "invalid --date-histogram %q: want <field>:<interval>", v)
	}
	if !intervalPattern.MatchString(interval) {
		return "", "", "", newExitError(exitUsage,
			"invalid --date-histogram interval %q: want a value like 5m, 1h, 1d, 1w, 1M, 1y", interval)
	}
	// Reject a zero-magnitude interval (e.g. 0m, 00h): Elasticsearch rejects a
	// zero interval, so catch it here as a usage error like the --since path does.
	if magnitude := interval[:len(interval)-1]; strings.Trim(magnitude, "0") == "" {
		return "", "", "", newExitError(exitUsage,
			"invalid --date-histogram interval %q: interval must be greater than zero", interval)
	}
	switch interval[len(interval)-1] {
	case 's', 'm', 'h', 'd':
		intervalKey = "fixed_interval"
	default: // w, M, y — Elasticsearch calendar intervals accept only a multiplier of 1
		intervalKey = "calendar_interval"
		if interval[:len(interval)-1] != "1" {
			return "", "", "", newExitError(exitUsage,
				"invalid --date-histogram interval %q: calendar intervals (w/M/y) accept only a multiplier of 1, e.g. 1w, 1M, 1y", interval)
		}
	}
	return field, intervalKey, interval, nil
}

// parseMetricFlag parses --metric <op>:<field>, validating the op. The returned
// error is an exit-2 usage error.
func parseMetricFlag(v string) (op, field string, err error) {
	op, field, ok := strings.Cut(v, ":")
	if !ok || op == "" || field == "" {
		return "", "", newExitError(exitUsage, "invalid --metric %q: want <op>:<field>", v)
	}
	if !metricOps[op] {
		return "", "", newExitError(exitUsage,
			"invalid --metric op %q: want one of sum, avg, min, max, value_count", op)
	}
	return op, field, nil
}

// metricName is the fixed aggregation name for a metric: <op>_<field>.
func metricName(op, field string) string { return op + "_" + field }

// cardinalityName is the fixed aggregation name for a cardinality: cardinality_<field>.
func cardinalityName(field string) string { return "cardinality_" + field }

// aggMetric is one parsed metric/cardinality sub-aggregation: its fixed output
// name and its Elasticsearch sub-aggregation body.
type aggMetric struct {
	name string
	agg  map[string]any
}

// parsedMetrics parses --metric and --cardinality into ordered, deduped sub-
// aggregations — the single source of truth for both the request body and the
// rendered column/key names, so the two can never diverge. Parse errors are
// dropped because validateAggregation rejects malformed flags before any builder
// or renderer runs. Colliding names (e.g. a repeated --metric) collapse to one
// entry, matching the map-keyed request body.
func (p searchParams) parsedMetrics() []aggMetric {
	out := make([]aggMetric, 0, len(p.metrics)+len(p.cardinalities))
	seen := make(map[string]bool)
	add := func(name string, agg map[string]any) {
		if !seen[name] {
			seen[name] = true
			out = append(out, aggMetric{name: name, agg: agg})
		}
	}
	for _, m := range p.metrics {
		if op, field, err := parseMetricFlag(m); err == nil {
			add(metricName(op, field), map[string]any{op: map[string]any{"field": field}})
		}
	}
	for _, c := range p.cardinalities {
		add(cardinalityName(c), map[string]any{"cardinality": map[string]any{"field": c}})
	}
	return out
}

// metricNames lists the structured metric/cardinality aggregation names in flag
// order, used to flatten buckets and build table columns.
func (p searchParams) metricNames() []string {
	parsed := p.parsedMetrics()
	names := make([]string, len(parsed))
	for i, m := range parsed {
		names[i] = m.name
	}
	return names
}

// metricAggMap builds the metric/cardinality sub-aggregations keyed by their
// fixed names.
func (p searchParams) metricAggMap() map[string]any {
	parsed := p.parsedMetrics()
	out := make(map[string]any, len(parsed))
	for _, m := range parsed {
		out[m.name] = m.agg
	}
	return out
}

// bucketAgg builds the single bucketing aggregation (terms or date_histogram), or
// nil when no bucketing flag is set.
func (p searchParams) bucketAgg() (map[string]any, error) {
	switch {
	case p.terms != "":
		field, size, err := parseTermsFlag(p.terms)
		if err != nil {
			return nil, err
		}
		return map[string]any{"terms": map[string]any{"field": field, "size": size}}, nil
	case p.dateHistogram != "":
		field, intervalKey, interval, err := parseDateHistogramFlag(p.dateHistogram)
		if err != nil {
			return nil, err
		}
		return map[string]any{"date_histogram": map[string]any{"field": field, intervalKey: interval}}, nil
	default:
		return nil, nil
	}
}

// buildAggs builds the aggs subtree. For raw mode it returns the supplied object
// verbatim; for structured mode it builds a single-level bucket with in-bucket
// metric sub-aggregations, or top-level metrics when no bucketing flag is set.
func (p searchParams) buildAggs() (any, error) {
	if p.rawAggs != "" {
		return json.RawMessage(p.rawAggs), nil
	}
	metrics := p.metricAggMap()
	bucket, err := p.bucketAgg()
	if err != nil {
		return nil, err
	}
	if bucket != nil {
		if len(metrics) > 0 {
			bucket["aggs"] = metrics
		}
		return map[string]any{"group": bucket}, nil
	}
	return metrics, nil
}

// buildAggBody constructs the aggregation _search request body: the shared
// query/range, the aggs subtree, and a hit size that defaults to 0. Sort is
// included only when hits are requested (size > 0).
func (p searchParams) buildAggBody(size int) ([]byte, error) {
	aggs, err := p.buildAggs()
	if err != nil {
		return nil, err
	}
	body := p.baseBody(size)
	body["aggs"] = aggs
	if size > 0 {
		body["sort"] = p.sortClause()
	}
	return json.Marshal(body)
}

// runAggregation issues a single aggregation _search, surfaces any partial shard
// failure, and renders the result. Hits default to size 0 (buckets only) unless
// --size was given explicitly.
func runAggregation(cmd *cobra.Command, p *searchParams, client *esclient.Client, format string, printer *output.Printer) error {
	size := 0
	if p.sizeExplicit {
		size = p.size
	}
	// Mirror the hit path: a size beyond max_result_window is capped with a
	// warning so the request returns its buckets instead of an opaque ES 400.
	if size > defaultWindow {
		window := lookupWindow(cmd.Context(), client, p.target)
		if size > window {
			printer.Warnf("requested size %d exceeds max_result_window %d; capping to %d", size, window, window)
			size = window
		}
	}
	body, err := p.buildAggBody(size)
	if err != nil {
		return err
	}
	resp, err := client.Search(cmd.Context(), p.target, body)
	if err != nil {
		return classifyESError(p.target, err)
	}
	if resp.Shards.Failed > 0 {
		printer.Warnf("warning: %d shard(s) failed: %s", resp.Shards.Failed, shardFailureReason(resp.Shards.Failures))
	}
	return renderAggregation(cmd.OutOrStdout(), format, p, resp)
}

// shardFailureReason extracts a human-readable reason from the first shard
// failure, falling back to the raw failure JSON when the shape is unexpected.
func shardFailureReason(failures []json.RawMessage) string {
	if len(failures) == 0 {
		return "unknown reason"
	}
	var f struct {
		Reason struct {
			Type   string `json:"type"`
			Reason string `json:"reason"`
		} `json:"reason"`
	}
	if err := json.Unmarshal(failures[0], &f); err == nil {
		switch {
		case f.Reason.Reason != "":
			return f.Reason.Reason
		case f.Reason.Type != "":
			return f.Reason.Type
		}
	}
	return string(failures[0])
}

// aggJSON is the json output document for aggregation results: the query total,
// the aggregations block verbatim, and any hits (empty when size is 0).
type aggJSON struct {
	Total        int             `json:"total"`
	Aggregations json.RawMessage `json:"aggregations"`
	Hits         []searchHitJSON `json:"hits"`
}

// renderAggregation dispatches aggregation rendering by output format.
func renderAggregation(out io.Writer, format string, p *searchParams, resp *esclient.SearchResponse) error {
	switch format {
	case output.FormatJSON:
		return renderAggJSON(out, resp)
	case output.FormatTable:
		return renderAggTable(out, p, resp)
	default: // jsonl
		return renderAggJSONL(out, p, resp)
	}
}

// renderAggJSON writes {"total","aggregations","hits"}; hits is empty when the
// request used size 0.
func renderAggJSON(out io.Writer, resp *esclient.SearchResponse) error {
	return output.WriteJSON(out, aggJSON{
		Total:        resp.Hits.Total.Value,
		Aggregations: resp.Aggregations,
		Hits:         hitsToJSON(resp.Hits.Hits),
	})
}

// renderAggJSONL writes one flattened object per bucket (structured bucketing),
// a single object of metric values (structured top-level metric), or the raw
// aggregations object on one line (raw mode).
func renderAggJSONL(out io.Writer, p *searchParams, resp *esclient.SearchResponse) error {
	if p.rawAggs != "" {
		return output.WriteRawJSONLines(out, [][]byte{resp.Aggregations})
	}
	_, rows, err := p.structuredAggRows(resp.Aggregations)
	if err != nil {
		return err
	}
	lines := make([][]byte, 0, len(rows))
	for _, row := range rows {
		line, err := marshalOrderedLine(row)
		if err != nil {
			return err
		}
		lines = append(lines, line)
	}
	return output.WriteRawJSONLines(out, lines)
}

// renderAggTable writes aligned columns for structured modes (key/doc_count/
// metrics for bucketing, a single metric row otherwise) and the raw aggregations
// JSON for raw mode.
func renderAggTable(out io.Writer, p *searchParams, resp *esclient.SearchResponse) error {
	if p.rawAggs != "" {
		return output.WriteJSON(out, resp.Aggregations)
	}
	headers, rows, err := p.structuredAggRows(resp.Aggregations)
	if err != nil {
		return err
	}
	strRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		cells := make([]string, 0, len(row))
		for _, pair := range row {
			cells = append(cells, aggCell(pair.val))
		}
		strRows = append(strRows, cells)
	}
	return output.RenderTable(out, headers, strRows)
}

// structuredAggRows turns the structured aggregations block into a normalized
// set of ordered key/value rows shared by the jsonl and table renderers: one row
// per bucket (key/doc_count/metrics) when bucketing, or a single metric-values
// row otherwise. headers carries the column names in the same order.
func (p searchParams) structuredAggRows(aggregations json.RawMessage) (headers []string, rows [][]aggPair, err error) {
	names := p.metricNames()
	if p.bucketed() {
		headers = append([]string{"key", "doc_count"}, names...)
		// A 200 response can omit the aggregations block (e.g. when every shard
		// failed — already warned upstream). Emit headers but no rows rather than
		// failing the render with a parse error.
		if len(aggregations) == 0 {
			return headers, nil, nil
		}
		buckets, err := extractBuckets(aggregations)
		if err != nil {
			return nil, nil, err
		}
		useKeyAsString := p.dateHistogram != ""
		rows = make([][]aggPair, 0, len(buckets))
		for _, b := range buckets {
			row := make([]aggPair, 0, 2+len(names))
			row = append(row, aggPair{"key", bucketKey(b, useKeyAsString)})
			row = append(row, aggPair{"doc_count", b["doc_count"]})
			for _, name := range names {
				row = append(row, aggPair{name, extractMetricValue(b[name])})
			}
			rows = append(rows, row)
		}
		return headers, rows, nil
	}
	if len(aggregations) == 0 {
		return names, nil, nil
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(aggregations, &top); err != nil {
		return nil, nil, fmt.Errorf("parse aggregations: %w", err)
	}
	row := make([]aggPair, 0, len(names))
	for _, name := range names {
		row = append(row, aggPair{name, extractMetricValue(top[name])})
	}
	return names, [][]aggPair{row}, nil
}

// extractBuckets parses the buckets of the canonical-named group aggregation.
func extractBuckets(aggregations json.RawMessage) ([]map[string]json.RawMessage, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(aggregations, &top); err != nil {
		return nil, fmt.Errorf("parse aggregations: %w", err)
	}
	groupRaw, ok := top["group"]
	if !ok {
		return nil, nil
	}
	var group struct {
		Buckets []map[string]json.RawMessage `json:"buckets"`
	}
	if err := json.Unmarshal(groupRaw, &group); err != nil {
		return nil, fmt.Errorf("parse buckets: %w", err)
	}
	return group.Buckets, nil
}

// bucketKey returns the bucket's key, preferring key_as_string for date_histogram
// buckets when present.
func bucketKey(b map[string]json.RawMessage, useKeyAsString bool) json.RawMessage {
	if useKeyAsString {
		if v, ok := b["key_as_string"]; ok {
			return v
		}
	}
	return b["key"]
}

// aggPair is one ordered key/raw-value pair for line and column rendering.
type aggPair struct {
	key string
	val json.RawMessage
}

// marshalOrderedLine builds a compact JSON object preserving pair order, emitting
// null for absent values.
func marshalOrderedLine(pairs []aggPair) ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, p := range pairs {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, err := json.Marshal(p.key)
		if err != nil {
			return nil, err
		}
		b.Write(kb)
		b.WriteByte(':')
		if len(p.val) == 0 {
			b.WriteString("null")
			continue
		}
		if err := json.Compact(&b, p.val); err != nil {
			return nil, err
		}
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// extractMetricValue reduces a metric sub-aggregation ({"value":X}) to its scalar
// value, or null when absent/unparseable.
func extractMetricValue(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("null")
	}
	var obj struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return json.RawMessage("null")
	}
	if obj.Value != nil {
		return obj.Value
	}
	return raw
}

// aggCell renders a raw JSON scalar as a table cell: strings unquoted, other
// literals (numbers, bool, null) verbatim.
func aggCell(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}
