## ADDED Requirements

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

### Requirement: Config help documents the file format

The `es-log config --help` output SHALL document where the configuration file is read from — the default path `~/.config/es-log/config.yaml`, the `$XDG_CONFIG_HOME/es-log/config.yaml` form, and the `--config` / `$ES_LOG_CONFIG` overrides — and SHALL include a minimal YAML example showing the `contexts` list with both `apikey` and `basic` auth. This makes the configuration format discoverable from the CLI's own help, without requiring the reader to consult the README or source.

#### Scenario: Help shows path and example

- **WHEN** the user runs `es-log config --help`
- **THEN** the output names the default path `~/.config/es-log/config.yaml` and the `$ES_LOG_CONFIG` override
- **AND** the output includes a YAML snippet containing a `contexts:` list with at least one context
