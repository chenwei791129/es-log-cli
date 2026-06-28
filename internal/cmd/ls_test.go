package cmd

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"testing"
)

const (
	aliasBody = `{
		"index-1":{"aliases":{"app-logs":{}}},
		"index-2":{"aliases":{"app-logs":{},"web-logs":{}}}
	}`
	dataStreamBody = `{"data_streams":[{"name":"metrics","indices":[{},{},{}]}]}`
)

// Independent fixtures for the --show-hidden behavior tests. Kept separate from
// aliasBody / dataStreamBody so the row-count assertions in TestLsCombined etc.
// are not coupled to these dot-prefixed entries.
const (
	hiddenAliasBody = `{
		"idx-k":{"aliases":{".kibana":{}}},
		"idx-a":{"aliases":{"app-logs":{}}}
	}`
	hiddenDataStreamBody = `{"data_streams":[{"name":".items-default","indices":[{}]},{"name":"metrics","indices":[{},{},{}]}]}`
	// allHiddenAliasBody exposes only dot-prefixed aliases for the empty-result scenario.
	allHiddenAliasBody  = `{"idx-1":{"aliases":{".kibana":{},".security":{}}}}`
	emptyDataStreamBody = `{"data_streams":[]}`
)

// hiddenStub serves the provided alias and data_stream bodies.
func hiddenStub(t *testing.T, aliasResp, dsResp string) *esStub {
	return newESStub(t, func(r recordedReq) (int, string) {
		switch r.path {
		case "/_alias":
			return 200, aliasResp
		case "/_data_stream":
			return 200, dsResp
		default:
			return 404, `{}`
		}
	})
}

// rowNames extracts the Name column from decoded rows.
func rowNames(rows []lsRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Name)
	}
	return out
}

// sameNameSet reports whether a and b contain the same names regardless of order.
func sameNameSet(a, b []string) bool {
	ac := slices.Clone(a)
	bc := slices.Clone(b)
	sort.Strings(ac)
	sort.Strings(bc)
	return slices.Equal(ac, bc)
}

// lsTableNames runs `ls <args>` with table output and returns the NAME column.
func lsTableNames(t *testing.T, stub *esStub, args ...string) []string {
	t.Helper()
	res := lsRun(t, stub, "table", args...)
	var names []string
	for i, line := range strings.Split(strings.TrimRight(res.stdout, "\n"), "\n") {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue // skip header and blank lines
		}
		names = append(names, strings.Fields(line)[0])
	}
	return names
}

// TestLsHidesDotPrefixedByDefault covers scenario "Dot-prefixed targets hidden by default".
func TestLsHidesDotPrefixedByDefault(t *testing.T) {
	got := rowNames(lsRows(t, hiddenStub(t, hiddenAliasBody, hiddenDataStreamBody)))
	for _, n := range got {
		if strings.HasPrefix(n, ".") {
			t.Errorf("dot-prefixed target leaked into default ls: %q (all: %v)", n, got)
		}
	}
	if !slices.Contains(got, "app-logs") || !slices.Contains(got, "metrics") {
		t.Errorf("default ls dropped a regular target: %v", got)
	}
}

// TestLsShowHiddenIncludesDotPrefixed covers scenario "--show-hidden includes dot-prefixed targets".
func TestLsShowHiddenIncludesDotPrefixed(t *testing.T) {
	got := rowNames(lsRows(t, hiddenStub(t, hiddenAliasBody, hiddenDataStreamBody), "--show-hidden"))
	for _, want := range []string{".kibana", "app-logs", ".items-default", "metrics"} {
		if !slices.Contains(got, want) {
			t.Errorf("--show-hidden output missing %q: %v", want, got)
		}
	}
}

// TestLsDatastreamsSubcommandInheritsFlag covers scenario "Subcommands inherit the flag and the default".
func TestLsDatastreamsSubcommandInheritsFlag(t *testing.T) {
	stub := hiddenStub(t, hiddenAliasBody, hiddenDataStreamBody)
	def := rowNames(lsRows(t, stub, "datastreams"))
	if slices.Contains(def, ".items-default") || !slices.Contains(def, "metrics") {
		t.Errorf("default ls datastreams wrong: %v", def)
	}
	all := rowNames(lsRows(t, stub, "datastreams", "--show-hidden"))
	if !slices.Contains(all, ".items-default") {
		t.Errorf("ls datastreams --show-hidden missing .items-default: %v", all)
	}
}

// TestLsAllHiddenAliasesEmptyNotError covers scenario "All-hidden listing yields an empty result, not an error".
func TestLsAllHiddenAliasesEmptyNotError(t *testing.T) {
	stub := hiddenStub(t, allHiddenAliasBody, emptyDataStreamBody)
	def := lsRows(t, stub, "aliases")
	if len(def) != 0 {
		t.Errorf("want empty rows when all aliases hidden, got %v", rowNames(def))
	}
	all := rowNames(lsRows(t, stub, "aliases", "--show-hidden"))
	if !slices.Contains(all, ".kibana") || !slices.Contains(all, ".security") {
		t.Errorf("ls aliases --show-hidden should list hidden aliases: %v", all)
	}
}

// TestLsFormatsListIdenticalTargets covers scenario where table and json list the same targets per flag value.
func TestLsFormatsListIdenticalTargets(t *testing.T) {
	for _, args := range [][]string{nil, {"--show-hidden"}} {
		stub := hiddenStub(t, hiddenAliasBody, hiddenDataStreamBody)
		jsonNames := rowNames(lsRows(t, stub, args...))
		tableNames := lsTableNames(t, stub, args...)
		// Anchor on a known non-empty set so an over-filtering regression that
		// empties both formats can't satisfy the parity check vacuously.
		if !slices.Contains(jsonNames, "app-logs") || !slices.Contains(jsonNames, "metrics") {
			t.Errorf("expected regular targets present for args %v: %v", args, jsonNames)
		}
		if !sameNameSet(jsonNames, tableNames) {
			t.Errorf("table/json target sets differ for args %v: json=%v table=%v", args, jsonNames, tableNames)
		}
	}
}

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

// lsRun executes `ls <args>` against stub with the given output format and
// returns the captured result, failing on a non-zero exit code.
func lsRun(t *testing.T, stub *esStub, format string, args ...string) cliResult {
	t.Helper()
	cfg := writeTestConfig(t, stub.url())
	full := append([]string{"ls"}, args...)
	full = append(full, "-c", "test", "-o", format, "--config", cfg)
	res := runCLI(t, context.Background(), full...)
	if res.code != 0 {
		t.Fatalf("ls exit %d: %s", res.code, res.stderr)
	}
	return res
}

// lsRows runs `ls <args>` with json output and decodes the row array.
func lsRows(t *testing.T, stub *esStub, args ...string) []lsRow {
	t.Helper()
	res := lsRun(t, stub, "json", args...)
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
