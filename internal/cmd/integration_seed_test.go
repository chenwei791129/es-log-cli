//go:build integration

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Seeding talks to Elasticsearch with plain net/http on purpose: esclient is a
// read-only whitelist (ADR 0002) and must never gain write endpoints, even for
// tests.

const (
	seedPerIndex  = 6   // docs in each of <p>-logs-a and <p>-logs-b
	seedPaging    = 130 // docs in <p>-paging, all level:error
	seedDataDocs  = 3   // docs in <p>-ds
	seedPageLimit = 60  // index.max_result_window of <p>-paging
)

// seedMappings is shared by every seeded target; levelType lets <p>-logs-b map
// level as text to create a deliberate cross-index type conflict.
func seedMappings(levelType string) map[string]any {
	return map[string]any{"properties": map[string]any{
		"@timestamp": map[string]any{"type": "date"},
		"level":      map[string]any{"type": levelType},
		"service":    map[string]any{"type": "keyword"},
		"message":    map[string]any{"type": "text"},
		"bytes":      map[string]any{"type": "long"},
	}}
}

// seeded describes the fixtures created for one integration run.
type seeded struct {
	prefix string
	// errorHits is the number of level:error docs across <p>-logs-a and
	// <p>-logs-b, i.e. what a level:error search on the alias must return.
	errorHits int
	// bytesByService is the expected sum of bytes per service across
	// <p>-logs-a and <p>-logs-b.
	bytesByService map[string]int
}

func (s seeded) logs() string   { return s.prefix + "-logs" }
func (s seeded) paging() string { return s.prefix + "-paging" }
func (s seeded) ds() string     { return s.prefix + "-ds" }

// seedCluster creates the integration fixtures under a unique prefix and
// registers their removal with t.Cleanup.
func seedCluster(t *testing.T, base string) seeded {
	t.Helper()
	p := fmt.Sprintf("eslogtest-%d", time.Now().UnixNano())
	s := seeded{prefix: p, bytesByService: map[string]int{}}

	// Registered first so a failure part-way through still cleans up; deletes
	// tolerate targets that were never created.
	t.Cleanup(func() {
		esCall(t, base, http.MethodDelete, "/_data_stream/"+p+"-ds", nil, true)
		esCall(t, base, http.MethodDelete, "/_index_template/"+p+"-tpl", nil, true)
		esCall(t, base, http.MethodDelete,
			"/"+p+"-logs-a,"+p+"-logs-b,"+p+"-paging?ignore_unavailable=true", nil, true)
	})

	esCall(t, base, http.MethodPut, "/"+p+"-logs-a", map[string]any{"mappings": seedMappings("keyword")}, false)
	esCall(t, base, http.MethodPut, "/"+p+"-logs-b", map[string]any{"mappings": seedMappings("text")}, false)
	esCall(t, base, http.MethodPost, "/_aliases", map[string]any{"actions": []any{
		map[string]any{"add": map[string]any{"indices": []string{p + "-logs-a", p + "-logs-b"}, "alias": p + "-logs"}},
	}}, false)
	esCall(t, base, http.MethodPut, "/"+p+"-paging", map[string]any{
		"settings": map[string]any{"index.max_result_window": seedPageLimit},
		"mappings": seedMappings("keyword"),
	}, false)
	esCall(t, base, http.MethodPut, "/_index_template/"+p+"-tpl", map[string]any{
		"index_patterns": []string{p + "-ds*"},
		"data_stream":    map[string]any{},
		"template":       map[string]any{"mappings": seedMappings("keyword")},
	}, false)
	esCall(t, base, http.MethodPut, "/_data_stream/"+p+"-ds", nil, false)

	var bulk bytes.Buffer
	line := func(v any) {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		bulk.Write(b)
		bulk.WriteByte('\n')
	}
	// Timestamps sit in the last few minutes so --since 1h matches everything,
	// and are strictly increasing so search_after paging is deterministic.
	start := time.Now().UTC().Add(-10 * time.Minute)
	seq := 0
	doc := func(level, service string, bytes int) map[string]any {
		seq++
		return map[string]any{
			"@timestamp": start.Add(time.Duration(seq) * time.Millisecond).Format(time.RFC3339Nano),
			"level":      level,
			"service":    service,
			"message":    fmt.Sprintf("synthetic event %d", seq),
			"bytes":      bytes,
		}
	}
	for _, idx := range []string{p + "-logs-a", p + "-logs-b"} {
		for i := range seedPerIndex {
			level := "info"
			if i%2 == 0 {
				level = "error"
				s.errorHits++
			}
			service := "checkout"
			if i >= seedPerIndex/2 {
				service = "search"
			}
			b := 100 * (i + 1)
			s.bytesByService[service] += b
			line(map[string]any{"index": map[string]any{"_index": idx}})
			line(doc(level, service, b))
		}
	}
	for range seedPaging {
		line(map[string]any{"index": map[string]any{"_index": p + "-paging"}})
		line(doc("error", "checkout", 1))
	}
	for range seedDataDocs {
		// Data streams only accept the create action.
		line(map[string]any{"create": map[string]any{"_index": p + "-ds"}})
		line(doc("info", "search", 1))
	}

	resp := esCall(t, base, http.MethodPost, "/_bulk?refresh=true", bulk.Bytes(), false)
	var br struct {
		Errors bool              `json:"errors"`
		Items  []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(resp, &br); err != nil {
		t.Fatalf("parse bulk response: %v", err)
	}
	if br.Errors {
		t.Fatalf("bulk seeding reported errors: %s", resp)
	}
	if want := 2*seedPerIndex + seedPaging + seedDataDocs; len(br.Items) != want {
		t.Fatalf("bulk indexed %d docs, want %d", len(br.Items), want)
	}
	return s
}

// esCall issues one seeding/cleanup request. body is JSON-encoded unless it is
// already []byte (NDJSON for _bulk). A non-2xx response fails the test, except
// that a 404 is tolerated when lenient is set (cleanup of absent targets);
// lenient failures are only logged so cleanup always runs to completion.
func esCall(t *testing.T, base, method, path string, body any, lenient bool) []byte {
	t.Helper()
	var reader io.Reader
	contentType := "application/json"
	switch b := body.(type) {
	case nil:
	case []byte:
		reader = bytes.NewReader(b)
		contentType = "application/x-ndjson"
	default:
		data, err := json.Marshal(b)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	ctx := t.Context()
	if lenient {
		// t.Context is canceled before Cleanup functions run.
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if reader != nil {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if lenient {
			t.Logf("%s %s: %v", method, path, err)
			return nil
		}
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		switch {
		case lenient && resp.StatusCode == http.StatusNotFound:
		case lenient:
			t.Logf("%s %s: HTTP %d: %s", method, path, resp.StatusCode, data)
		default:
			t.Fatalf("%s %s: HTTP %d: %s", method, path, resp.StatusCode, data)
		}
	}
	return data
}
