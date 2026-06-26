## ADDED Requirements

### Requirement: List aliases and datastreams in a combined view

The `es-log ls` command SHALL list both index aliases and datastreams in a single combined view. Each row SHALL include a `name` field and a `type` field whose value is `alias` or `datastream`. The command SHALL require an active context.

#### Scenario: Combined listing

- **WHEN** the cluster has aliases `app-logs`, `web-logs` and datastream `metrics`
- **THEN** `es-log -c prod ls` outputs three rows, with `app-logs` and `web-logs` typed `alias` and `metrics` typed `datastream`

### Requirement: Filter listing by target type

The `es-log ls aliases` subcommand SHALL list only aliases, and the `es-log ls datastreams` subcommand SHALL list only datastreams.

#### Scenario: Aliases only

- **WHEN** the user runs `es-log -c prod ls aliases`
- **THEN** the output contains only rows typed `alias`

#### Scenario: Datastreams only

- **WHEN** the user runs `es-log -c prod ls datastreams`
- **THEN** the output contains only rows typed `datastream`

### Requirement: Type-specific count fields

Datastream rows SHALL include a `backing_indices_count` field reporting the number of backing indices. Alias rows SHALL include an `index_count` field reporting the number of indices the alias points to.

#### Scenario: Counts present per type

- **WHEN** datastream `metrics` has 3 backing indices and alias `app-logs` points to 2 indices
- **THEN** the `metrics` row reports `backing_indices_count: 3` and the `app-logs` row reports `index_count: 2`
