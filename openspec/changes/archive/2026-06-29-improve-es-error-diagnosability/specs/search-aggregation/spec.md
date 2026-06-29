## MODIFIED Requirements

### Requirement: Partial shard failure surfacing for aggregations

When an aggregation response reports `_shards.failed` greater than zero, the command SHALL emit the aggregation results that were returned to stdout, SHALL print a diagnostic to stderr naming the failed-shard count and the failure reason, and SHALL exit with code `5` (incomplete results). The command SHALL NOT report empty or zero aggregation results silently when shards failed, and SHALL NOT exit `0` when shards failed.

#### Scenario: fielddata shard failure is surfaced and exits 5

- **WHEN** an aggregation request returns HTTP 200 with `_shards.failed` greater than zero (e.g. `Fielddata is disabled`)
- **THEN** the command SHALL emit the partial aggregations to stdout, print a diagnostic to stderr naming the failed-shard count and reason, and exit with code 5 rather than reporting zero or exiting 0

##### Example: aggregation incomplete result

- **GIVEN** an aggregation response with `_shards.total` of 5, `_shards.failed` of 2, and a first failure reason of `Fielddata is disabled on [some_field]`
- **THEN** stderr reads `error: incomplete results: 2 of 5 shards failed (Fielddata is disabled on [some_field])` and the exit code is 5
