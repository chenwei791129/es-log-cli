## MODIFIED Requirements

### Requirement: Errors on stderr with layered exit codes

All error messages SHALL be written to stderr as plain text. On a hard failure — exit codes `2`, `3`, or `4` — stdout SHALL NOT contain any result output. When an Elasticsearch query fails with a non-2xx response, the stderr message SHALL include the response `error.type` and the deepest available root-cause reason whenever one can be extracted from the error body; when no reason can be extracted, the message SHALL fall back to the status and `error.type` alone. The process exit code SHALL be layered: `0` on success, `2` for argument or configuration errors (including a missing context and mutually-exclusive time flags), `3` for connection or authentication failures, `4` when the target does not exist (Elasticsearch 404 index_not_found, surfaced as a friendly message naming the target), and `5` when Elasticsearch returns a 2xx response but reports partial shard failures (incomplete results). For exit code `5` stdout SHALL carry the partial results that were returned and stderr SHALL describe the failed-shard count and reason.

#### Scenario: Connection failure

- **WHEN** the target server is unreachable during a search
- **THEN** an error is printed to stderr, stdout is empty, and the exit code is 3

#### Scenario: Target not found

- **WHEN** a search targets a non-existent index and Elasticsearch returns 404 index_not_found
- **THEN** a friendly error naming the target is printed to stderr and the exit code is 4

#### Scenario: Query error surfaces root-cause reason

- **WHEN** a search fails with a non-2xx Elasticsearch response whose body carries a root-cause reason
- **THEN** the stderr message includes both the `error.type` and the extracted root-cause reason, stdout is empty, and the exit code is 3

##### Example: root-cause reason in message

- **GIVEN** Elasticsearch returns HTTP 400 with `error.type` of `search_phase_execution_exception` and a failed-shard reason of `Fielddata is disabled on [some_field]`
- **THEN** stderr reads `error: elasticsearch error (HTTP 400: search_phase_execution_exception — Fielddata is disabled on [some_field])`

#### Scenario: Incomplete results exit with code 5

- **WHEN** a search returns HTTP 200 but reports `_shards.failed` greater than zero
- **THEN** the partial results are written to stdout, a diagnostic naming the failed-shard count and reason is written to stderr, and the exit code is 5

##### Example: exit code mapping

| Condition | Exit code |
| --------- | --------- |
| Successful command | 0 |
| Missing context / mutually-exclusive time flags / bad arguments | 2 |
| Connection or authentication failure | 3 |
| Target index/datastream not found (404) | 4 |
| Partial shard failure (2xx with `_shards.failed` greater than zero) | 5 |
