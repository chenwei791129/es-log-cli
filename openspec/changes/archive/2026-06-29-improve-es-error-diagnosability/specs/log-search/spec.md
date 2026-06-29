## ADDED Requirements

### Requirement: Partial shard failure surfacing for searches

When a hit (document) search returns HTTP 200 but reports `_shards.failed` greater than zero, the `es-log search` command SHALL emit the hits that were returned to stdout, SHALL print a diagnostic to stderr naming the failed-shard count and the failure reason, and SHALL exit with code `5` (incomplete results). The command SHALL NOT return incomplete hits silently with exit code `0`. This SHALL apply to both a single bounded search and unbounded `search_after` pagination; during pagination, if any page reports `_shards.failed` greater than zero, the command SHALL mark the overall result incomplete and report the failed-shard count, total, and reason from the first failing page as a single consistent summary. The command SHALL NOT accumulate failed-shard counts across pages, since a shard that fails on every page would be counted repeatedly and the reported count could exceed the total.

#### Scenario: single search reports partial shard failure

- **WHEN** a bounded hit search returns HTTP 200 with `_shards.failed` greater than zero
- **THEN** the returned hits are written to stdout, a diagnostic naming the failed-shard count and reason is written to stderr, and the exit code is 5

#### Scenario: paginated search reports partial shard failure on any page

- **WHEN** an unbounded `search_after` search returns HTTP 200 on every page but at least one page reports `_shards.failed` greater than zero
- **THEN** all collected hits are written to stdout, a diagnostic carrying the first failing page's failed-shard count, total, and reason is written to stderr, and the exit code is 5

#### Scenario: fully successful search is unchanged

- **WHEN** a hit search returns HTTP 200 with `_shards.failed` equal to zero
- **THEN** the hits are written to stdout and the exit code is 0 with no shard-failure diagnostic
