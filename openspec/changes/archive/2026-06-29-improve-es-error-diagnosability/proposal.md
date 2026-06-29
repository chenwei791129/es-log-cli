## Summary

Surface Elasticsearch root-cause failure reasons in error messages, and treat partial shard failures on every query path as a non-zero-exit incomplete result rather than a silent success.

## Motivation

`es-log` already maps Elasticsearch query failures to a non-zero exit code with an empty stdout, which is good for scripting. Two diagnosability gaps remain:

1. **Root cause is hidden on hard failures.** When Elasticsearch returns a non-2xx error (e.g. an HTTP 400 `search_phase_execution_exception`), the stderr message only reports the HTTP status and the top-level `error.type`. The actionable reason — for example `Fielddata is disabled on [some_field]` — is already captured in the response body but is never extracted, so the operator cannot tell *why* the query failed without re-running the request by hand.

2. **Partial shard failures are silent on the hit path.** A `200 OK` response can still report `_shards.failed > 0`, meaning some shards returned no data. The aggregation path warns about this, but the hit (document) path does not check `_shards.failed` at all — it returns incomplete results with exit code `0`. For a log query tool this is the most dangerous outcome: the operator believes they saw every matching log line when a shard silently dropped out.

## Proposed Solution

- Extract the deepest available failure reason from the Elasticsearch error body (`error.root_cause[].reason` → `error.failed_shards[].reason.reason` → `error.reason`) and include it, together with the `error.type`, in the stderr message for non-2xx failures.
- Detect `_shards.failed > 0` on the hit path (single search and `search_after` pagination), mirroring the existing aggregation behavior.
- On any partial shard failure, adopt a hybrid contract: still emit the partial results to stdout, write a diagnostic naming the failed-shard count and reason to stderr, and exit with a new dedicated code `5` (incomplete results). This applies uniformly to the hit and aggregation paths.
- Consolidate the failure-reason extraction so the non-2xx body path and the `_shards.failures` path share one parser, and expose the `_shards.total` count so messages can read `N of M shards failed`.

## Alternatives Considered

- **Fail loud on partial (empty stdout, exit non-zero).** Rejected: discards the partial results the operator may still want; the hybrid contract keeps the data available while still signalling incompleteness via the exit code.
- **Reuse exit code 3 for partial failures.** Rejected: a dedicated code `5` lets scripts distinguish "query fully failed / unreachable" from "results returned but incomplete".

## Impact

- Affected specs: `readonly-es-client`, `log-search`, `output-format`, `search-aggregation`
- Affected code:
  - Modified:
    - internal/esclient/client.go
    - internal/cmd/exit.go
    - internal/cmd/search.go
    - internal/cmd/aggregation.go
    - README.md
  - New: (none)
  - Removed: (none)
  - Indirectly affected (no code change): internal/cmd/ls.go — it calls the shared `classifyESError`, so the improved non-2xx message (now carrying the root-cause reason) applies to `ls` errors as well. Partial-shard detection and exit code `5` do not apply to `ls`.
