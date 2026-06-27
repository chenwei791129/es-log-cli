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

---
### Requirement: Scaffold a config template to stdout

The `es-log config init` command SHALL print a commented configuration template to stdout and exit 0. The command SHALL NOT write, create, or modify any file, SHALL NOT create directories, and SHALL NOT read any existing configuration file — it is purely a generator that emits text. The printed template SHALL be valid YAML that loads into the configuration schema with its modeled fields populated (not silently dropped), SHALL contain at least one `apikey` context and one `basic` context, and SHALL demonstrate `${ENV_VAR}` references for secret fields. Because `config init` writes nothing, it does not contradict the existing requirement that the CLI SHALL NOT write the configuration.

#### Scenario: Template printed to stdout

- **WHEN** the user runs `es-log config init`
- **THEN** the CLI prints a YAML configuration template to stdout and exits 0
- **AND** no file is created or modified on disk
- **AND** the command does not read the resolved config path (it succeeds even when no config file exists)

#### Scenario: Template loads with populated fields

- **WHEN** the template printed by `es-log config init` is loaded through the config loader
- **THEN** the document loads without error
- **AND** the loaded config contains a context with `auth.type` equal to `apikey` whose `api-key` field holds a `${...}` reference
- **AND** the loaded config contains a context with `auth.type` equal to `basic` whose `password` field holds a `${...}` reference

##### Example: field-level round-trip

- **GIVEN** the emitted template includes `auth: { type: apikey, api-key: ${ES_API_KEY} }`
- **WHEN** it is loaded via the config loader
- **THEN** the loaded context's `Auth.Type` is `apikey` and `Auth.APIKey` is the literal string `${ES_API_KEY}` (proving the `api-key` key was modeled, not dropped by a typo)

#### Scenario: No configuration-writing flag is registered

- **WHEN** the `config init` command is constructed
- **THEN** it registers no `--write`, `--force`, or any other flag that would write the configuration to disk
- **AND** invoking `es-log config init` therefore has no code path that creates or modifies a file

---
### Requirement: Config help documents the file format

The `es-log config --help` output SHALL document where the configuration file is read from — the default path `~/.config/es-log/config.yaml`, the `$XDG_CONFIG_HOME/es-log/config.yaml` form, and the `--config` / `$ES_LOG_CONFIG` overrides — and SHALL include a minimal YAML example showing the `contexts` list with both `apikey` and `basic` auth. This makes the configuration format discoverable from the CLI's own help, without requiring the reader to consult the README or source.

#### Scenario: Help shows path and example

- **WHEN** the user runs `es-log config --help`
- **THEN** the output names the default path `~/.config/es-log/config.yaml` and the `$ES_LOG_CONFIG` override
- **AND** the output includes a YAML snippet containing a `contexts:` list with at least one context
