package cmd

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// Mapping fixtures for the fields command tests, using synthetic field/index
// names only.
const (
	// flattenMappingBody exercises nested objects, multi-fields, and an object
	// without sub-properties within a single index.
	flattenMappingBody = `{"idx-1":{"mappings":{"properties":{` +
		`"user":{"properties":{"id":{"type":"keyword"}}},` +
		`"message":{"type":"text","fields":{"keyword":{"type":"keyword"}}},` +
		`"host":{}` +
		`}}}}`

	// conflictMappingBody resolves to two indices: tags has divergent types
	// (text vs keyword) and url.path exists only in web-logs with one type.
	conflictMappingBody = `{` +
		`"app-logs":{"mappings":{"properties":{"tags":{"type":"text"}}}},` +
		`"web-logs":{"mappings":{"properties":{` +
		`"tags":{"type":"keyword"},` +
		`"url":{"properties":{"path":{"type":"keyword"}}}` +
		`}}}` +
		`}`

	// consistentMappingBody has @timestamp as date in every index.
	consistentMappingBody = `{` +
		`"idx-1":{"mappings":{"properties":{"@timestamp":{"type":"date"}}}},` +
		`"idx-2":{"mappings":{"properties":{"@timestamp":{"type":"date"}}}}` +
		`}`
)

// fieldsStub serves the given mapping body for any _mapping request.
func fieldsStub(t *testing.T, mappingBody string) *esStub {
	return newESStub(t, func(r recordedReq) (int, string) {
		if strings.HasSuffix(r.path, "/_mapping") {
			return 200, mappingBody
		}
		return 404, `{}`
	})
}

// fieldsRun executes `fields <target>` against stub with the given format and
// extra args, returning the captured result without asserting the exit code.
func fieldsRun(t *testing.T, stub *esStub, format, target string, extra ...string) cliResult {
	t.Helper()
	cfg := writeTestConfig(t, stub.url())
	full := []string{"fields", target, "-c", "test", "-o", format, "--config", cfg}
	full = append(full, extra...)
	return runCLI(t, context.Background(), full...)
}

// fieldsRows runs `fields <target>` with json output and decodes the row array,
// failing on a non-zero exit code.
func fieldsRows(t *testing.T, stub *esStub, target string) []fieldRow {
	t.Helper()
	res := fieldsRun(t, stub, "json", target)
	if res.code != 0 {
		t.Fatalf("fields exit %d: %s", res.code, res.stderr)
	}
	var rows []fieldRow
	if err := json.Unmarshal([]byte(res.stdout), &rows); err != nil {
		t.Fatalf("fields output not a JSON array: %v\n%s", err, res.stdout)
	}
	return rows
}

// rowByName returns the row with the given name, failing when absent.
func rowByName(t *testing.T, rows []fieldRow, name string) fieldRow {
	t.Helper()
	for _, r := range rows {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("field %q not found in %+v", name, rows)
	return fieldRow{}
}

// TestFieldsFlattensMapping covers "Inspect target mapping as flattened field
// paths and types": nested object to dotted path, multi-field as a separate
// entry, object without sub-properties typed object, no intermediate node.
func TestFieldsFlattensMapping(t *testing.T) {
	rows := fieldsRows(t, fieldsStub(t, flattenMappingBody), "app-logs")

	names := make([]string, len(rows))
	for i, r := range rows {
		names[i] = r.Name
	}
	if slices.Contains(names, "user") {
		t.Errorf("intermediate object node leaked into output: %v", names)
	}
	want := map[string][]string{
		"user.id":         {"keyword"},
		"message":         {"text"},
		"message.keyword": {"keyword"},
		"host":            {"object"},
	}
	for name, types := range want {
		r := rowByName(t, rows, name)
		if !slices.Equal(r.Types, types) {
			t.Errorf("%s types = %v, want %v", name, r.Types, types)
		}
		if r.Conflict {
			t.Errorf("%s should not be a conflict", name)
		}
	}
}

// TestFieldsMarksConflict covers "Mark cross-index type conflicts" and the
// partial-absence-versus-conflict example: tags is a conflict listing both types
// while url.path (present in one index only) is not.
func TestFieldsMarksConflict(t *testing.T) {
	rows := fieldsRows(t, fieldsStub(t, conflictMappingBody), "app-logs,web-logs")

	tags := rowByName(t, rows, "tags")
	if !tags.Conflict {
		t.Errorf("tags should be marked as a conflict: %+v", tags)
	}
	if !slices.Equal(tags.Types, []string{"keyword", "text"}) {
		t.Errorf("tags types = %v, want [keyword text]", tags.Types)
	}

	urlPath := rowByName(t, rows, "url.path")
	if urlPath.Conflict {
		t.Errorf("url.path present in one index should not be a conflict: %+v", urlPath)
	}
	if !slices.Equal(urlPath.Types, []string{"keyword"}) {
		t.Errorf("url.path types = %v, want [keyword]", urlPath.Types)
	}
}

// TestFieldsJSONConsistentOmitsIndices covers the json scenario where a
// consistent field carries conflict=false and no indices key.
func TestFieldsJSONConsistentOmitsIndices(t *testing.T) {
	res := fieldsRun(t, fieldsStub(t, consistentMappingBody), "json", "app-logs")
	if res.code != 0 {
		t.Fatalf("fields exit %d: %s", res.code, res.stderr)
	}
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(res.stdout), &raw); err != nil {
		t.Fatalf("not a JSON array: %v\n%s", err, res.stdout)
	}
	var found bool
	for _, row := range raw {
		if string(row["name"]) != `"@timestamp"` {
			continue
		}
		found = true
		if string(row["conflict"]) != "false" {
			t.Errorf("@timestamp conflict = %s, want false", row["conflict"])
		}
		var types []string
		if err := json.Unmarshal(row["types"], &types); err != nil {
			t.Fatalf("@timestamp types not a JSON array: %v", err)
		}
		if !slices.Equal(types, []string{"date"}) {
			t.Errorf("@timestamp types = %v, want [date]", types)
		}
		if _, ok := row["indices"]; ok {
			t.Errorf("@timestamp row should omit indices key: %v", row)
		}
	}
	if !found {
		t.Fatalf("@timestamp row not present: %s", res.stdout)
	}
}

// TestFieldsJSONConflictBreakdown covers the json scenario where a conflict row
// carries conflict=true, the sorted type set, and a per-index breakdown.
func TestFieldsJSONConflictBreakdown(t *testing.T) {
	rows := fieldsRows(t, fieldsStub(t, conflictMappingBody), "app-logs,web-logs")
	tags := rowByName(t, rows, "tags")
	if !tags.Conflict {
		t.Fatalf("tags should be a conflict: %+v", tags)
	}
	if !slices.Equal(tags.Types, []string{"keyword", "text"}) {
		t.Errorf("tags types = %v, want [keyword text]", tags.Types)
	}
	want := map[string]string{"app-logs": "text", "web-logs": "keyword"}
	if len(tags.Indices) != len(want) {
		t.Fatalf("tags indices = %v, want %v", tags.Indices, want)
	}
	for idx, typ := range want {
		if tags.Indices[idx] != typ {
			t.Errorf("tags indices[%s] = %q, want %q", idx, tags.Indices[idx], typ)
		}
	}
}

// TestFieldsJSONL covers the jsonl format: one row object per line.
func TestFieldsJSONL(t *testing.T) {
	res := fieldsRun(t, fieldsStub(t, conflictMappingBody), "jsonl", "app-logs,web-logs")
	if res.code != 0 {
		t.Fatalf("fields exit %d: %s", res.code, res.stderr)
	}
	lines := strings.Split(strings.TrimRight(res.stdout, "\n"), "\n")
	var sawTags bool
	for _, line := range lines {
		var row fieldRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("jsonl line not a JSON object: %v\n%q", err, line)
		}
		if row.Name == "tags" {
			sawTags = true
			if !row.Conflict || !slices.Equal(row.Types, []string{"keyword", "text"}) {
				t.Errorf("tags jsonl row wrong: %+v", row)
			}
		}
	}
	if !sawTags {
		t.Errorf("jsonl output missing tags row: %s", res.stdout)
	}
}

// TestFieldsTableConflictMarker covers the table scenario: the conflict row's
// TYPE column joins the types and appends the conflict marker.
func TestFieldsTableConflictMarker(t *testing.T) {
	res := fieldsRun(t, fieldsStub(t, conflictMappingBody), "table", "app-logs,web-logs")
	if res.code != 0 {
		t.Fatalf("fields exit %d: %s", res.code, res.stderr)
	}
	var tagsLine string
	for _, line := range strings.Split(res.stdout, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "tags") {
			tagsLine = line
			break
		}
	}
	if tagsLine == "" {
		t.Fatalf("no tags row in table output:\n%s", res.stdout)
	}
	if !strings.Contains(tagsLine, "keyword, text  ⚠ conflict") {
		t.Errorf("tags table row missing conflict marker: %q", tagsLine)
	}
}

// TestFieldsInvalidOutputExits2 covers the invalid-output scenario.
func TestFieldsInvalidOutputExits2(t *testing.T) {
	res := fieldsRun(t, fieldsStub(t, consistentMappingBody), "yaml", "app-logs")
	if res.code != exitUsage {
		t.Errorf("invalid -o exit = %d, want %d\nstderr: %s", res.code, exitUsage, res.stderr)
	}
}

// TestFieldsEmptyTargetExits2 asserts an empty target argument is rejected as a
// usage error rather than issuing a request, consistent with search.
func TestFieldsEmptyTargetExits2(t *testing.T) {
	stub := fieldsStub(t, consistentMappingBody)
	res := fieldsRun(t, stub, "json", "")
	if res.code != exitUsage {
		t.Errorf("empty target exit = %d, want %d\nstderr: %s", res.code, exitUsage, res.stderr)
	}
	if len(stub.reqs) != 0 {
		t.Errorf("empty target should issue no request, got %d", len(stub.reqs))
	}
}
