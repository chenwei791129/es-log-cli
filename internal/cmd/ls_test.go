package cmd

import (
	"context"
	"encoding/json"
	"testing"
)

const (
	aliasBody = `{
		"index-1":{"aliases":{"app-logs":{}}},
		"index-2":{"aliases":{"app-logs":{},"web-logs":{}}}
	}`
	dataStreamBody = `{"data_streams":[{"name":"metrics","indices":[{},{},{}]}]}`
)

func lsStub(t *testing.T) *esStub {
	return newESStub(t, func(r recordedReq) (int, string) {
		switch r.path {
		case "/_alias":
			return 200, aliasBody
		case "/_data_stream":
			return 200, dataStreamBody
		default:
			return 404, `{}`
		}
	})
}

// lsRows runs `ls <args>` with json output and decodes the row array.
func lsRows(t *testing.T, stub *esStub, args ...string) []lsRow {
	t.Helper()
	cfg := writeTestConfig(t, stub.url())
	full := append([]string{"ls"}, args...)
	full = append(full, "-c", "test", "-o", "json", "--config", cfg)
	res := runCLI(t, context.Background(), full...)
	if res.code != 0 {
		t.Fatalf("ls exit %d: %s", res.code, res.stderr)
	}
	var rows []lsRow
	if err := json.Unmarshal([]byte(res.stdout), &rows); err != nil {
		t.Fatalf("ls output not a JSON array: %v\n%s", err, res.stdout)
	}
	return rows
}

func TestLsCombined(t *testing.T) {
	rows := lsRows(t, lsStub(t))
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d: %+v", len(rows), rows)
	}
	types := map[string]string{}
	for _, r := range rows {
		types[r.Name] = r.Type
	}
	if types["app-logs"] != "alias" || types["web-logs"] != "alias" || types["metrics"] != "datastream" {
		t.Errorf("type tagging wrong: %v", types)
	}
}

func TestLsAliasesOnly(t *testing.T) {
	rows := lsRows(t, lsStub(t), "aliases")
	for _, r := range rows {
		if r.Type != "alias" {
			t.Errorf("non-alias row in `ls aliases`: %+v", r)
		}
	}
	if len(rows) != 2 {
		t.Errorf("want 2 alias rows, got %d", len(rows))
	}
}

func TestLsDatastreamsOnly(t *testing.T) {
	rows := lsRows(t, lsStub(t), "datastreams")
	for _, r := range rows {
		if r.Type != "datastream" {
			t.Errorf("non-datastream row in `ls datastreams`: %+v", r)
		}
	}
	if len(rows) != 1 {
		t.Errorf("want 1 datastream row, got %d", len(rows))
	}
}

func TestLsCountFields(t *testing.T) {
	rows := lsRows(t, lsStub(t))
	for _, r := range rows {
		switch r.Name {
		case "app-logs":
			if r.IndexCount == nil || *r.IndexCount != 2 {
				t.Errorf("app-logs index_count = %v, want 2", r.IndexCount)
			}
			if r.BackingIndicesCount != nil {
				t.Errorf("alias should not carry backing_indices_count: %+v", r)
			}
		case "metrics":
			if r.BackingIndicesCount == nil || *r.BackingIndicesCount != 3 {
				t.Errorf("metrics backing_indices_count = %v, want 3", r.BackingIndicesCount)
			}
			if r.IndexCount != nil {
				t.Errorf("datastream should not carry index_count: %+v", r)
			}
		}
	}
}

// TestLsJSONArray asserts the json shape is an array iterable with jq '.[]'.
func TestLsJSONArray(t *testing.T) {
	cfg := writeTestConfig(t, lsStub(t).url())
	res := runCLI(t, context.Background(), "ls", "-c", "test", "-o", "json", "--config", cfg)
	if res.code != 0 {
		t.Fatal(res.stderr)
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(res.stdout), &arr); err != nil {
		t.Fatalf("not a JSON array: %v", err)
	}
	for _, row := range arr {
		if _, ok := row["name"]; !ok {
			t.Errorf("row missing name: %v", row)
		}
		if _, ok := row["type"]; !ok {
			t.Errorf("row missing type: %v", row)
		}
	}
}
