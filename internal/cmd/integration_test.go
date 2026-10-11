//go:build integration

package cmd

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// Integration tests run the CLI against a real Elasticsearch. They verify the
// Elasticsearch side of the contract — requests are accepted and responses
// parse — not output formatting, which the stub-based tests already cover.
//
//	ES_LOG_TEST_URL=http://localhost:9200 go test -tags integration ./...
//
// ES_LOG_TEST_EXPECT_VERSION, when set, must match the version ping reports.

// deprecationPrefix is the stderr prefix of an Elasticsearch deprecation
// warning (see buildClient).
const deprecationPrefix = "elasticsearch deprecation: "

// knownDeprecations lists deprecation messages that are accepted for now. Any
// other deprecation warning fails the run so new server-side deprecations are
// noticed before they become removals. Keep it empty unless a fix is pending.
var knownDeprecations = map[string]struct{}{}

func TestIntegration(t *testing.T) {
	base := os.Getenv("ES_LOG_TEST_URL")
	if base == "" {
		t.Fatal("ES_LOG_TEST_URL is required for integration tests")
	}
	s := seedCluster(t, base)
	cfg := writeTestConfig(t, base)

	run := func(t *testing.T, args ...string) cliResult {
		t.Helper()
		res := runCLI(t, context.Background(), append(args, "-c", "test", "--config", cfg)...)
		assertNoDeprecation(t, res.stderr)
		return res
	}
	wantExit := func(t *testing.T, res cliResult, code int) {
		t.Helper()
		if res.code != code {
			t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", res.code, code, res.stdout, res.stderr)
		}
	}

	t.Run("ping", func(t *testing.T) {
		res := run(t, "ping")
		wantExit(t, res, 0)
		out := strings.TrimSpace(res.stdout)
		if !strings.HasPrefix(out, "ok:") {
			t.Fatalf("stdout = %q, want an ok: line", out)
		}
		if want := os.Getenv("ES_LOG_TEST_EXPECT_VERSION"); want != "" {
			if !strings.HasSuffix(out, "(Elasticsearch "+want+")") {
				t.Errorf("stdout = %q, want version (Elasticsearch %s)", out, want)
			}
		}
	})

	t.Run("ls", func(t *testing.T) {
		res := run(t, "ls", "-o", "json")
		wantExit(t, res, 0)
		var rows []lsRow
		decodeJSON(t, res.stdout, &rows)
		found := map[string]string{}
		for _, r := range rows {
			found[r.Name] = r.Type
		}
		if found[s.logs()] != listAliases {
			t.Errorf("alias %s: type %q, want %q", s.logs(), found[s.logs()], listAliases)
		}
		if found[s.ds()] != listDataStreams {
			t.Errorf("datastream %s: type %q, want %q", s.ds(), found[s.ds()], listDataStreams)
		}
	})

	t.Run("fields", func(t *testing.T) {
		res := run(t, "fields", s.logs(), "-o", "json")
		wantExit(t, res, 0)
		var rows []fieldRow
		decodeJSON(t, res.stdout, &rows)
		byName := map[string]fieldRow{}
		for _, r := range rows {
			byName[r.Name] = r
		}
		if lv := byName["level"]; !slices.Equal(lv.Types, []string{"keyword", "text"}) || !lv.Conflict {
			t.Errorf("level = %+v, want types [keyword text] with conflict", lv)
		}
		if ts := byName["@timestamp"]; !slices.Equal(ts.Types, []string{"date"}) || ts.Conflict {
			t.Errorf("@timestamp = %+v, want date without conflict", ts)
		}
		if msg := byName["message"]; !slices.Equal(msg.Types, []string{"text"}) {
			t.Errorf("message = %+v, want text", msg)
		}
	})

	t.Run("search hits", func(t *testing.T) {
		res := run(t, "search", "-t", s.logs(), "-q", "level:error", "--since", "1h")
		wantExit(t, res, 0)
		if n := countLines(res.stdout); n != s.errorHits {
			t.Errorf("jsonl lines = %d, want %d", n, s.errorHits)
		}
		res = run(t, "search", "-t", s.logs(), "-q", "level:error", "--since", "1h", "-o", "json")
		wantExit(t, res, 0)
		var doc searchJSON
		decodeJSON(t, res.stdout, &doc)
		if doc.Total != s.errorHits {
			t.Errorf("total = %d, want %d", doc.Total, s.errorHits)
		}
	})

	t.Run("search datastream", func(t *testing.T) {
		res := run(t, "search", "-t", s.ds(), "--since", "1h")
		wantExit(t, res, 0)
		if n := countLines(res.stdout); n != seedDataDocs {
			t.Errorf("jsonl lines = %d, want %d", n, seedDataDocs)
		}
	})

	t.Run("search paging", func(t *testing.T) {
		res := run(t, "search", "-t", s.paging(), "-q", "level:error", "--since", "1h", "--size", "0")
		wantExit(t, res, 0)
		if n := countLines(res.stdout); n != seedPaging {
			t.Errorf("jsonl lines = %d, want %d (paged with window %d)", n, seedPaging, seedPageLimit)
		}
	})

	t.Run("search capping", func(t *testing.T) {
		res := run(t, "search", "-t", s.paging(), "-q", "level:error", "--since", "1h", "--size", "10001")
		wantExit(t, res, 0)
		if n := countLines(res.stdout); n != seedPageLimit {
			t.Errorf("jsonl lines = %d, want %d", n, seedPageLimit)
		}
		if !strings.Contains(res.stderr, "capping to 60") {
			t.Errorf("stderr = %q, want a capping warning", res.stderr)
		}
	})

	t.Run("terms with metric", func(t *testing.T) {
		res := run(t, "search", "-t", s.logs(), "--since", "1h",
			"--terms", "service:10", "--metric", "sum:bytes", "-o", "json")
		wantExit(t, res, 0)
		var doc struct {
			Aggregations struct {
				Group struct {
					Buckets []struct {
						Key      string `json:"key"`
						DocCount int    `json:"doc_count"`
						Sum      struct {
							Value float64 `json:"value"`
						} `json:"sum_bytes"`
					} `json:"buckets"`
				} `json:"group"`
			} `json:"aggregations"`
		}
		decodeJSON(t, res.stdout, &doc)
		got := map[string]int{}
		for _, b := range doc.Aggregations.Group.Buckets {
			got[b.Key] = int(b.Sum.Value)
		}
		if len(got) != len(s.bytesByService) {
			t.Errorf("buckets = %v, want %v", got, s.bytesByService)
		}
		for svc, want := range s.bytesByService {
			if got[svc] != want {
				t.Errorf("sum_bytes[%s] = %d, want %d", svc, got[svc], want)
			}
		}
	})

	t.Run("date histogram", func(t *testing.T) {
		res := run(t, "search", "-t", s.logs(), "--since", "1h",
			"--date-histogram", "@timestamp:5m", "-o", "json")
		wantExit(t, res, 0)
		var doc struct {
			Aggregations struct {
				Group struct {
					Buckets []struct {
						DocCount int `json:"doc_count"`
					} `json:"buckets"`
				} `json:"group"`
			} `json:"aggregations"`
		}
		decodeJSON(t, res.stdout, &doc)
		buckets := doc.Aggregations.Group.Buckets
		total := 0
		for _, b := range buckets {
			total += b.DocCount
		}
		if len(buckets) == 0 || total != 2*seedPerIndex {
			t.Errorf("%d buckets with %d docs, want >=1 bucket and %d docs", len(buckets), total, 2*seedPerIndex)
		}
	})

	t.Run("raw aggs", func(t *testing.T) {
		res := run(t, "search", "-t", s.logs(), "--since", "1h", "-o", "json",
			"--aggs", `{"by_service":{"terms":{"field":"service"}}}`)
		wantExit(t, res, 0)
		var doc struct {
			Aggregations map[string]json.RawMessage `json:"aggregations"`
		}
		decodeJSON(t, res.stdout, &doc)
		if _, ok := doc.Aggregations["by_service"]; !ok {
			t.Errorf("aggregations = %s, want by_service", res.stdout)
		}
	})

	t.Run("partial shard failure", func(t *testing.T) {
		// level is keyword in <p>-logs-a but text (no fielddata) in <p>-logs-b,
		// so the terms aggregation fails on b's shard and succeeds on a's.
		res := run(t, "search", "-t", s.logs(), "--since", "1h", "--terms", "level:10", "-o", "json")
		wantExit(t, res, exitPartial)
		if !strings.Contains(res.stderr, "incomplete results") {
			t.Errorf("stderr = %q, want an incomplete results diagnostic", res.stderr)
		}
		var doc struct {
			Aggregations struct {
				Group struct {
					Buckets []json.RawMessage `json:"buckets"`
				} `json:"group"`
			} `json:"aggregations"`
		}
		decodeJSON(t, res.stdout, &doc)
		if len(doc.Aggregations.Group.Buckets) == 0 {
			t.Errorf("stdout = %s, want the partial buckets from the healthy shard", res.stdout)
		}
	})
}

// assertNoDeprecation fails the test for every deprecation warning on stderr
// that is not in knownDeprecations.
func assertNoDeprecation(t *testing.T, stderr string) {
	t.Helper()
	for line := range strings.Lines(stderr) {
		msg, ok := strings.CutPrefix(strings.TrimRight(line, "\n"), deprecationPrefix)
		if !ok {
			continue
		}
		if _, known := knownDeprecations[msg]; !known {
			t.Errorf("unexpected Elasticsearch deprecation warning: %s", msg)
		}
	}
}

func decodeJSON(t *testing.T, s string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(s), v); err != nil {
		t.Fatalf("decode %q: %v", s, err)
	}
}

// countLines counts newline-terminated output lines (JSONL records).
func countLines(s string) int {
	return strings.Count(s, "\n")
}
