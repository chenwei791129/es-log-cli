## Why

The `es-log search` command can only return raw hits today; it cannot ask Elasticsearch to aggregate server-side. A benchmark against the `itop-wc-check` workflow showed this is the single blocker preventing es-log from replacing the Grafana datasource-proxy `_msearch` path: aggregation-heavy sections (netflow `sum(bytes)` + `date_histogram`, nginx top buckets, Bifrost unique-IP counts) currently require fetching millions of raw docs and aggregating client-side, which is both slow and impractical. Because Elasticsearch aggregations ride the very same `POST /<target>/_search` endpoint es-log already uses, this capability can be added without introducing any new endpoint and without weakening the read-only-by-construction guarantee.

## What Changes

- Add a **structured aggregation** mode (A) to `es-log search` via new flags:
  - `--terms <field>[:<size>]` — terms bucketing on a keyword field (default bucket size 10), ordered by `doc_count` descending.
  - `--date-histogram <field>:<interval>` — calendar/fixed-interval time bucketing (e.g. `@timestamp:5m`).
  - `--metric <op>:<field>` (repeatable) — a metric sub-aggregation where `op` is one of `sum`, `avg`, `min`, `max`, `value_count`.
  - `--cardinality <field>` (repeatable) — approximate distinct-value count.
- Add a **raw aggregation** mode (B) via `--aggs <json>` — the caller supplies the contents of the Elasticsearch `aggs` key (a single JSON object), enabling nested sub-aggregations, ordering by sub-metric, pipeline aggregations, and any aggregation type the structured flags cannot express.
- **Mutual exclusion**: the structured aggregation flags (`--terms`/`--date-histogram`/`--metric`/`--cardinality`) and `--aggs` cannot be combined; supplying both exits with code 2. At most one bucketing flag (`--terms` xor `--date-histogram`) may be given.
- **Shared query/time flags**: `--query`, `--since`, `--from`/`--to`, `--timestamp-field`, and the target build the `query`/range portion of the request and apply to both aggregation modes — es-log wraps the supplied aggregation into a request that already carries the time range and Lucene query.
- **Default `size: 0`**: when any aggregation flag is present, the request defaults to returning no hits (buckets only); `--size N` overrides this to also return N hits alongside the aggregations.
- Render aggregation results in all three output formats: `json` (canonical/raw `aggregations` block), `jsonl` (one flattened object per bucket for structured mode), and `table` (aligned bucket columns for structured mode).
- The read-only Elasticsearch client parses the `aggregations` block from the existing `_search` response; no new endpoint or HTTP verb is added.

## Non-Goals

(none — design.md records scope boundaries and rejected approaches)

## Capabilities

### New Capabilities

- `search-aggregation`: server-side Elasticsearch aggregations on the `es-log search` command — structured flags (terms/date_histogram/metric/cardinality), a raw `--aggs` passthrough, their mutual-exclusion and shared-query semantics, the default `size: 0` behavior, and the aggregation output shapes for jsonl/json/table.

### Modified Capabilities

(none)

## Impact

- Affected specs: new `search-aggregation`
- Affected code:
  - New:
    - internal/cmd/aggregation.go
    - internal/cmd/aggregation_test.go
  - Modified:
    - internal/cmd/search.go
    - internal/esclient/client.go
    - internal/output/format.go
    - skill/SKILL.md
    - README.md
  - Removed: (none)
