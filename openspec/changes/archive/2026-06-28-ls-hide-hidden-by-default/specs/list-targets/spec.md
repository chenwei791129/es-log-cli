## ADDED Requirements

### Requirement: Hide dot-prefixed targets by default

The `es-log ls` command and its `aliases` and `datastreams` subcommands SHALL, by default, omit every target whose name begins with a dot (`.`). The command SHALL accept a boolean `--show-hidden` flag, defaulting to `false`; when `--show-hidden` is set, dot-prefixed targets SHALL be included in the output. The `--show-hidden` flag SHALL be registered on the `ls` command such that the `aliases` and `datastreams` subcommands inherit it, and the same dot-prefix rule SHALL apply uniformly to aliases and datastreams. The filtering SHALL be applied before rendering so that the `table`, `json`, and `jsonl` output formats list an identical set of targets for the same flag value. The hidden criterion SHALL be the literal name prefix `.` only; it SHALL NOT depend on the Elasticsearch `index.hidden` setting. This filtering SHALL apply only to the `ls` command tree and SHALL NOT change the targets the `search` command queries.

#### Scenario: Dot-prefixed targets hidden by default

- **WHEN** the cluster has aliases `.kibana`, `app-logs` and datastreams `.items-default`, `metrics`
- **THEN** `es-log -c prod ls` outputs only `app-logs` (typed `alias`) and `metrics` (typed `datastream`)
- **AND** neither `.kibana` nor `.items-default` appears in the output

#### Scenario: --show-hidden includes dot-prefixed targets

- **WHEN** the cluster has aliases `.kibana`, `app-logs` and datastreams `.items-default`, `metrics`
- **THEN** `es-log -c prod ls --show-hidden` outputs all four targets, including `.kibana` and `.items-default`

#### Scenario: Subcommands inherit the flag and the default

- **WHEN** the user runs `es-log -c prod ls datastreams` against datastreams `.items-default`, `metrics`
- **THEN** the output contains only `metrics`
- **AND** running `es-log -c prod ls datastreams --show-hidden` additionally includes `.items-default`

#### Scenario: All-hidden listing yields an empty result, not an error

- **WHEN** every alias in the cluster is dot-prefixed and the user runs `es-log -c prod ls aliases`
- **THEN** the command exits 0 with an empty list of rows
- **AND** running `es-log -c prod ls aliases --show-hidden` lists those dot-prefixed aliases
