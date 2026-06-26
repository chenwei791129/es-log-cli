## ADDED Requirements

### Requirement: Target selection

The `es-log search` command SHALL require a target (alias or datastream) supplied via `--target/-t` or as a positional argument. The search SHALL be issued as `POST /<target>/_search`. When no target is supplied, the CLI SHALL print an error to stderr and exit with code 2.

#### Scenario: Target via flag

- **WHEN** the user runs `es-log -c prod search -t app-logs`
- **THEN** the request is sent to `POST /app-logs/_search`

#### Scenario: Missing target

- **WHEN** the user runs `es-log -c prod search` with no target
- **THEN** the CLI prints an error to stderr and exits with code 2

### Requirement: Lucene query

The `--query/-q` flag SHALL accept a Lucene query string that is sent to Elasticsearch as a `query_string` query. When `--query` is omitted, the search SHALL use `match_all`.

#### Scenario: Query string applied

- **WHEN** the user runs `es-log -c prod search -t app-logs -q 'level:error'`
- **THEN** the request body contains a `query_string` query with `level:error`

#### Scenario: Omitted query defaults to match_all

- **WHEN** the user runs `es-log -c prod search -t app-logs` with no `--query`
- **THEN** the request body uses `match_all`

### Requirement: Time range filtering

The `--since <dur>` flag SHALL accept a relative duration (`15m`, `1h`, `24h`, `7d`) and translate it into a range filter on the timestamp field covering from `now - dur` to `now`. The `--from <RFC3339>` and `--to <RFC3339>` flags SHALL define an absolute range. `--since` SHALL be mutually exclusive with `--from`/`--to`; supplying both SHALL cause an error to stderr and exit code 2. The timestamp field SHALL default to `@timestamp` and be overridable by `--timestamp-field`. When `--timestamp-field` is set, both the range filter AND the default sort field SHALL use the overridden field (the default sort SHALL NOT remain hardcoded to `@timestamp`).

#### Scenario: Relative time range

- **WHEN** the user runs `es-log -c prod search -t app-logs --since 1h`
- **THEN** the request body contains a range filter on `@timestamp` with a lower bound of one hour before now

#### Scenario: Mutually exclusive time flags

- **WHEN** the user supplies both `--since 1h` and `--from 2026-01-01T00:00:00Z`
- **THEN** the CLI prints an error to stderr and exits with code 2

#### Scenario: Custom timestamp field retargets range and default sort

- **WHEN** the user runs `es-log -c prod search -t app-logs --since 1h --timestamp-field event.created` with no `--sort`
- **THEN** the range filter is applied on `event.created`
- **AND** the default sort is `event.created` descending (not `@timestamp`)

### Requirement: Result size and unbounded pagination

The `--size/-n` flag (short flag `-n`, alias `--limit`, default 50) SHALL bound the number of hits fetched from Elasticsearch. The window value `W` SHALL be the target's `max_result_window`, obtained by calling `GetMaxResultWindow` ONLY when needed: when `--size 0`, or when `--size N` with `N > 10000`. For `0 < N <= 10000` the CLI SHALL issue a single search with `size: N` WITHOUT calling `GetMaxResultWindow`. When `GetMaxResultWindow` is unavailable, errors, or returns no explicit value, `W` SHALL default to `10000`; when the target spans multiple indices with differing windows, `W` SHALL be the minimum across those indices. When `--size N` with `N > W`, the CLI SHALL print a capping warning to stderr and issue a single search with `size: W`. When `--size 0`, the CLI SHALL retrieve all matching results by paging with `search_after` using a page size of `W` and a sort key whose final element is the `_doc` tiebreaker, continuing until a page returns no hits. The CLI SHALL NOT use `_shard_doc` (valid only within a Point-in-Time context, which is out of scope).

`--size` SHALL bound the hits fetched from Elasticsearch, NOT the count emitted after client-side `--include/--exclude` filtering; the emitted record count is therefore not guaranteed to equal `N`, and the CLI SHALL NOT fetch additional pages to refill a bounded result.

#### Scenario: Size within default window skips the settings lookup

- **WHEN** the user runs `--size 100`
- **THEN** a single search is issued with `size: 100`, no `GetMaxResultWindow` request is made, and no warning is printed

#### Scenario: Size exceeds window is capped

- **WHEN** `max_result_window` is 10000 and the user runs `--size 50000`
- **THEN** `GetMaxResultWindow` is called, a capping warning is printed to stderr, and a single search is issued with `size: 10000`

#### Scenario: Size zero pages through all results

- **WHEN** `max_result_window` is 10000, a target matches 25000 documents, and the user runs `--size 0`
- **THEN** the CLI issues successive `_search` requests carrying `search_after` and returns all 25000 hits

#### Scenario: Window lookup failure falls back to 10000

- **WHEN** the user runs `--size 0` and `GetMaxResultWindow` returns no explicit value or errors
- **THEN** `W` defaults to 10000 and pagination uses `size: 10000` pages
- **AND** the CLI does NOT issue `size: 0` pages

##### Example: pagination, capping, and window-lookup behavior

| max_result_window | --size | Matching docs | GetMaxResultWindow called | Requests issued | Hits fetched | Stderr warning |
| ----------------- | ------ | ------------- | ------------------------- | --------------- | ------------ | -------------- |
| 10000 | 100 | 25000 | no | 1 (size=100) | 100 | no |
| 10000 | 50000 | 25000 | yes | 1 (size=10000) | 10000 | yes (capping) |
| 10000 | 0 | 25000 | yes | 3 (search_after) | 25000 | no |
| (lookup fails) | 0 | 25000 | yes | 3 (size=10000) | 25000 | no |

### Requirement: Source field projection and sorting

The `--fields <a,b,c>` flag SHALL restrict the returned `_source` to the listed fields. The `--sort <field:asc|desc>` flag SHALL set the sort order, defaulting to `@timestamp:desc`.

#### Scenario: Field projection

- **WHEN** the user runs `--fields message,level`
- **THEN** the request body restricts `_source` to `message` and `level`

#### Scenario: Default sort

- **WHEN** the user runs a search without `--sort`
- **THEN** the request body sorts by `@timestamp` descending

### Requirement: Client-side regex filtering

The `--include/-i` and `--exclude/-e` flags SHALL each be repeatable and apply client-side regex filtering against the serialized JSON string of each hit's `_source`. The match target SHALL be the `_source` JSON regardless of the `--output` format (it SHALL NOT include the `_id`/`_index`/`_score` envelope), so the same patterns produce the same filtered set in both `jsonl` and `json` output. `--include` SHALL keep only records whose `_source` JSON matches at least one include pattern. `--exclude` SHALL drop records whose `_source` JSON matches any exclude pattern. Filtering SHALL be applied after `--fields` projection, so patterns match only against the projected fields when `--fields` is set.

#### Scenario: Include keeps matching records

- **WHEN** results contain records whose `_source` JSON contains `timeout` and others that do not, and the user runs `--include timeout`
- **THEN** only records whose `_source` JSON matches `timeout` are emitted

#### Scenario: Exclude drops matching records

- **WHEN** the user runs `--exclude healthcheck`
- **THEN** records whose `_source` JSON matches `healthcheck` are removed from the output

#### Scenario: Same filter result across output formats

- **WHEN** the same `--include app` is run once with `-o jsonl` and once with `-o json` against identical hits where the substring `app` appears only in the `_index` envelope value and not in any `_source`
- **THEN** both invocations emit the same (empty) filtered set, because matching ignores the envelope
