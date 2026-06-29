# inspect-mapping Specification

## Purpose

TBD - created by archiving change 'add-mapping-fields-command'. Update Purpose after archive.

## Requirements

### Requirement: Inspect target mapping as flattened field paths and types

The `es-log fields <target>` command SHALL fetch the target's mapping and present it as a flattened list of field paths and their types. The command SHALL require an active context and SHALL accept exactly one `<target>` argument, passed verbatim to the client (supporting a single index, alias, datastream, comma-separated targets, or wildcard). Flattening SHALL follow these rules: a leaf field (carrying a `type` and no sub-`properties`) SHALL produce one entry `path → type`; a field carrying sub-`properties` SHALL be recursed into with its name as a dotted prefix and SHALL NOT itself produce an entry; a field carrying neither `type` nor `properties` (a dynamic or empty object) SHALL produce one entry `path → object`; and a leaf field carrying multi-fields (`fields`) SHALL additionally produce one entry per multi-field as `path.subname → subtype`. Entries SHALL be ordered by field path so that all output formats list fields in the same stable order.

#### Scenario: Nested object flattened to dotted path

- **WHEN** the target mapping has an object field `user` with a sub-field `id` of type `keyword`
- **THEN** `es-log -c prod fields <target>` lists a field named `user.id` with type `keyword`
- **AND** no entry named `user` (the intermediate object) appears

#### Scenario: Multi-field flattened as a separate entry

- **WHEN** a leaf field `message` of type `text` declares a multi-field `keyword` of type `keyword`
- **THEN** the output lists both `message` with type `text` and `message.keyword` with type `keyword`

#### Scenario: Object without sub-properties typed as object

- **WHEN** a field `host` is declared with neither an explicit `type` nor sub-`properties`
- **THEN** the output lists `host` with type `object`


<!-- @trace
source: add-mapping-fields-command
updated: 2026-06-29
code:
  - README.md
  - internal/cmd/fields.go
  - internal/cmd/root.go
  - internal/esclient/client.go
tests:
  - internal/cmd/fields_test.go
  - internal/esclient/client_test.go
-->

---
### Requirement: Mark cross-index type conflicts

When the target resolves to multiple indices, the command SHALL aggregate, for each field path, the distinct set of types observed across those indices. A field whose distinct type set has size one is consistent. A field whose distinct type set has size two or more is a type conflict, and the command SHALL mark it explicitly so the user can see the divergence before querying. A field that exists in only some of the indices but carries a single consistent type SHALL NOT be marked as a conflict.

#### Scenario: Same field with divergent types is a conflict

- **WHEN** the target resolves to two indices where field `tags` is `text` in one and `keyword` in the other
- **THEN** the `tags` entry is marked as a type conflict and lists both `text` and `keyword`

#### Scenario: Field absent from some indices is not a conflict

- **WHEN** field `url.path` of type `keyword` exists in one resolved index and is absent from another, with no type divergence
- **THEN** the `url.path` entry is listed with type `keyword`
- **AND** it is NOT marked as a conflict

##### Example: partial absence versus type conflict

- **GIVEN** target `app-logs,web-logs` where `tags` is `text` in `app-logs` and `keyword` in `web-logs`, and `url.path` is `keyword` in `web-logs` only
- **WHEN** the user runs `es-log -c prod fields app-logs,web-logs`
- **THEN** `tags` is marked as a conflict listing `keyword, text`
- **AND** `url.path` is listed as `keyword` without a conflict mark


<!-- @trace
source: add-mapping-fields-command
updated: 2026-06-29
code:
  - README.md
  - internal/cmd/fields.go
  - internal/cmd/root.go
  - internal/esclient/client.go
tests:
  - internal/cmd/fields_test.go
  - internal/esclient/client_test.go
-->

---
### Requirement: Field output formats and per-index conflict breakdown

The `es-log fields` command SHALL support the `json`, `jsonl`, and `table` formats and SHALL default to `json` (consistent with the non-search default). In `json` the command SHALL emit a JSON array of row objects of the shape `{"name": <path>, "types": [<type>...], "conflict": <bool>}`, where `types` is the distinct type set ordered ascending and `conflict` is true when that set has size two or more; for a conflicting field the object SHALL additionally carry `"indices": {<index>: <type>}` giving the per-index type breakdown, and for a non-conflicting field the `indices` key SHALL be omitted. In `jsonl` the command SHALL emit one such row object per line. In `table` the command SHALL render `FIELD` and `TYPE` columns where `TYPE` is the comma-joined type set, and for a conflicting field SHALL append a `⚠ conflict` marker to that row.

#### Scenario: json default omits indices for consistent fields

- **WHEN** the user runs `es-log -c prod fields <target>` with no `--output` and field `@timestamp` is `date` across all indices
- **THEN** the `@timestamp` row is `{"name": "@timestamp", "types": ["date"], "conflict": false}`
- **AND** the row has no `indices` key

#### Scenario: json carries per-index breakdown for a conflict

- **WHEN** the user runs `es-log -c prod fields <target>` with `-o json` and `tags` is `text` in index `app-logs` and `keyword` in index `web-logs`
- **THEN** the `tags` row has `"conflict": true`, `"types": ["keyword", "text"]`, and `"indices": {"app-logs": "text", "web-logs": "keyword"}`

#### Scenario: table appends a conflict marker

- **WHEN** the user runs `es-log -c prod fields <target>` with `-o table` and `tags` has divergent types `text` and `keyword`
- **THEN** the `tags` row's `TYPE` column reads `keyword, text  ⚠ conflict`

#### Scenario: invalid output value rejected

- **WHEN** the user runs `es-log -c prod fields <target> -o yaml`
- **THEN** the command exits with code 2 and prints an error to stderr

<!-- @trace
source: add-mapping-fields-command
updated: 2026-06-29
code:
  - README.md
  - internal/cmd/fields.go
  - internal/cmd/root.go
  - internal/esclient/client.go
tests:
  - internal/cmd/fields_test.go
  - internal/esclient/client_test.go
-->