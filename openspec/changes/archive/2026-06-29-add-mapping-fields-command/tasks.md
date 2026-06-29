## 1. GetMapping client layer flatten and cross-index type aggregation (esclient)

Implements design decision "GetMapping 在 client 層攤平並跨 index 彙整型別" and "型別衝突偵測與 per-index breakdown 的資料形狀"; satisfies spec requirement "Mapping retrieval and cross-index type aggregation".

- [x] 1.1 Add failing tests in internal/esclient/client_test.go for `GetMapping`: single-index flatten (nested `user.id → keyword`, multi-field `message`/`message.keyword`, object-without-properties `host → object`, no intermediate object node), cross-index aggregation (consistent → type set size 1, no `ByIndex`), and type conflict (divergent types → sorted distinct set size ≥2 with `ByIndex` per-index breakdown), with the returned slice ordered by field path. Verification: `go test ./internal/esclient/` fails on the missing method (Red).
- [x] 1.2 Implement `FieldType{Name, Types, ByIndex}` and `func (c *Client) GetMapping(ctx, target)` to deliver mapping retrieval and cross-index type aggregation: issue `GET /<target>/_mapping` via `escapeTarget`, flatten per the design rules, and aggregate the distinct type set across indices (`ByIndex` populated only when len(Types)>1; slice sorted by Name). Verification: tests from 1.1 pass (Green); `go test ./internal/esclient/` green.

## 2. Read-only endpoint whitelist update (esclient)

Satisfies the MODIFIED spec requirement "Read-only endpoint whitelist" (adds GetMapping to the exported method set).

- [x] 2.1 Update the read-only endpoint whitelist test so the exported method set asserts exactly `{ListAliases, ListDataStreams, Search, GetMaxResultWindow, GetMapping, Ping}` and the request-recording test accepts `GET /<target>/_mapping` as whitelisted while still rejecting any write/maintenance verb or generic request method. Verification: `go test ./internal/esclient/` green including the whitelist scenarios.

## 3. fields command with flattened field listing and conflict marking (cmd)

Implements design decisions "新增 fields 命令沿用既有 ls 命令骨架" and "三格式渲染：json 預設、table 標示衝突、jsonl per-line"; satisfies spec requirements "Inspect target mapping as flattened field paths and types", "Mark cross-index type conflicts", and "Field output formats and per-index conflict breakdown".

- [x] 3.1 Add failing tests in internal/cmd/fields_test.go covering the three command requirements — inspect target mapping as flattened field paths and types, mark cross-index type conflicts, and field output formats and per-index conflict breakdown: json default emits a row array `{name, types, conflict}` with `indices` omitted for consistent fields and present for conflicts; jsonl emits one row object per line; table renders `FIELD`/`TYPE` columns with the `⚠ conflict` marker appended on divergent rows; an invalid `-o` value exits 2. Verification: `go test ./internal/cmd/` fails on the missing command (Red).
- [x] 3.2 Implement `newFieldsCommand`/`runFields` (cobra.ExactArgs(1)) following the `runLs` pattern (`loadConfig → formatFor("fields") → buildClient → client.GetMapping → renderFields`) to inspect the target mapping as flattened field paths and types, mark cross-index type conflicts, and produce the field output formats with per-index conflict breakdown: a command-level row type maps `FieldType` to the `{name, types, conflict, indices?}` json shape and a table renderer appends the conflict marker; register it in `NewRootCommand`. Verification: tests from 3.1 pass (Green); `go test ./internal/cmd/` green.

## 4. Documentation

- [x] 4.1 Document the `es-log fields <target>` command in README.md: purpose, the three output formats with `json` as the default, and the cross-index `⚠ conflict` marking, using synthetic field/index names only (no real index or field content). Verification: README renders the new command section; content review confirms only synthetic examples.

## 5. Verification

- [x] 5.1 Run the full quality gate: `gofmt` clean, `go test ./...` green, `golangci-lint run` reports 0 issues, and `spectra analyze add-mapping-fields-command` reports no Critical/Warning findings. Verification: all four checks pass.
