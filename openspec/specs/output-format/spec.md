# output-format Specification

## Purpose

TBD - created by archiving change 'bootstrap-es-log-cli'. Update Purpose after archive.

## Requirements

### Requirement: Output format selection and defaults

The `--output/-o` flag SHALL accept `jsonl`, `json`, or `table`. The `search` command SHALL default to `jsonl`; all other commands SHALL default to `json`. The `table` format SHALL produce human-readable aligned columns whose columns mirror the fields of that command's `json` shape (e.g. `ls` renders `name`/`type`/count; `search` renders a fixed set including `_id`/`_index`/the timestamp field). All commands SHALL share one aligned-column renderer rather than bespoke per-command table layouts.

#### Scenario: Search defaults to jsonl

- **WHEN** the user runs `es-log -c prod search -t app-logs` with no `--output`
- **THEN** the output is in JSONL format

#### Scenario: Non-search commands default to json

- **WHEN** the user runs `es-log -c prod ls` with no `--output`
- **THEN** the output is a single JSON document

---
### Requirement: JSON document shapes for non-search commands

The `json` output of non-search commands SHALL follow fixed, agent-parseable shapes. `ls` (and `ls aliases` / `ls datastreams`) SHALL emit a JSON array of row objects (`[{"name": ..., "type": ..., ...}]`), directly iterable with `jq '.[]'`. `config get-contexts` SHALL emit a JSON array of context-name strings. `config view` SHALL emit a single JSON object representing the resolved configuration with secrets redacted.

#### Scenario: ls emits a JSON array

- **WHEN** the user runs `es-log -c prod ls -o json` with two targets
- **THEN** the output is a JSON array of two row objects, each carrying at least `name` and `type`

#### Scenario: get-contexts emits a string array

- **WHEN** the config defines contexts `prod` and `staging` and the user runs `es-log config get-contexts -o json`
- **THEN** the output is a JSON array of the strings `prod` and `staging`

---
### Requirement: Search JSONL output is bare source

In `jsonl` format, the `search` command SHALL emit one record per line, where each line is the JSON object of the hit's `_source` with no Elasticsearch envelope fields (`_id`, `_index`, `_score`). When `--fields` is set, each line SHALL contain only the projected `_source` fields. Each line SHALL be a valid standalone JSON object.

#### Scenario: Each line is bare source

- **WHEN** a search returns hits and the output format is `jsonl`
- **THEN** each output line is a valid JSON object equal to that hit's `_source`
- **AND** no line contains `_id`, `_index`, or `_score`

---
### Requirement: Search JSON output carries metadata

In `json` format, the `search` command SHALL emit a single JSON document of the shape `{"total": N, "hits": [...]}`, where each element of `hits` includes `_id`, `_index`, `_score`, and `_source`.

#### Scenario: JSON document includes metadata and total

- **WHEN** a search matching 7 documents returns hits and the output format is `json`
- **THEN** the output is a single JSON document with `total` equal to 7 and a `hits` array whose elements carry `_id`, `_index`, `_score`, and `_source`

---
### Requirement: Quiet mode keeps stdout clean

The `--quiet` flag SHALL suppress all non-result noise (connection info, progress, prompts, warnings) so that stdout contains only result data. Suppressed noise that is still emitted SHALL go to stderr.

#### Scenario: Quiet suppresses warnings on stdout

- **WHEN** a size-capping warning would otherwise be printed and the user passes `--quiet`
- **THEN** stdout contains only the result records and the warning does not appear on stdout

---
### Requirement: Errors on stderr with layered exit codes

All error messages SHALL be written to stderr as plain text, and stdout SHALL NOT contain partial result output on failure. The process exit code SHALL be layered: `0` on success, `2` for argument or configuration errors (including a missing context and mutually-exclusive time flags), `3` for connection or authentication failures, and `4` when the target does not exist (Elasticsearch 404 index_not_found, surfaced as a friendly message naming the target).

#### Scenario: Connection failure

- **WHEN** the target server is unreachable during a search
- **THEN** an error is printed to stderr, stdout is empty, and the exit code is 3

#### Scenario: Target not found

- **WHEN** a search targets a non-existent index and Elasticsearch returns 404 index_not_found
- **THEN** a friendly error naming the target is printed to stderr and the exit code is 4

##### Example: exit code mapping

| Condition | Exit code |
| --------- | --------- |
| Successful command | 0 |
| Missing context / mutually-exclusive time flags / bad arguments | 2 |
| Connection or authentication failure | 3 |
| Target index/datastream not found (404) | 4 |

---
### Requirement: Config init emits a fixed template regardless of output format

The `es-log config init` command SHALL always emit its YAML template as fixed text and SHALL NOT vary that output based on the value of the `-o/--output` flag. Unlike `config view` or `config get-contexts`, `config init` does not serialize a data structure, so `jsonl`, `json`, and `table` produce identical template output. To keep the argument-validation contract uniform with every other command, `config init` SHALL still reject an invalid `-o` value with exit code 2 (the same error path used by `config view`, `ls`, and `search`); it SHALL NOT silently accept an unrecognized format. A valid `-o` value SHALL be accepted and ignored.

#### Scenario: Valid output flag does not change the template

- **WHEN** the user runs `es-log config init -o json` and `es-log config init` with no flag
- **THEN** both print the identical YAML template and exit 0

#### Scenario: Invalid output flag is rejected consistently

- **WHEN** the user runs `es-log config init -o bogus`
- **THEN** the CLI prints an invalid-output-format error to stderr and exits with code 2
- **AND** the behavior matches `es-log config view -o bogus` (no divergence between subcommands for the same invalid value)
