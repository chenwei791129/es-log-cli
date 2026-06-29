package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// mustAggs marshals the aggs subtree produced by buildAggs for a params value.
func mustAggs(t *testing.T, p searchParams) []byte {
	t.Helper()
	aggs, err := p.buildAggs()
	if err != nil {
		t.Fatalf("buildAggs: %v", err)
	}
	b, err := json.Marshal(aggs)
	if err != nil {
		t.Fatalf("marshal aggs: %v", err)
	}
	return b
}

// assertJSONEqual compares got against want as semantically-equal JSON (key order
// independent).
func assertJSONEqual(t *testing.T, got []byte, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("got is not valid JSON: %v (%s)", err, got)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("want is not valid JSON: %v", err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("JSON mismatch:\n got: %s\nwant: %s", got, want)
	}
}

// ---- structured aggregation flags (A) ----

// TestBuildAggsCanonical locks the design contract's canonical aggs body for
// terms + metric + cardinality.
func TestBuildAggsCanonical(t *testing.T) {
	p := searchParams{terms: "log.info:10", metrics: []string{"sum:bytes"}, cardinalities: []string{"client.ip"}}
	want := `{"group":{"terms":{"field":"log.info","size":10},"aggs":{"sum_bytes":{"sum":{"field":"bytes"}},"cardinality_client.ip":{"cardinality":{"field":"client.ip"}}}}}`
	assertJSONEqual(t, mustAggs(t, p), want)
}

// TestBuildAggsTermsDefaultSize asserts the bucket size defaults to 10 when
// omitted from the --terms value.
func TestBuildAggsTermsDefaultSize(t *testing.T) {
	p := searchParams{terms: "service"}
	want := `{"group":{"terms":{"field":"service","size":10}}}`
	assertJSONEqual(t, mustAggs(t, p), want)
}

// TestBuildAggsDateHistogramFixed asserts s/m/h/d intervals map to fixed_interval.
func TestBuildAggsDateHistogramFixed(t *testing.T) {
	p := searchParams{dateHistogram: "@timestamp:5m"}
	want := `{"group":{"date_histogram":{"field":"@timestamp","fixed_interval":"5m"}}}`
	assertJSONEqual(t, mustAggs(t, p), want)
}

// TestBuildAggsDateHistogramCalendar asserts w/M/y intervals map to
// calendar_interval.
func TestBuildAggsDateHistogramCalendar(t *testing.T) {
	p := searchParams{dateHistogram: "@timestamp:1M"}
	want := `{"group":{"date_histogram":{"field":"@timestamp","calendar_interval":"1M"}}}`
	assertJSONEqual(t, mustAggs(t, p), want)
}

// TestBuildAggsTopLevelMetric asserts a metric with no bucketing flag becomes a
// top-level aggregation with no group bucket.
func TestBuildAggsTopLevelMetric(t *testing.T) {
	p := searchParams{metrics: []string{"sum:bytes"}}
	want := `{"sum_bytes":{"sum":{"field":"bytes"}}}`
	assertJSONEqual(t, mustAggs(t, p), want)
}

// ---- raw aggregation passthrough (B) ----

// TestBuildAggBodyRawPassthrough asserts --aggs is placed verbatim under aggs and
// es-log still wraps it with the shared query/range and the default size 0.
func TestBuildAggBodyRawPassthrough(t *testing.T) {
	p := searchParams{target: "netflow", since: "1h", tsField: "@timestamp",
		rawAggs: `{"top_isp":{"terms":{"field":"isp"}}}`}
	body, err := p.buildAggBody(0)
	if err != nil {
		t.Fatal(err)
	}
	m := parseBody(t, body)
	aggsBytes, err := json.Marshal(m["aggs"])
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, aggsBytes, `{"top_isp":{"terms":{"field":"isp"}}}`)
	if rng := digRange(t, m, "@timestamp"); rng["gte"] != "now-1h" {
		t.Errorf("range gte = %v, want now-1h", rng["gte"])
	}
	if size, ok := m["size"].(float64); !ok || size != 0 {
		t.Errorf("size = %v, want 0", m["size"])
	}
}

// ---- flag validation and mutual exclusion ----

// TestAggregationValidation asserts every invalid aggregation flag combination is
// rejected with exit code 2 during validate, before any request is built.
func TestAggregationValidation(t *testing.T) {
	base := func() searchParams {
		return searchParams{target: "app-logs", tsField: "@timestamp", size: 50}
	}
	cases := []struct {
		name   string
		mutate func(*searchParams)
	}{
		{"structured with raw aggs", func(p *searchParams) {
			p.terms = "host"
			p.rawAggs = `{"x":{"max":{"field":"y"}}}`
		}},
		{"metric with raw aggs", func(p *searchParams) {
			p.metrics = []string{"sum:bytes"}
			p.rawAggs = `{"x":{"max":{"field":"y"}}}`
		}},
		{"two bucketing flags", func(p *searchParams) {
			p.terms = "host"
			p.dateHistogram = "@timestamp:5m"
		}},
		{"unknown metric op", func(p *searchParams) { p.metrics = []string{"median:bytes"} }},
		{"metric missing field", func(p *searchParams) { p.metrics = []string{"sum"} }},
		{"terms zero size", func(p *searchParams) { p.terms = "host:0" }},
		{"terms non-integer size", func(p *searchParams) { p.terms = "host:abc" }},
		{"date-histogram missing interval", func(p *searchParams) { p.dateHistogram = "@timestamp" }},
		{"date-histogram bad interval", func(p *searchParams) { p.dateHistogram = "@timestamp:5x" }},
		{"date-histogram calendar multiplier", func(p *searchParams) { p.dateHistogram = "@timestamp:2w" }},
		{"date-histogram zero interval", func(p *searchParams) { p.dateHistogram = "@timestamp:0m" }},
		{"raw aggs not object", func(p *searchParams) { p.rawAggs = `[1,2,3]` }},
		{"raw aggs null", func(p *searchParams) { p.rawAggs = `null` }},
		{"raw aggs invalid json", func(p *searchParams) { p.rawAggs = `{not json}` }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := base()
			c.mutate(&p)
			err := p.validate()
			if err == nil {
				t.Fatalf("expected validation error, got nil")
			}
			var ee *exitError
			if !errors.As(err, &ee) || ee.ExitCode() != exitUsage {
				t.Errorf("want exit %d, got %v", exitUsage, err)
			}
		})
	}
}

// aggResponse is a minimal aggregation _search response with one terms bucket.
const aggResponse = `{"_shards":{"failed":0},"hits":{"total":{"value":3},"hits":[]},` +
	`"aggregations":{"group":{"buckets":[{"key":"svc-a","doc_count":3,"sum_bytes":{"value":12}}]}}}`

// TestAggregationRequestBody asserts the aggregation request carries the shared
// bool query + range alongside the generated aggs, and that size defaults to 0
// but is overridden by an explicit --size.
func TestAggregationRequestBody(t *testing.T) {
	run := func(args ...string) recordedReq {
		stub := newESStub(t, func(recordedReq) (int, string) { return 200, aggResponse })
		cfg := writeTestConfig(t, stub.url())
		full := append([]string{"search", "-c", "test", "-t", "app-logs", "--config", cfg}, args...)
		res := runCLI(t, context.Background(), full...)
		if res.code != 0 {
			t.Fatalf("exit %d: %s", res.code, res.stderr)
		}
		return stub.searchRequests()[0]
	}

	// bool query + range + aggs all present.
	m := parseBody(t, run("-q", "level:error", "--since", "24h", "--terms", "service").body)
	q := m["query"].(map[string]any)
	if _, ok := q["bool"].(map[string]any); !ok {
		t.Errorf("expected bool query, got %v", q)
	}
	if rng := digRange(t, m, "@timestamp"); rng["gte"] != "now-24h" {
		t.Errorf("range gte = %v, want now-24h", rng["gte"])
	}
	if _, ok := m["aggs"].(map[string]any); !ok {
		t.Errorf("expected aggs in body, got %v", m["aggs"])
	}
	if size, ok := m["size"].(float64); !ok || size != 0 {
		t.Errorf("default aggregation size = %v, want 0", m["size"])
	}

	// explicit --size overrides the default and still carries aggs.
	m2 := parseBody(t, run("--terms", "service", "--size", "5").body)
	if size, ok := m2["size"].(float64); !ok || size != 5 {
		t.Errorf("overridden size = %v, want 5", m2["size"])
	}
	if _, ok := m2["aggs"].(map[string]any); !ok {
		t.Errorf("aggs missing with --size override: %v", m2["aggs"])
	}
}

// TestAggregationMutualExclusionCommand asserts a structured flag combined with
// --aggs exits 2 without issuing any request (spec mutual-exclusion scenario).
func TestAggregationMutualExclusionCommand(t *testing.T) {
	stub := newESStub(t, func(recordedReq) (int, string) { return 200, aggResponse })
	cfg := writeTestConfig(t, stub.url())
	res := runCLI(t, context.Background(), "search", "-c", "test", "-t", "app-logs",
		"--terms", "host", "--aggs", `{"x":{"max":{"field":"y"}}}`, "--config", cfg)
	if res.code != 2 {
		t.Errorf("exit %d, want 2 (stderr=%s)", res.code, res.stderr)
	}
	if len(stub.searchRequests()) != 0 {
		t.Errorf("no request should be issued, got %d", len(stub.searchRequests()))
	}
}

// ---- aggregation output rendering ----

// aggRun runs a search with the given args against a stub returning respBody.
func aggRun(t *testing.T, respBody string, args ...string) cliResult {
	t.Helper()
	stub := newESStub(t, func(recordedReq) (int, string) { return 200, respBody })
	cfg := writeTestConfig(t, stub.url())
	full := append([]string{"search", "-c", "test", "-t", "app-logs", "--config", cfg}, args...)
	return runCLI(t, context.Background(), full...)
}

// TestAggJSONShape asserts json output is {"total","aggregations","hits"} with
// empty hits at the default size 0.
func TestAggJSONShape(t *testing.T) {
	resp := `{"_shards":{"failed":0},"hits":{"total":{"value":42},"hits":[]},` +
		`"aggregations":{"group":{"buckets":[{"key":"svc-a","doc_count":42}]}}}`
	res := aggRun(t, resp, "--terms", "service", "-o", "json")
	if res.code != 0 {
		t.Fatal(res.stderr)
	}
	var doc struct {
		Total        int               `json:"total"`
		Aggregations json.RawMessage   `json:"aggregations"`
		Hits         []json.RawMessage `json:"hits"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &doc); err != nil {
		t.Fatalf("output not a JSON doc: %v\n%s", err, res.stdout)
	}
	if doc.Total != 42 {
		t.Errorf("total = %d, want 42", doc.Total)
	}
	if len(doc.Hits) != 0 {
		t.Errorf("hits should be empty at size 0, got %d", len(doc.Hits))
	}
	var aggs map[string]json.RawMessage
	if err := json.Unmarshal(doc.Aggregations, &aggs); err != nil {
		t.Fatalf("aggregations not an object: %v", err)
	}
	if _, ok := aggs["group"]; !ok {
		t.Errorf("aggregations missing group: %s", doc.Aggregations)
	}
}

// TestAggJSONLFlattensBuckets locks the spec example: a terms bucket with a metric
// sub-aggregation flattens to {"key","doc_count","<metric>"} with the metric
// reduced to its scalar value.
func TestAggJSONLFlattensBuckets(t *testing.T) {
	resp := `{"_shards":{"failed":0},"hits":{"total":{"value":42},"hits":[]},` +
		`"aggregations":{"group":{"buckets":[{"key":"timeout","doc_count":42,"sum_bytes":{"value":1024}}]}}}`
	res := aggRun(t, resp, "--terms", "log.info", "--metric", "sum:bytes", "-o", "jsonl")
	if res.code != 0 {
		t.Fatal(res.stderr)
	}
	got := strings.TrimSpace(res.stdout)
	want := `{"key":"timeout","doc_count":42,"sum_bytes":1024}`
	if got != want {
		t.Errorf("jsonl line = %q, want %q", got, want)
	}
}

// TestAggJSONLDateHistogramKey asserts date_histogram buckets use key_as_string as
// the flattened key.
func TestAggJSONLDateHistogramKey(t *testing.T) {
	resp := `{"_shards":{"failed":0},"hits":{"total":{"value":7},"hits":[]},` +
		`"aggregations":{"group":{"buckets":[{"key_as_string":"2026-06-29T00:00:00Z","key":1719619200000,"doc_count":7}]}}}`
	res := aggRun(t, resp, "--date-histogram", "@timestamp:1h", "-o", "jsonl")
	if res.code != 0 {
		t.Fatal(res.stderr)
	}
	got := strings.TrimSpace(res.stdout)
	want := `{"key":"2026-06-29T00:00:00Z","doc_count":7}`
	if got != want {
		t.Errorf("jsonl line = %q, want %q", got, want)
	}
}

// TestAggJSONLTopLevelMetric asserts a pure metric (no bucketing) renders a single
// flattened object.
func TestAggJSONLTopLevelMetric(t *testing.T) {
	resp := `{"_shards":{"failed":0},"hits":{"total":{"value":99},"hits":[]},` +
		`"aggregations":{"sum_bytes":{"value":2048}}}`
	res := aggRun(t, resp, "--metric", "sum:bytes", "-o", "jsonl")
	if res.code != 0 {
		t.Fatal(res.stderr)
	}
	got := strings.TrimSpace(res.stdout)
	want := `{"sum_bytes":2048}`
	if got != want {
		t.Errorf("jsonl line = %q, want %q", got, want)
	}
}

// TestAggJSONLRawMode asserts raw mode emits the aggregations object on a single
// line.
func TestAggJSONLRawMode(t *testing.T) {
	resp := `{"_shards":{"failed":0},"hits":{"total":{"value":5},"hits":[]},` +
		`"aggregations":{"top_isp":{"buckets":[{"key":"isp-a","doc_count":5}]}}}`
	res := aggRun(t, resp, "--aggs", `{"top_isp":{"terms":{"field":"isp"}}}`, "-o", "jsonl")
	if res.code != 0 {
		t.Fatal(res.stderr)
	}
	out := strings.TrimRight(res.stdout, "\n")
	if strings.Contains(out, "\n") {
		t.Errorf("raw jsonl should be a single line: %q", res.stdout)
	}
	var aggs map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &aggs); err != nil {
		t.Fatalf("raw jsonl not a JSON object: %v", err)
	}
	if _, ok := aggs["top_isp"]; !ok {
		t.Errorf("raw jsonl missing top_isp: %q", out)
	}
}

// TestAggJSONRawMode asserts the aggregations block is carried verbatim under json
// for raw mode (caller-named keys).
func TestAggJSONRawMode(t *testing.T) {
	resp := `{"_shards":{"failed":0},"hits":{"total":{"value":5},"hits":[]},` +
		`"aggregations":{"top_isp":{"buckets":[{"key":"isp-a","doc_count":5}]}}}`
	res := aggRun(t, resp, "--aggs", `{"top_isp":{"terms":{"field":"isp"}}}`, "-o", "json")
	if res.code != 0 {
		t.Fatal(res.stderr)
	}
	var doc struct {
		Aggregations map[string]json.RawMessage `json:"aggregations"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &doc); err != nil {
		t.Fatalf("output not a JSON doc: %v", err)
	}
	if _, ok := doc.Aggregations["top_isp"]; !ok {
		t.Errorf("json aggregations missing top_isp: %s", res.stdout)
	}
}

// TestAggTableBuckets asserts table output renders key/doc_count/metric columns
// for structured bucketing.
func TestAggTableBuckets(t *testing.T) {
	resp := `{"_shards":{"failed":0},"hits":{"total":{"value":3},"hits":[]},` +
		`"aggregations":{"group":{"buckets":[{"key":"svc-a","doc_count":3,"sum_bytes":{"value":12}}]}}}`
	res := aggRun(t, resp, "--terms", "service", "--metric", "sum:bytes", "-o", "table")
	if res.code != 0 {
		t.Fatal(res.stderr)
	}
	for _, want := range []string{"key", "doc_count", "sum_bytes", "svc-a", "3", "12"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("table output missing %q:\n%s", want, res.stdout)
		}
	}
}

// TestAggTableRawMode asserts raw mode renders the aggregations JSON under table.
func TestAggTableRawMode(t *testing.T) {
	resp := `{"_shards":{"failed":0},"hits":{"total":{"value":5},"hits":[]},` +
		`"aggregations":{"top_isp":{"buckets":[{"key":"isp-a","doc_count":5}]}}}`
	res := aggRun(t, resp, "--aggs", `{"top_isp":{"terms":{"field":"isp"}}}`, "-o", "table")
	if res.code != 0 {
		t.Fatal(res.stderr)
	}
	if !strings.Contains(res.stdout, "top_isp") {
		t.Errorf("raw table output missing top_isp:\n%s", res.stdout)
	}
}

// TestBuildAggBodyProjectsFields asserts --fields projects _source in the
// aggregation body so hits returned alongside aggregations carry only the
// requested fields.
func TestBuildAggBodyProjectsFields(t *testing.T) {
	p := searchParams{target: "app-logs", tsField: "@timestamp", terms: "service",
		fields: []string{"message", "level"}}
	body, err := p.buildAggBody(5)
	if err != nil {
		t.Fatal(err)
	}
	m := parseBody(t, body)
	src, ok := m["_source"].([]any)
	if !ok || len(src) != 2 || src[0] != "message" || src[1] != "level" {
		t.Errorf("_source projection = %v", m["_source"])
	}
}

// TestAggDuplicateMetricDedup asserts a repeated --metric collapses to a single
// column/key in the rendered output, matching the single sub-aggregation in the
// request body.
func TestAggDuplicateMetricDedup(t *testing.T) {
	resp := `{"_shards":{"failed":0},"hits":{"total":{"value":3},"hits":[]},` +
		`"aggregations":{"group":{"buckets":[{"key":"svc-a","doc_count":3,"sum_bytes":{"value":12}}]}}}`
	res := aggRun(t, resp, "--terms", "service", "--metric", "sum:bytes", "--metric", "sum:bytes", "-o", "jsonl")
	if res.code != 0 {
		t.Fatal(res.stderr)
	}
	got := strings.TrimSpace(res.stdout)
	want := `{"key":"svc-a","doc_count":3,"sum_bytes":12}`
	if got != want {
		t.Errorf("jsonl line = %q, want %q (duplicate metric not deduped)", got, want)
	}
}

// TestAggMissingAggregationsGraceful asserts a 200 response with no aggregations
// block renders an empty result instead of a parse error.
func TestAggMissingAggregationsGraceful(t *testing.T) {
	resp := `{"_shards":{"failed":0},"hits":{"total":{"value":0},"hits":[]}}`
	res := aggRun(t, resp, "--metric", "sum:bytes", "-o", "jsonl")
	if res.code != 0 {
		t.Fatalf("missing aggregations should render gracefully, got exit %d: %s", res.code, res.stderr)
	}
	if strings.TrimSpace(res.stdout) != "" {
		t.Errorf("expected empty output for missing aggregations, got %q", res.stdout)
	}
}

// TestAggSizeCapping asserts an explicit --size beyond max_result_window is capped
// with a warning on the aggregation path, mirroring the hit path.
func TestAggSizeCapping(t *testing.T) {
	stub := newESStub(t, func(r recordedReq) (int, string) {
		if containsPath(r.path, "_settings") {
			return 200, settingsBody(10000)
		}
		return 200, aggResponse
	})
	cfg := writeTestConfig(t, stub.url())
	res := runCLI(t, context.Background(), "search", "-c", "test", "-t", "app-logs",
		"--terms", "service", "--size", "50000", "--config", cfg)
	if res.code != 0 {
		t.Fatalf("exit %d: %s", res.code, res.stderr)
	}
	if !strings.Contains(res.stderr, "capping") {
		t.Errorf("expected capping warning on stderr, got %q", res.stderr)
	}
	sr := stub.searchRequests()
	if size := parseBody(t, sr[len(sr)-1].body)["size"].(float64); size != 10000 {
		t.Errorf("capped size = %v, want 10000", size)
	}
}

// ---- partial shard failure surfacing ----

// shardFailureResp is an aggregation response reporting one failed shard with a
// fielddata reason alongside a partial bucket result.
const shardFailureResp = `{"_shards":{"total":5,"successful":4,"failed":1,` +
	`"failures":[{"shard":0,"index":"i","reason":{"type":"illegal_argument_exception","reason":"Fielddata is disabled on [ip]"}}]},` +
	`"hits":{"total":{"value":3},"hits":[]},` +
	`"aggregations":{"group":{"buckets":[{"key":"svc-a","doc_count":3}]}}}`

// TestAggShardFailureSurfaced asserts a partial shard failure emits the partial
// aggregations to stdout, prints the documented incomplete-results diagnostic to
// stderr, and exits 5 rather than reporting success.
func TestAggShardFailureSurfaced(t *testing.T) {
	res := aggRun(t, shardFailureResp, "--terms", "service", "-o", "jsonl")
	if res.code != 5 {
		t.Fatalf("exit %d, want 5 (stderr=%s)", res.code, res.stderr)
	}
	want := "incomplete results: 1 of 5 shards failed (Fielddata is disabled on [ip])"
	if !strings.Contains(res.stderr, want) {
		t.Errorf("stderr = %q, want it to contain %q", res.stderr, want)
	}
	if !strings.Contains(res.stdout, "svc-a") {
		t.Errorf("expected partial aggregation emitted, got %q", res.stdout)
	}
}

// TestAggShardFailureQuietStillReports asserts --quiet does NOT swallow the
// partial-failure diagnostic: it is now a non-zero-exit error (exit 5), not a
// suppressible warning, so the diagnostic must still reach stderr.
func TestAggShardFailureQuietStillReports(t *testing.T) {
	res := aggRun(t, shardFailureResp, "--terms", "service", "--quiet", "-o", "jsonl")
	if res.code != 5 {
		t.Fatalf("exit %d, want 5 (stderr=%s)", res.code, res.stderr)
	}
	if !strings.Contains(res.stderr, "incomplete results: 1 of 5 shards failed") {
		t.Errorf("quiet must not suppress the exit-5 diagnostic, got %q", res.stderr)
	}
	if !strings.Contains(res.stdout, "svc-a") {
		t.Errorf("expected partial aggregation emitted, got %q", res.stdout)
	}
}

// TestAggregationInvalidRegexRejected asserts a malformed --include/--exclude
// regex is still reported as a usage error in the aggregation path (regression
// guard: regex validation must not be skipped by the aggregation early-return).
func TestAggregationInvalidRegexRejected(t *testing.T) {
	stub := newESStub(t, func(recordedReq) (int, string) { return 200, aggResponse })
	cfg := writeTestConfig(t, stub.url())
	res := runCLI(t, context.Background(), "search", "-c", "test", "-t", "app-logs",
		"--terms", "service", "--include", "[", "--config", cfg)
	if res.code != 2 {
		t.Errorf("exit %d, want 2 (stderr=%s)", res.code, res.stderr)
	}
	if len(stub.searchRequests()) != 0 {
		t.Errorf("no request should be issued on bad regex, got %d", len(stub.searchRequests()))
	}
}

// TestAggregationValidationAccepts asserts valid aggregation flag combinations
// pass validation.
func TestAggregationValidationAccepts(t *testing.T) {
	base := func() searchParams {
		return searchParams{target: "app-logs", tsField: "@timestamp", size: 50}
	}
	cases := []func(*searchParams){
		func(p *searchParams) { p.terms = "host:5"; p.metrics = []string{"sum:bytes"} },
		func(p *searchParams) { p.dateHistogram = "@timestamp:1M" },
		func(p *searchParams) { p.metrics = []string{"avg:latency"}; p.cardinalities = []string{"client.ip"} },
		func(p *searchParams) { p.rawAggs = `{"top_isp":{"terms":{"field":"isp"}}}` },
	}
	for i, mutate := range cases {
		p := base()
		mutate(&p)
		if err := p.validate(); err != nil {
			t.Errorf("case %d: unexpected validation error: %v", i, err)
		}
	}
}
