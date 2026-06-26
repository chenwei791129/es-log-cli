package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const happyHits = `{"hits":{"total":{"value":2},"hits":[
	{"_id":"1","_index":"app-logs-000001","_score":null,"_source":{"level":"error","message":"timeout"},"sort":["2026-06-26T00:00:00Z",1]},
	{"_id":"2","_index":"app-logs-000001","_score":null,"_source":{"level":"error","message":"ok-healthcheck"},"sort":["2026-06-26T00:00:01Z",2]}]}}`

// TestE2EHappyPath drives a realistic search and asserts request shape and all
// output formats (task 7.1).
func TestE2EHappyPath(t *testing.T) {
	stub := newESStub(t, func(r recordedReq) (int, string) { return 200, happyHits })
	cfg := writeTestConfig(t, stub.url())

	res := runCLI(t, context.Background(), "search", "-c", "test", "-t", "app-logs",
		"-q", "level:error", "--since", "1h", "--config", cfg)
	if res.code != 0 {
		t.Fatalf("exit %d: %s", res.code, res.stderr)
	}

	// Only POST /app-logs/_search; default size 50 means no _settings lookup.
	if stub.settingsRequested() {
		t.Error("default size should not trigger _settings lookup")
	}
	sr := stub.searchRequests()
	if len(sr) != 1 || sr[0].path != "/app-logs/_search" {
		t.Fatalf("search requests = %+v", sr)
	}

	body := parseBody(t, sr[0].body)
	if body["size"].(float64) != 50 {
		t.Errorf("size = %v, want 50", body["size"])
	}
	bq := body["query"].(map[string]any)["bool"].(map[string]any)
	must := bq["must"].([]any)[0].(map[string]any)
	if _, ok := must["query_string"]; !ok {
		t.Errorf("missing query_string: %v", must)
	}
	rng := digRange(t, body, "@timestamp")
	if rng["gte"] != "now-1h" {
		t.Errorf("range = %v", rng)
	}
	if body["sort"].([]any)[0].(map[string]any)["@timestamp"] != "desc" {
		t.Errorf("sort = %v", body["sort"])
	}

	// jsonl: bare _source, one per line.
	lines := strings.Split(strings.TrimRight(res.stdout, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("jsonl lines = %d", len(lines))
	}
	var src map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &src); err != nil || src["level"] != "error" {
		t.Errorf("bare source line wrong: %q", lines[0])
	}
	if _, ok := src["_id"]; ok {
		t.Errorf("jsonl should not carry envelope: %q", lines[0])
	}

	// json: metadata + total.
	resJSON := runCLI(t, context.Background(), "search", "-c", "test", "-t", "app-logs",
		"-q", "level:error", "--since", "1h", "-o", "json", "--config", cfg)
	var doc struct {
		Total int               `json:"total"`
		Hits  []json.RawMessage `json:"hits"`
	}
	if err := json.Unmarshal([]byte(resJSON.stdout), &doc); err != nil {
		t.Fatalf("json parse: %v", err)
	}
	if doc.Total != 2 || len(doc.Hits) != 2 {
		t.Errorf("json total/hits = %d/%d", doc.Total, len(doc.Hits))
	}

	// include keeps only timeout; exclude drops healthcheck.
	resInc := runCLI(t, context.Background(), "search", "-c", "test", "-t", "app-logs",
		"--include", "timeout", "--config", cfg)
	if strings.Count(strings.TrimSpace(resInc.stdout), "\n") != 0 || !strings.Contains(resInc.stdout, "timeout") {
		t.Errorf("include result = %q", resInc.stdout)
	}
	resExc := runCLI(t, context.Background(), "search", "-c", "test", "-t", "app-logs",
		"--exclude", "healthcheck", "--config", cfg)
	if strings.Contains(resExc.stdout, "healthcheck") {
		t.Errorf("exclude result = %q", resExc.stdout)
	}
}

// TestE2EMissingContext asserts exit 2 with clean stdout (task 7.2).
func TestE2EMissingContext(t *testing.T) {
	t.Setenv("ES_LOG_CONTEXT", "")
	cfg := writeTestConfig(t, "http://unused")
	res := runCLI(t, context.Background(), "search", "-t", "app-logs", "--config", cfg)
	if res.code != 2 {
		t.Errorf("exit %d, want 2 (%s)", res.code, res.stderr)
	}
	if res.stdout != "" {
		t.Errorf("stdout should be clean: %q", res.stdout)
	}
}

// TestE2EConnectionFailure asserts exit 3 with clean stdout (task 7.2).
func TestE2EConnectionFailure(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := dead.URL
	dead.Close()
	cfg := writeTestConfig(t, url)
	res := runCLI(t, context.Background(), "search", "-c", "test", "-t", "app-logs", "--config", cfg)
	if res.code != 3 {
		t.Errorf("exit %d, want 3 (%s)", res.code, res.stderr)
	}
	if res.stdout != "" {
		t.Errorf("stdout should be clean: %q", res.stdout)
	}
}

// TestE2ETargetNotFound asserts a 404 surfaces a friendly message naming the
// target and exits 4 (task 7.3).
func TestE2ETargetNotFound(t *testing.T) {
	stub := newESStub(t, func(recordedReq) (int, string) {
		return 404, `{"error":{"type":"index_not_found_exception","reason":"no such index"}}`
	})
	cfg := writeTestConfig(t, stub.url())
	res := runCLI(t, context.Background(), "search", "-c", "test", "-t", "ghost-logs", "--config", cfg)
	if res.code != 4 {
		t.Errorf("exit %d, want 4 (%s)", res.code, res.stderr)
	}
	if !strings.Contains(res.stderr, "ghost-logs") {
		t.Errorf("error should name target: %q", res.stderr)
	}
	if res.stdout != "" {
		t.Errorf("stdout should be clean: %q", res.stdout)
	}
}

// TestSearchContextCancellation asserts a cancelled context stops pagination
// before issuing search requests (task 1.3).
func TestSearchContextCancellation(t *testing.T) {
	stub := newESStub(t, func(r recordedReq) (int, string) {
		if containsPath(r.path, "_settings") {
			return 200, settingsBody(10000)
		}
		return 200, searchPage(100000, 0, 10000)
	})
	cfg := writeTestConfig(t, stub.url())
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before running

	res := runCLI(t, ctx, "search", "-c", "test", "-t", "app-logs", "--size", "0", "--config", cfg)
	if res.code == 0 {
		t.Error("cancelled search should not succeed")
	}
	if n := len(stub.searchRequests()); n != 0 {
		t.Errorf("cancelled search issued %d _search requests, want 0", n)
	}
}
