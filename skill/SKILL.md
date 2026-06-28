---
name: es-log
description: Query Elasticsearch logs in read-only mode. Use when you need to fetch, search, or filter logs from ES (beats → index alias or datastream) and require "never run destructive operations" plus "agent-friendly JSONL output". Triggers on situations like "query ES logs", "search elasticsearch logs", "fetch errors from app-logs".
---

# es-log

`es-log` is a read-only Elasticsearch log query CLI. It guarantees at the code level that only read requests are issued (no `PUT`/`DELETE`/`PATCH`, no `_rollover`/`_reindex`). Output defaults to JSONL suited for `jq`/pipe, and it uses tiered exit codes so programs can branch reliably.

## When to use

- You need to fetch or search logs from Elasticsearch (alias or datastream).
- You need a safe query entry point that cannot accidentally hit destructive endpoints.
- You need structured output consumable line-by-line by `jq`, or a full payload with metadata/total.

Do not use for: any write/maintenance operation (intentionally not provided), live tail (`--follow` not yet supported), or ES 7.x (only 8.x+ is supported).

## Prerequisites

- `es-log` is installed on `PATH`.
- The config file `~/.config/es-log/config.yaml` exists (see README). Override with `--config` or `$ES_LOG_CONFIG`.
- Every ES command must specify a context: use `--context/-c <name>` or set `$ES_LOG_CONTEXT`. There is no hidden current-context.

## Global flags

- `-c, --context <name>`: select the context to connect to (or set `$ES_LOG_CONTEXT`).
- `-o, --output <jsonl|json|table>`: output format. `search` defaults to `jsonl`; other commands default to `json`.
- `--quiet`: suppress warnings and noise so stdout carries only results.
- `--config <path>`: config file path.

## Commands

### `es-log config get-contexts`

List all context names in the config file (no context required).

```bash
es-log config get-contexts            # JSON string array
es-log config get-contexts -o table
```

### `es-log config view`

Print the resolved config. `api-key`/`password` are always redacted to `***` (no context required, secrets not expanded).

### `es-log -c <ctx> ping`

Hit `GET /_cluster/health` to verify connectivity and authentication. Exit 0 on success; exit 3 on connection/auth failure.

### `es-log -c <ctx> ls [aliases|datastreams]`

List aliases and datastreams together; each row has `name` and `type`. `ls aliases` / `ls datastreams` lists a single type only. Datastream rows carry `backing_indices_count`; alias rows carry `index_count`.

```bash
es-log -c prod ls                     # combined view (JSON array)
es-log -c prod ls datastreams -o table
```

### `es-log -c <ctx> search -t <target> [flags]`

Run `POST /<target>/_search` against `<target>` (an alias or datastream). The target can also be passed as a positional argument.

Key flags:

- `-q, --query <lucene>`: Lucene query string (`match_all` when omitted).
- `--since <dur>`: relative time range, e.g. `15m`/`1h`/`24h`/`7d`. Mutually exclusive with `--from`/`--to`.
- `--from <RFC3339> --to <RFC3339>`: absolute time range.
- `--timestamp-field <field>`: timestamp field (default `@timestamp`), applied to both the range filter and the default sort.
- `-n, --size <N>` (alias `--limit`, default 50): fetch limit. `--size 0` fetches **everything** via `search_after` (auto-paginates beyond `max_result_window`). An `N` exceeding the window is truncated with a warning on stderr.
- `--fields <a,b,c>`: return only the specified `_source` fields.
- `--sort <field:asc|desc>`: sort order (default `<timestamp-field>:desc`).
- `-i, --include <regex>` / `-e, --exclude <regex>` (repeatable): client-side regex refinement against each hit's serialized `_source` JSON (independent of the `-o` format).

#### Lucene query examples

```bash
# Errors within the last hour
es-log -c prod search -t app-logs -q 'level:error' --since 1h

# Multiple conditions + field projection + client-side refinement
es-log -c prod search -t app-logs \
  -q 'level:error AND service:checkout' \
  --fields '@timestamp,level,message' \
  --include 'timeout|deadline' --exclude 'healthcheck' \
  --since 24h

# Fetch all matching results (auto-paginated)
es-log -c prod search -t app-logs -q 'status:500' --size 0 > errors.jsonl

# Absolute time range + custom timestamp field
es-log -c prod search -t metrics -q '*' \
  --timestamp-field event.created \
  --from 2026-06-01T00:00:00Z --to 2026-06-02T00:00:00Z
```

## Output formats

- **jsonl** (`search` default): one hit per line = raw `_source`, with no `_id`/`_index`/`_score` wrapper. Best for `jq -c`, `grep`, `head`, and streaming.
- **json**: `search` returns `{"total":N,"hits":[{"_id","_index","_score","_source"}]}`; `ls` returns an array of row objects; `config get-contexts` returns an array of name strings; `config view` returns the config object.
- **table**: human-readable aligned columns.

`--include/--exclude` always match against the `_source` JSON, so the same pattern set yields identical filtering under both jsonl and json.

## Exit codes

| code | meaning |
| ---- | ---- |
| 0 | success |
| 2 | argument/config error (missing context, `--since` conflicting with `--from/--to`, missing target, unset secret `${ENV_VAR}`) |
| 3 | connection or authentication failure |
| 4 | target not found (ES 404 index_not_found) |

Errors always go to stderr as plain text; on failure, stdout never contains partial results.
