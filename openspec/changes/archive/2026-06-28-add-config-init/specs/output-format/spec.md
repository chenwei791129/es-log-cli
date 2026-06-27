## ADDED Requirements

### Requirement: Config init emits a fixed template regardless of output format

The `es-log config init` command SHALL always emit its YAML template as fixed text and SHALL NOT vary that output based on the value of the `-o/--output` flag. Unlike `config view` or `config get-contexts`, `config init` does not serialize a data structure, so `jsonl`, `json`, and `table` produce identical template output. To keep the argument-validation contract uniform with every other command, `config init` SHALL still reject an invalid `-o` value with exit code 2 (the same error path used by `config view`, `ls`, and `search`); it SHALL NOT silently accept an unrecognized format. A valid `-o` value SHALL be accepted and ignored.

#### Scenario: Valid output flag does not change the template

- **WHEN** the user runs `es-log config init -o json` and `es-log config init` with no flag
- **THEN** both print the identical YAML template and exit 0

#### Scenario: Invalid output flag is rejected consistently

- **WHEN** the user runs `es-log config init -o bogus`
- **THEN** the CLI prints an invalid-output-format error to stderr and exits with code 2
- **AND** the behavior matches `es-log config view -o bogus` (no divergence between subcommands for the same invalid value)
