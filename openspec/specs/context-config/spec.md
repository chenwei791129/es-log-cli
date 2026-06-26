# context-config Specification

## Purpose

TBD - created by archiving change 'bootstrap-es-log-cli'. Update Purpose after archive.

## Requirements

### Requirement: Config file location and override

The CLI SHALL read its configuration from `~/.config/es-log/config.yaml` by default, following XDG conventions (`$XDG_CONFIG_HOME/es-log/config.yaml` when `$XDG_CONFIG_HOME` is set). The path SHALL be overridable by the `--config <path>` flag and by the `$ES_LOG_CONFIG` environment variable, with `--config` taking precedence over the environment variable.

#### Scenario: Default config path

- **WHEN** the user runs a command without `--config` and `$ES_LOG_CONFIG` is unset and `$XDG_CONFIG_HOME` is unset
- **THEN** the CLI reads configuration from `~/.config/es-log/config.yaml`

#### Scenario: Path override precedence

- **WHEN** both `--config /a/cfg.yaml` is given and `$ES_LOG_CONFIG=/b/cfg.yaml` is set
- **THEN** the CLI reads configuration from `/a/cfg.yaml`

---
### Requirement: Context selection without hidden default

The configuration file SHALL NOT contain a `current-context` field, and the CLI SHALL NOT provide any command that writes the configuration (no `set-context`, `use-context`, `delete-context`, or `current-context`). For commands that contact Elasticsearch (`ls`, `search`, `ping`), the active context SHALL be resolved from `--context/-c` first, then from the `$ES_LOG_CONTEXT` environment variable. When neither source supplies a context, the CLI SHALL print an error naming the missing context to stderr, list the available context names, and exit with code 2.

#### Scenario: Flag takes precedence over environment

- **WHEN** the user runs `es-log -c prod ls` while `$ES_LOG_CONTEXT=staging` is set
- **THEN** the CLI uses the `prod` context

#### Scenario: Environment fallback

- **WHEN** the user runs `es-log ls` with no `-c` flag and `$ES_LOG_CONTEXT=staging` is set
- **THEN** the CLI uses the `staging` context

#### Scenario: Missing context on an Elasticsearch command

- **WHEN** the user runs `es-log search -t app-logs` with no `-c` flag and `$ES_LOG_CONTEXT` unset
- **THEN** the CLI prints an error to stderr listing the available context names and exits with code 2
- **AND** stdout produces no output

---
### Requirement: Environment variable expansion for secrets

When a context's secret value is actually used (i.e. when establishing an Elasticsearch connection for `ls`, `search`, or `ping`), the CLI SHALL expand `${ENV_VAR}` references found in string values (including `api-key`, `username`, and `password`) using the process environment. A `${ENV_VAR}` reference whose variable is unset SHALL be treated as an error ONLY for the context being used: the CLI SHALL print an error to stderr and exit with code 2. The `config get-contexts` and `config view` commands SHALL NOT fail when an unrelated (or even the inspected) context references an unset variable — `get-contexts` only lists names, and `config view` redacts secrets to `***` without needing their resolved values.

#### Scenario: Expand a defined variable when connecting

- **WHEN** the active context has `api-key: ${ES_PROD_API_KEY}`, the environment defines `ES_PROD_API_KEY=abc123`, and the user runs an Elasticsearch command
- **THEN** the connection uses the API key value `abc123`

#### Scenario: Undefined variable for the used context is an error

- **WHEN** the active context has `password: ${MISSING_VAR}`, `MISSING_VAR` is unset, and the user runs an Elasticsearch command
- **THEN** the CLI prints an error to stderr and exits with code 2

#### Scenario: get-contexts succeeds despite an unset variable in another context

- **WHEN** the config defines contexts `prod` and `staging`, `staging` references unset `${MISSING_VAR}`, and the user runs `es-log config get-contexts`
- **THEN** the CLI lists both `prod` and `staging` and exits 0 without expanding `${MISSING_VAR}`

---
### Requirement: List contexts

The `es-log config get-contexts` command SHALL list the names of all contexts defined in the configuration file. This command SHALL NOT require a context to be selected.

#### Scenario: List configured contexts

- **WHEN** the config defines contexts `prod` and `staging` and the user runs `es-log config get-contexts`
- **THEN** the CLI outputs both names `prod` and `staging`

---
### Requirement: View resolved configuration with secret redaction

The `es-log config view` command SHALL print the resolved configuration. Every secret value (`api-key`, `password`) SHALL be redacted to `***` in the output. The command SHALL NOT print Authorization headers or any unredacted secret.

#### Scenario: Secrets are redacted

- **WHEN** a context resolves to `api-key: abc123` and the user runs `es-log config view`
- **THEN** the output shows `api-key: ***` and never shows `abc123`

##### Example: redaction across auth types

| Auth field | Stored/resolved value | Output in `config view` |
| ---------- | --------------------- | ----------------------- |
| api-key    | abc123                | ***                     |
| password   | s3cret                | ***                     |
| username   | elastic               | elastic                 |
| server     | https://es:9200       | https://es:9200         |
