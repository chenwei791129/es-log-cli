package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// parseBody unmarshals a request body into a generic map.
func parseBody(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse body: %v (%s)", err, raw)
	}
	return m
}

// ---- buildBody unit tests (no server) ----

func TestQueryString(t *testing.T) {
	p := searchParams{target: "app-logs", query: "level:error", tsField: "@timestamp"}
	body, err := p.buildBody(50, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := parseBody(t, body)
	q := m["query"].(map[string]any)
	qs, ok := q["query_string"].(map[string]any)
	if !ok || qs["query"] != "level:error" {
		t.Errorf("query_string not built: %v", q)
	}
}

func TestMatchAllDefault(t *testing.T) {
	p := searchParams{target: "app-logs", tsField: "@timestamp"}
	m := parseBody(t, mustBody(t, p, 50, nil))
	q := m["query"].(map[string]any)
	if _, ok := q["match_all"]; !ok {
		t.Errorf("expected match_all, got %v", q)
	}
}

func TestSinceRange(t *testing.T) {
	p := searchParams{target: "app-logs", since: "1h", tsField: "@timestamp"}
	m := parseBody(t, mustBody(t, p, 50, nil))
	rng := digRange(t, m, "@timestamp")
	if rng["gte"] != "now-1h" {
		t.Errorf("range gte = %v, want now-1h", rng["gte"])
	}
}

func TestFieldsProjection(t *testing.T) {
	p := searchParams{target: "app-logs", fields: []string{"message", "level"}, tsField: "@timestamp"}
	m := parseBody(t, mustBody(t, p, 50, nil))
	src, ok := m["_source"].([]any)
	if !ok || len(src) != 2 || src[0] != "message" || src[1] != "level" {
		t.Errorf("_source projection = %v", m["_source"])
	}
}

func TestDefaultSort(t *testing.T) {
	p := searchParams{target: "app-logs", tsField: "@timestamp"}
	m := parseBody(t, mustBody(t, p, 50, nil))
	sort := m["sort"].([]any)
	first := sort[0].(map[string]any)
	if first["@timestamp"] != "desc" {
		t.Errorf("default sort = %v, want @timestamp:desc", sort)
	}
	last := sort[len(sort)-1].(map[string]any)
	if _, ok := last["_doc"]; !ok {
		t.Errorf("missing _doc tiebreaker: %v", sort)
	}
}

func TestTimestampFieldRetargetsRangeAndSort(t *testing.T) {
	p := searchParams{target: "app-logs", since: "1h", tsField: "event.created"}
	m := parseBody(t, mustBody(t, p, 50, nil))
	if rng := digRange(t, m, "event.created"); rng["gte"] != "now-1h" {
		t.Errorf("range not retargeted: %v", m["query"])
	}
	sort := m["sort"].([]any)
	first := sort[0].(map[string]any)
	if _, ok := first["event.created"]; !ok {
		t.Errorf("default sort not retargeted: %v", sort)
	}
}

func mustBody(t *testing.T, p searchParams, size int, after []json.RawMessage) []byte {
	t.Helper()
	b, err := p.buildBody(size, after)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// digRange extracts the range bounds for field from a bool.filter query body.
func digRange(t *testing.T, m map[string]any, field string) map[string]any {
	t.Helper()
	q := m["query"].(map[string]any)
	b, ok := q["bool"].(map[string]any)
	if !ok {
		t.Fatalf("expected bool query, got %v", q)
	}
	filters := b["filter"].([]any)
	for _, f := range filters {
		fm := f.(map[string]any)
		if rng, ok := fm["range"].(map[string]any); ok {
			if bounds, ok := rng[field].(map[string]any); ok {
				return bounds
			}
		}
	}
	t.Fatalf("range on %q not found in %v", field, filters)
	return nil
}

// ---- command-level tests (stub server) ----

const okSearch = `{"hits":{"total":{"value":0},"hits":[]}}`

func settingsBody(window int) string {
	return fmt.Sprintf(`{"idx-1":{"settings":{"index.max_result_window":"%d"}}}`, window)
}

// searchPage builds a _search response with count hits starting at startIdx.
func searchPage(total, startIdx, count int) string {
	hits := make([]string, 0, count)
	for i := startIdx; i < startIdx+count; i++ {
		hits = append(hits, fmt.Sprintf(
			`{"_id":"id-%d","_index":"app-logs","_score":null,"_source":{"n":%d,"message":"m%d"},"sort":[%d]}`,
			i, i, i, i))
	}
	return fmt.Sprintf(`{"hits":{"total":{"value":%d},"hits":[%s]}}`, total, strings.Join(hits, ","))
}

func TestSearchTargetFlag(t *testing.T) {
	stub := newESStub(t, func(recordedReq) (int, string) { return 200, okSearch })
	cfg := writeTestConfig(t, stub.url())
	res := runCLI(t, context.Background(), "search", "-c", "test", "-t", "app-logs", "--config", cfg)
	if res.code != 0 {
		t.Fatalf("exit %d, stderr=%s", res.code, res.stderr)
	}
	sr := stub.searchRequests()
	if len(sr) != 1 || sr[0].path != "/app-logs/_search" {
		t.Errorf("search requests = %+v", sr)
	}
}

func TestSearchMissingTarget(t *testing.T) {
	cfg := writeTestConfig(t, "http://unused")
	res := runCLI(t, context.Background(), "search", "-c", "test", "--config", cfg)
	if res.code != 2 {
		t.Errorf("exit %d, want 2 (stderr=%s)", res.code, res.stderr)
	}
	if res.stdout != "" {
		t.Errorf("stdout not empty: %q", res.stdout)
	}
}

func TestTimeFlagsMutualExclusion(t *testing.T) {
	cfg := writeTestConfig(t, "http://unused")
	res := runCLI(t, context.Background(), "search", "-c", "test", "-t", "app-logs",
		"--since", "1h", "--from", "2026-01-01T00:00:00Z", "--config", cfg)
	if res.code != 2 {
		t.Errorf("exit %d, want 2 (stderr=%s)", res.code, res.stderr)
	}
}

func TestSizeNoLookupForSmallN(t *testing.T) {
	stub := newESStub(t, func(r recordedReq) (int, string) {
		if containsPath(r.path, "_settings") {
			return 200, settingsBody(10000)
		}
		return 200, searchPage(3, 0, 3)
	})
	cfg := writeTestConfig(t, stub.url())
	res := runCLI(t, context.Background(), "search", "-c", "test", "-t", "app-logs", "--config", cfg)
	if res.code != 0 {
		t.Fatalf("exit %d stderr=%s", res.code, res.stderr)
	}
	if stub.settingsRequested() {
		t.Error("GetMaxResultWindow should not be called for default size 50")
	}
	sr := stub.searchRequests()
	if len(sr) != 1 || parseBody(t, sr[0].body)["size"].(float64) != 50 {
		t.Errorf("expected single size:50 search, got %+v", sr)
	}
}

func TestSizeCapping(t *testing.T) {
	stub := newESStub(t, func(r recordedReq) (int, string) {
		if containsPath(r.path, "_settings") {
			return 200, settingsBody(10000)
		}
		return 200, searchPage(10000, 0, 1)
	})
	cfg := writeTestConfig(t, stub.url())
	res := runCLI(t, context.Background(), "search", "-c", "test", "-t", "app-logs",
		"--size", "50000", "--config", cfg)
	if res.code != 0 {
		t.Fatalf("exit %d stderr=%s", res.code, res.stderr)
	}
	if !stub.settingsRequested() {
		t.Error("GetMaxResultWindow should be called for N>10000")
	}
	if !strings.Contains(res.stderr, "capping") {
		t.Errorf("expected capping warning on stderr, got %q", res.stderr)
	}
	sr := stub.searchRequests()
	if size := parseBody(t, sr[len(sr)-1].body)["size"].(float64); size != 10000 {
		t.Errorf("capped size = %v, want 10000", size)
	}
}

func TestSizeZeroPagination(t *testing.T) {
	const total = 25000
	served := 0
	stub := newESStub(t, func(r recordedReq) (int, string) {
		if containsPath(r.path, "_settings") {
			return 200, settingsBody(10000)
		}
		remaining := total - served
		n := remaining
		if n > 10000 {
			n = 10000
		}
		start := served
		served += n
		return 200, searchPage(total, start, n)
	})
	cfg := writeTestConfig(t, stub.url())
	res := runCLI(t, context.Background(), "search", "-c", "test", "-t", "app-logs",
		"--size", "0", "--config", cfg, "-o", "jsonl")
	if res.code != 0 {
		t.Fatalf("exit %d stderr=%s", res.code, res.stderr)
	}
	sr := stub.searchRequests()
	if len(sr) != 3 {
		t.Errorf("expected 3 paged requests, got %d", len(sr))
	}
	// second and third requests must carry search_after
	for i := 1; i < len(sr); i++ {
		if _, ok := parseBody(t, sr[i].body)["search_after"]; !ok {
			t.Errorf("request %d missing search_after", i)
		}
	}
	lines := strings.Split(strings.TrimRight(res.stdout, "\n"), "\n")
	if len(lines) != total {
		t.Errorf("emitted %d lines, want %d", len(lines), total)
	}
}

func TestWindowFallbackTo10000(t *testing.T) {
	const total = 25000
	served := 0
	stub := newESStub(t, func(r recordedReq) (int, string) {
		if containsPath(r.path, "_settings") {
			return 500, `{"error":"boom"}` // lookup fails -> fallback 10000
		}
		body := parseBody(t, r.body)
		if size := body["size"].(float64); size != 10000 {
			t.Errorf("page size = %v, want 10000 fallback", size)
		}
		remaining := total - served
		n := remaining
		if n > 10000 {
			n = 10000
		}
		start := served
		served += n
		return 200, searchPage(total, start, n)
	})
	cfg := writeTestConfig(t, stub.url())
	res := runCLI(t, context.Background(), "search", "-c", "test", "-t", "app-logs",
		"--size", "0", "--config", cfg, "-o", "jsonl")
	if res.code != 0 {
		t.Fatalf("exit %d stderr=%s", res.code, res.stderr)
	}
	if len(stub.searchRequests()) != 3 {
		t.Errorf("expected 3 paged requests, got %d", len(stub.searchRequests()))
	}
}

// ---- output schema tests ----

func TestJSONLBareSource(t *testing.T) {
	stub := newESStub(t, func(recordedReq) (int, string) { return 200, searchPage(2, 0, 2) })
	cfg := writeTestConfig(t, stub.url())
	res := runCLI(t, context.Background(), "search", "-c", "test", "-t", "app-logs", "--config", cfg)
	if res.code != 0 {
		t.Fatal(res.stderr)
	}
	for _, line := range strings.Split(strings.TrimRight(res.stdout, "\n"), "\n") {
		var src map[string]any
		if err := json.Unmarshal([]byte(line), &src); err != nil {
			t.Fatalf("line not valid JSON: %q", line)
		}
		for _, env := range []string{"_id", "_index", "_score"} {
			if _, ok := src[env]; ok {
				t.Errorf("line carries envelope field %q: %q", env, line)
			}
		}
		if _, ok := src["n"]; !ok {
			t.Errorf("line missing source field: %q", line)
		}
	}
}

func TestJSONWithMetadata(t *testing.T) {
	stub := newESStub(t, func(recordedReq) (int, string) { return 200, searchPage(7, 0, 7) })
	cfg := writeTestConfig(t, stub.url())
	res := runCLI(t, context.Background(), "search", "-c", "test", "-t", "app-logs",
		"-o", "json", "--config", cfg)
	if res.code != 0 {
		t.Fatal(res.stderr)
	}
	var doc struct {
		Total int `json:"total"`
		Hits  []struct {
			ID     string          `json:"_id"`
			Index  string          `json:"_index"`
			Source json.RawMessage `json:"_source"`
		} `json:"hits"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &doc); err != nil {
		t.Fatalf("output not a JSON doc: %v\n%s", err, res.stdout)
	}
	if doc.Total != 7 {
		t.Errorf("total = %d, want 7", doc.Total)
	}
	if len(doc.Hits) != 7 || doc.Hits[0].ID == "" || doc.Hits[0].Index == "" {
		t.Errorf("hits metadata incomplete: %+v", doc.Hits)
	}
}

// ---- client-side filtering tests ----

func TestIncludeFilter(t *testing.T) {
	page := `{"hits":{"total":{"value":2},"hits":[
		{"_id":"1","_index":"i","_score":null,"_source":{"message":"timeout occurred"},"sort":[1]},
		{"_id":"2","_index":"i","_score":null,"_source":{"message":"all good"},"sort":[2]}]}}`
	stub := newESStub(t, func(recordedReq) (int, string) { return 200, page })
	cfg := writeTestConfig(t, stub.url())
	res := runCLI(t, context.Background(), "search", "-c", "test", "-t", "app-logs",
		"--include", "timeout", "--config", cfg)
	if res.code != 0 {
		t.Fatal(res.stderr)
	}
	lines := strings.Split(strings.TrimRight(res.stdout, "\n"), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "timeout") {
		t.Errorf("include filter result = %q", res.stdout)
	}
}

func TestExcludeFilter(t *testing.T) {
	page := `{"hits":{"total":{"value":2},"hits":[
		{"_id":"1","_index":"i","_score":null,"_source":{"message":"healthcheck ok"},"sort":[1]},
		{"_id":"2","_index":"i","_score":null,"_source":{"message":"real error"},"sort":[2]}]}}`
	stub := newESStub(t, func(recordedReq) (int, string) { return 200, page })
	cfg := writeTestConfig(t, stub.url())
	res := runCLI(t, context.Background(), "search", "-c", "test", "-t", "app-logs",
		"--exclude", "healthcheck", "--config", cfg)
	if res.code != 0 {
		t.Fatal(res.stderr)
	}
	if strings.Contains(res.stdout, "healthcheck") {
		t.Errorf("exclude failed: %q", res.stdout)
	}
}

// TestFilterSameAcrossFormats asserts include matches _source only (ignoring the
// envelope) so jsonl and json produce the same (empty) filtered set when the
// pattern only appears in the _index envelope.
func TestFilterSameAcrossFormats(t *testing.T) {
	page := `{"hits":{"total":{"value":1},"hits":[
		{"_id":"1","_index":"app-special","_score":null,"_source":{"message":"nothing here"},"sort":[1]}]}}`
	run := func(format string) cliResult {
		stub := newESStub(t, func(recordedReq) (int, string) { return 200, page })
		cfg := writeTestConfig(t, stub.url())
		return runCLI(t, context.Background(), "search", "-c", "test", "-t", "app-logs",
			"--include", "app", "-o", format, "--config", cfg)
	}
	jsonl := run("jsonl")
	js := run("json")
	if strings.TrimSpace(jsonl.stdout) != "" {
		t.Errorf("jsonl should be empty (envelope ignored), got %q", jsonl.stdout)
	}
	var doc struct {
		Hits []json.RawMessage `json:"hits"`
	}
	_ = json.Unmarshal([]byte(js.stdout), &doc)
	if len(doc.Hits) != 0 {
		t.Errorf("json should have no hits (envelope ignored), got %q", js.stdout)
	}
}

// TestSearchFlagValidation locks the usage-error guards added after review:
// negative size, malformed --sort, zero --since, and reversed --from/--to all
// fail fast with exit 2 before contacting Elasticsearch.
func TestSearchFlagValidation(t *testing.T) {
	cfg := writeTestConfig(t, "http://unused")
	cases := []struct {
		name string
		args []string
	}{
		{"negative size", []string{"--size", "-5"}},
		{"sort empty field", []string{"--sort", ":desc"}},
		{"sort bad direction", []string{"--sort", "@timestamp:up"}},
		{"zero since", []string{"--since", "0h"}},
		{"multi-digit zero since", []string{"--since", "000s"}},
		{"reversed range", []string{"--from", "2026-06-26T00:00:00Z", "--to", "2026-06-25T00:00:00Z"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := append([]string{"search", "-c", "test", "-t", "app-logs", "--config", cfg}, c.args...)
			res := runCLI(t, context.Background(), args...)
			if res.code != 2 {
				t.Errorf("exit %d, want 2 (stderr=%s)", res.code, res.stderr)
			}
			if res.stdout != "" {
				t.Errorf("stdout should be empty: %q", res.stdout)
			}
		})
	}
}

// TestSearchTableNoNilCells asserts table output renders absent _source fields as
// blanks rather than the literal "<nil>".
func TestSearchTableNoNilCells(t *testing.T) {
	page := `{"hits":{"total":{"value":1},"hits":[
		{"_id":"1","_index":"i","_score":null,"_source":{"level":"error"},"sort":[1]}]}}`
	stub := newESStub(t, func(recordedReq) (int, string) { return 200, page })
	cfg := writeTestConfig(t, stub.url())
	res := runCLI(t, context.Background(), "search", "-c", "test", "-t", "app-logs", "-o", "table", "--config", cfg)
	if res.code != 0 {
		t.Fatal(res.stderr)
	}
	if strings.Contains(res.stdout, "<nil>") {
		t.Errorf("table should not render <nil>: %q", res.stdout)
	}
}

// TestSearchJSONLNilSource asserts a hit with no _source does not abort the
// jsonl stream (it renders as a null line) — regression for the json.Compact crash.
func TestSearchJSONLNilSource(t *testing.T) {
	page := `{"hits":{"total":{"value":2},"hits":[
		{"_id":"1","_index":"i","_score":null,"sort":[1]},
		{"_id":"2","_index":"i","_score":null,"_source":{"level":"error"},"sort":[2]}]}}`
	stub := newESStub(t, func(recordedReq) (int, string) { return 200, page })
	cfg := writeTestConfig(t, stub.url())
	res := runCLI(t, context.Background(), "search", "-c", "test", "-t", "app-logs", "--config", cfg)
	if res.code != 0 {
		t.Fatalf("nil _source should not crash jsonl render: exit %d (%s)", res.code, res.stderr)
	}
	lines := strings.Split(strings.TrimRight(res.stdout, "\n"), "\n")
	if len(lines) != 2 || lines[0] != "null" {
		t.Errorf("expected first line 'null', got %q", res.stdout)
	}
}

// TestSearchTrackTotalHits asserts track_total_hits is requested only in json
// mode (where total must be accurate beyond 10000), not on the default jsonl path.
func TestSearchTrackTotalHits(t *testing.T) {
	run := func(args ...string) recordedReq {
		stub := newESStub(t, func(recordedReq) (int, string) { return 200, searchPage(1, 0, 1) })
		cfg := writeTestConfig(t, stub.url())
		full := append([]string{"search", "-c", "test", "-t", "app-logs", "--config", cfg}, args...)
		res := runCLI(t, context.Background(), full...)
		if res.code != 0 {
			t.Fatalf("exit %d: %s", res.code, res.stderr)
		}
		return stub.searchRequests()[0]
	}
	if _, ok := parseBody(t, run("-o", "json").body)["track_total_hits"]; !ok {
		t.Error("json mode should request track_total_hits")
	}
	if _, ok := parseBody(t, run().body)["track_total_hits"]; ok {
		t.Error("jsonl mode should not request track_total_hits")
	}
}

// TestSearchTableNestedAndNumeric locks two round-3 fixes: a dotted
// --timestamp-field resolves into nested _source, and numeric values render as
// integers rather than scientific-notation float64.
func TestSearchTableNestedAndNumeric(t *testing.T) {
	page := `{"hits":{"total":{"value":1},"hits":[
		{"_id":"1","_index":"i","_score":null,"_source":{"event":{"created":1719123456},"message":"ok"},"sort":[1]}]}}`
	stub := newESStub(t, func(recordedReq) (int, string) { return 200, page })
	cfg := writeTestConfig(t, stub.url())
	res := runCLI(t, context.Background(), "search", "-c", "test", "-t", "app-logs",
		"-o", "table", "--timestamp-field", "event.created", "--config", cfg)
	if res.code != 0 {
		t.Fatal(res.stderr)
	}
	if !strings.Contains(res.stdout, "1719123456") {
		t.Errorf("nested numeric timestamp not rendered as integer: %q", res.stdout)
	}
	if strings.Contains(res.stdout, "e+") {
		t.Errorf("numeric rendered in scientific notation: %q", res.stdout)
	}
}

// TestConfigViewJSONL asserts `config view -o jsonl` emits a single compact line.
func TestConfigViewJSONL(t *testing.T) {
	cfg := writeMultiContextConfig(t, twoContexts)
	res := runCLI(t, context.Background(), "config", "view", "-o", "jsonl", "--config", cfg)
	if res.code != 0 {
		t.Fatal(res.stderr)
	}
	out := strings.TrimRight(res.stdout, "\n")
	if strings.Contains(out, "\n") {
		t.Errorf("jsonl view should be a single line: %q", res.stdout)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("jsonl view not valid JSON: %v", err)
	}
}
