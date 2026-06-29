# search-aggregation Specification

## Purpose

TBD - created by archiving change 'add-search-aggregation'. Update Purpose after archive.

## Requirements

### Requirement: Structured aggregation flags

The `es-log search` command SHALL accept structured aggregation flags that generate the Elasticsearch `aggs` body server-side. The flags SHALL be: `--terms <field>[:<size>]` (terms bucketing, default bucket size 10, ordered by `doc_count` descending), `--date-histogram <field>:<interval>` (time bucketing), `--metric <op>:<field>` (repeatable; `op` one of `sum`, `avg`, `min`, `max`, `value_count`), and `--cardinality <field>` (repeatable; approximate distinct count). At most one bucketing flag (`--terms` or `--date-histogram`) SHALL be permitted. When a bucketing flag is present, each `--metric`/`--cardinality` SHALL be emitted as a sub-aggregation inside the bucket; when no bucketing flag is present, each metric SHALL be emitted as a top-level aggregation. The generated bucketing aggregation SHALL be named `group`, each metric named `<op>_<field>`, and each cardinality named `cardinality_<field>`. For `--date-histogram`, interval suffixes `s`/`m`/`h`/`d` SHALL map to `fixed_interval` and `w`/`M`/`y` SHALL map to `calendar_interval`.

#### Scenario: terms bucketing with metric sub-aggregation

- **WHEN** the user runs `search -t app-logs --terms log.info:10 --metric sum:bytes`
- **THEN** the request body's `aggs` SHALL contain a `group` terms aggregation on `log.info` with `size` 10 holding a `sum_bytes` sub-aggregation

##### Example: generated aggs body

- **GIVEN** flags `--terms log.info:10 --metric sum:bytes --cardinality client.ip`
- **WHEN** the request body is built
- **THEN** `aggs` equals `{"group":{"terms":{"field":"log.info","size":10},"aggs":{"sum_bytes":{"sum":{"field":"bytes"}},"cardinality_client.ip":{"cardinality":{"field":"client.ip"}}}}}`

#### Scenario: top-level metric without bucketing

- **WHEN** the user runs `search -t app-logs --metric sum:bytes` with no bucketing flag
- **THEN** the request body's `aggs` SHALL contain a top-level `sum_bytes` sum aggregation and no `group` bucket

#### Scenario: date_histogram interval mapping

- **WHEN** the user runs `search -t metrics --date-histogram @timestamp:5m`
- **THEN** the `group` aggregation SHALL use `date_histogram` with `fixed_interval` of `5m`


<!-- @trace
source: add-search-aggregation
updated: 2026-06-29
code:
  - internal/cmd/aggregation.go
  - internal/cmd/search.go
  - internal/esclient/client.go
  - README.md
  - skill/SKILL.md
tests:
  - internal/cmd/aggregation_test.go
  - internal/esclient/client_test.go
-->

---
### Requirement: Raw aggregation passthrough

The `es-log search` command SHALL accept a `--aggs <json>` flag whose value is the contents of the Elasticsearch `aggs` key (a single JSON object). The supplied object SHALL be placed verbatim as the request body's `aggs` value. The command SHALL still build the `query` and time-range portion of the request from the shared query/time flags so the caller supplies only the aggregation subtree.

#### Scenario: raw aggs is wrapped with query and range

- **WHEN** the user runs `search -t netflow --since 1h --aggs '{"top_isp":{"terms":{"field":"isp"}}}'`
- **THEN** the request body SHALL contain the supplied object as `aggs` and a `query` carrying the `now-1h` range filter

#### Scenario: invalid raw aggs is rejected before request

- **WHEN** the user passes `--aggs` a value that is not a valid JSON object
- **THEN** the command SHALL print an error to stderr and exit with code 2 without issuing any request


<!-- @trace
source: add-search-aggregation
updated: 2026-06-29
code:
  - internal/cmd/aggregation.go
  - internal/cmd/search.go
  - internal/esclient/client.go
  - README.md
  - skill/SKILL.md
tests:
  - internal/cmd/aggregation_test.go
  - internal/esclient/client_test.go
-->

---
### Requirement: Aggregation flag mutual exclusion and validation

The structured aggregation flags (`--terms`, `--date-histogram`, `--metric`, `--cardinality`) and `--aggs` SHALL be mutually exclusive. Supplying any structured aggregation flag together with `--aggs`, or supplying both `--terms` and `--date-histogram`, SHALL cause the command to exit with code 2. A `--metric` value that is not `op:field` or whose `op` is outside the allowed set, a `--terms` size that is not a positive integer, and a `--date-histogram` value missing its interval SHALL each cause exit code 2. All such errors SHALL be detected during flag validation before any request is issued.

#### Scenario: structured flag combined with raw aggs

- **WHEN** the user runs `search -t app-logs --terms host --aggs '{"x":{"max":{"field":"y"}}}'`
- **THEN** the command SHALL print an error to stderr and exit with code 2

#### Scenario: two bucketing flags

- **WHEN** the user runs `search -t app-logs --terms host --date-histogram @timestamp:5m`
- **THEN** the command SHALL print an error to stderr and exit with code 2

#### Scenario: malformed metric

- **WHEN** the user runs `search -t app-logs --metric median:bytes`
- **THEN** the command SHALL reject the unknown `op` and exit with code 2


<!-- @trace
source: add-search-aggregation
updated: 2026-06-29
code:
  - internal/cmd/aggregation.go
  - internal/cmd/search.go
  - internal/esclient/client.go
  - README.md
  - skill/SKILL.md
tests:
  - internal/cmd/aggregation_test.go
  - internal/esclient/client_test.go
-->

---
### Requirement: Shared query and time range with aggregation

The `--query`, `--since`, `--from`/`--to`, and `--timestamp-field` flags SHALL build the `query` and range-filter portion of the request and SHALL apply to both the structured and raw aggregation modes.

#### Scenario: lucene query and range apply to aggregation

- **WHEN** the user runs `search -t app-logs -q 'level:error' --since 24h --terms service`
- **THEN** the request body SHALL contain a `bool` query whose `must` carries the `level:error` query_string and whose `filter` carries the `now-24h` range, alongside the generated `aggs`


<!-- @trace
source: add-search-aggregation
updated: 2026-06-29
code:
  - internal/cmd/aggregation.go
  - internal/cmd/search.go
  - internal/esclient/client.go
  - README.md
  - skill/SKILL.md
tests:
  - internal/cmd/aggregation_test.go
  - internal/esclient/client_test.go
-->

---
### Requirement: Default hit size during aggregation

When any aggregation flag (structured or `--aggs`) is present, the request SHALL default to `size` 0 so that no hits are returned. An explicit `--size N` SHALL override the default so that N hits are returned alongside the aggregations.

#### Scenario: aggregation defaults to no hits

- **WHEN** the user runs `search -t app-logs --terms service` without `--size`
- **THEN** the request body SHALL set `size` to 0 and the rendered output SHALL contain buckets and no hits

#### Scenario: size override returns hits with aggregations

- **WHEN** the user runs `search -t app-logs --terms service --size 5`
- **THEN** the request body SHALL set `size` to 5 and the output SHALL include both the aggregations and up to 5 hits


<!-- @trace
source: add-search-aggregation
updated: 2026-06-29
code:
  - internal/cmd/aggregation.go
  - internal/cmd/search.go
  - internal/esclient/client.go
  - README.md
  - skill/SKILL.md
tests:
  - internal/cmd/aggregation_test.go
  - internal/esclient/client_test.go
-->

---
### Requirement: Aggregation output formats

Aggregation results SHALL render in all three output formats. In `json`, the output SHALL be `{"total":N,"aggregations":<block>,"hits":[...]}` where `aggregations` carries the canonical-named block for structured mode or the caller-named block for raw mode, and `hits` is empty when `size` is 0. In `jsonl`, structured bucketing mode SHALL emit one flattened object per bucket carrying `key`, `doc_count`, and each requested metric; structured top-level-metric mode SHALL emit a single object of metric values; raw mode SHALL emit the `aggregations` object as a single line. In `table`, structured bucketing mode SHALL render aligned columns `key`, `doc_count`, and one column per metric using the shared column renderer; raw mode SHALL render the `aggregations` JSON.

#### Scenario: jsonl flattens structured buckets

- **WHEN** the user runs `search -t app-logs --terms log.info --metric sum:bytes -o jsonl`
- **THEN** each output line SHALL be an object with `key`, `doc_count`, and `sum_bytes`

##### Example: flattened bucket line

- **GIVEN** a terms bucket `{"key":"timeout","doc_count":42,"sum_bytes":{"value":1024}}`
- **WHEN** rendered as jsonl
- **THEN** the line SHALL be `{"key":"timeout","doc_count":42,"sum_bytes":1024}`

#### Scenario: raw mode renders the aggregations block under json

- **WHEN** the user runs `search -t netflow --aggs '{"top_isp":{"terms":{"field":"isp"}}}' -o json`
- **THEN** the `aggregations` field SHALL contain the `top_isp` block as returned by Elasticsearch


<!-- @trace
source: add-search-aggregation
updated: 2026-06-29
code:
  - internal/cmd/aggregation.go
  - internal/cmd/search.go
  - internal/esclient/client.go
  - README.md
  - skill/SKILL.md
tests:
  - internal/cmd/aggregation_test.go
  - internal/esclient/client_test.go
-->

---
### Requirement: Partial shard failure surfacing for aggregations

When an aggregation response reports `_shards.failed` greater than zero, the command SHALL emit the aggregation results that were returned to stdout, SHALL print a diagnostic to stderr naming the failed-shard count and the failure reason, and SHALL exit with code `5` (incomplete results). The command SHALL NOT report empty or zero aggregation results silently when shards failed, and SHALL NOT exit `0` when shards failed.

#### Scenario: fielddata shard failure is surfaced and exits 5

- **WHEN** an aggregation request returns HTTP 200 with `_shards.failed` greater than zero (e.g. `Fielddata is disabled`)
- **THEN** the command SHALL emit the partial aggregations to stdout, print a diagnostic to stderr naming the failed-shard count and reason, and exit with code 5 rather than reporting zero or exiting 0

##### Example: aggregation incomplete result

- **GIVEN** an aggregation response with `_shards.total` of 5, `_shards.failed` of 2, and a first failure reason of `Fielddata is disabled on [some_field]`
- **THEN** stderr reads `error: incomplete results: 2 of 5 shards failed (Fielddata is disabled on [some_field])` and the exit code is 5


<!-- @trace
source: improve-es-error-diagnosability
updated: 2026-06-29
code:
  - internal/cmd/search.go
  - internal/esclient/client.go
  - internal/cmd/exit.go
  - internal/cmd/aggregation.go
  - README.md
tests:
  - internal/cmd/aggregation_test.go
  - internal/esclient/client_test.go
  - internal/cmd/search_test.go
  - internal/cmd/exit_test.go
-->

---
### Requirement: Read-only client aggregation parsing

The read-only Elasticsearch client SHALL parse the `aggregations` block from the existing `_search` response. No new endpoint, HTTP verb, or generic request method SHALL be introduced; aggregation requests SHALL continue to be issued through the existing `POST /<target>/_search` method.

#### Scenario: aggregations parsed from existing search endpoint

- **WHEN** an aggregation request is issued
- **THEN** it SHALL go through the existing `POST /<target>/_search` method and the client SHALL expose the response `aggregations` block to the caller

<!-- @trace
source: add-search-aggregation
updated: 2026-06-29
code:
  - internal/cmd/aggregation.go
  - internal/cmd/search.go
  - internal/esclient/client.go
  - README.md
  - skill/SKILL.md
tests:
  - internal/cmd/aggregation_test.go
  - internal/esclient/client_test.go
-->