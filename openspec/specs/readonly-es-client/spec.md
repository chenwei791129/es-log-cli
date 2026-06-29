# readonly-es-client Specification

## Purpose

TBD - created by archiving change 'bootstrap-es-log-cli'. Update Purpose after archive.

## Requirements

### Requirement: Read-only endpoint whitelist

The Elasticsearch client SHALL expose ONLY the following methods, each bound to a fixed HTTP verb and path template. The client SHALL NOT expose any generic request method that accepts an arbitrary HTTP verb or path.

| Method | HTTP verb | Path template |
| ------ | --------- | ------------- |
| ListAliases | GET | `/_alias` |
| ListDataStreams | GET | `/_data_stream` |
| Search | POST | `/<target>/_search` |
| GetMaxResultWindow | GET | `/<target>/_settings/index.max_result_window?include_defaults=true&flat_settings=true` |
| Ping | GET | `/_cluster/health` |

The client SHALL NOT issue any request using a write or maintenance verb (`PUT`, `DELETE`, `PATCH`) or any path outside the templates above. Pagination for unbounded search SHALL be implemented using the `search_after` field of the `_search` request body, and SHALL NOT introduce a Point-in-Time (`_pit`) endpoint or any `DELETE` request.

#### Scenario: Only whitelisted requests are issued

- **WHEN** any sequence of client operations runs against a request-recording server
- **THEN** every captured request matches one of the five whitelisted verb/path templates
- **AND** no captured request uses `PUT`, `DELETE`, or `PATCH`

#### Scenario: No generic request method exists

- **WHEN** the client's exported method set is enumerated
- **THEN** there is no exported method that accepts a caller-supplied HTTP verb or arbitrary path

##### Example: exported method set

- **GIVEN** the esclient package
- **WHEN** its exported methods are listed
- **THEN** the set is exactly `{ListAliases, ListDataStreams, Search, GetMaxResultWindow, Ping}`
- **AND** there is no `Do`, `Request`, `Raw`, or similar method taking a verb/path argument

---
### Requirement: Authentication

The client SHALL support exactly two authentication modes selected per context: API Key and Basic Auth. For API Key auth the client SHALL send the configured key in the `Authorization: ApiKey <key>` header. For Basic Auth the client SHALL send the configured username and password using HTTP Basic authentication. The client SHALL NOT support any other authentication mode.

#### Scenario: API Key authentication

- **WHEN** the active context uses `auth.type: apikey` with a resolved key
- **THEN** outgoing requests carry an `Authorization: ApiKey <key>` header

#### Scenario: Basic authentication

- **WHEN** the active context uses `auth.type: basic` with a resolved username and password
- **THEN** outgoing requests carry an HTTP Basic `Authorization` header derived from those credentials

---
### Requirement: TLS configuration

The client SHALL honor per-context TLS settings: a custom CA certificate file (`tls.ca-cert`) and an insecure flag (`tls.insecure-skip-verify`). When a CA certificate is configured, the client SHALL trust certificates signed by it. When `insecure-skip-verify` is true, the client SHALL skip server certificate verification.

#### Scenario: Custom CA certificate

- **WHEN** the active context sets `tls.ca-cert` to a valid CA file and connects to a server presenting a certificate signed by that CA
- **THEN** the connection succeeds without a certificate verification error

#### Scenario: Insecure skip verify

- **WHEN** the active context sets `tls.insecure-skip-verify: true` and connects to a server with a self-signed certificate
- **THEN** the connection succeeds without a certificate verification error

---
### Requirement: Max result window resolution

The `GetMaxResultWindow` method SHALL request `GET /<target>/_settings/index.max_result_window?include_defaults=true&flat_settings=true` and parse the flat-keyed `index.max_result_window` value. When the target is an alias or datastream resolving to multiple backing indices, the response is a per-index settings map; the method SHALL return the MINIMUM `max_result_window` across those indices as a safe lower bound. When the value is absent, empty, or the request fails, the method SHALL surface that so the caller can apply the default of `10000`.

#### Scenario: Flat settings parsed

- **WHEN** the server returns a flat-keyed `index.max_result_window` of 5000 for a single-index target
- **THEN** `GetMaxResultWindow` returns 5000

#### Scenario: Minimum across multiple indices

- **WHEN** an alias resolves to two indices reporting `max_result_window` of 10000 and 5000
- **THEN** `GetMaxResultWindow` returns 5000

---
### Requirement: Connectivity check

The `Ping` method SHALL issue `GET /_cluster/health` and report success only when the server responds successfully with valid authentication. A failed connection or rejected authentication SHALL be reported as an error. The `es-log ping` command SHALL wrap this method: on success it SHALL exit with code 0, and on connection or authentication failure it SHALL print an error to stderr and exit with code 3.

#### Scenario: Healthy cluster

- **WHEN** `Ping` is called against a reachable, authenticated cluster
- **THEN** it returns success

#### Scenario: Authentication rejected

- **WHEN** `Ping` is called and the server rejects authentication
- **THEN** it returns an error

#### Scenario: Ping command exit codes

- **WHEN** the user runs `es-log -c prod ping` against an unreachable or auth-rejecting cluster
- **THEN** an error is printed to stderr and the command exits with code 3

#### Scenario: Ping command success

- **WHEN** the user runs `es-log -c prod ping` against a reachable, authenticated cluster
- **THEN** the command exits with code 0

---
### Requirement: Error response root-cause extraction

The read-only Elasticsearch client SHALL extract diagnostic detail from query responses so the command layer can surface actionable failures. For a non-2xx response, the client SHALL parse the error body and expose both the `error.type` and the deepest available root-cause reason, resolved in order of preference: `error.root_cause[0].reason`, then `error.failed_shards[0].reason.reason`, then `error.reason`. When none of these can be parsed, the exposed reason SHALL be empty so the command layer can fall back to a type-only message. For a 2xx search response, the client SHALL expose the `_shards.total`, `_shards.failed`, and `_shards.failures` values so the command layer can detect and describe partial shard failures. A single reason extractor SHALL serve both the non-2xx error body and the `_shards.failures` element shapes, since both nest the reason identically. No new endpoint, HTTP verb, or generic request method SHALL be introduced by this extraction.

#### Scenario: deepest root-cause reason is preferred

- **WHEN** a non-2xx error body carries a top-level `error.reason` of `all shards failed` and a `failed_shards[0].reason.reason` of `Fielddata is disabled on [some_field]`
- **THEN** the client exposes `Fielddata is disabled on [some_field]` as the root-cause reason together with the `error.type`

#### Scenario: unparseable error body yields empty reason

- **WHEN** a non-2xx response body cannot be parsed into the known error shape
- **THEN** the exposed root-cause reason is empty and the client still exposes the HTTP status

#### Scenario: shard failure counts exposed from 2xx response

- **WHEN** a 2xx search response reports `_shards.total` of 5 and `_shards.failed` of 2
- **THEN** the client exposes the total, the failed count, and the failure entries so the command layer can report `2 of 5 shards failed`

<!-- @trace
source: improve-es-error-diagnosability
updated: 2026-06-29
code:
  - internal/cmd/search.go
  - internal/esclient/client.go
  - internal/cmd/exit.go
  - internal/cmd/aggregation.go
  - README.md
tests:
  - internal/cmd/aggregation_test.go
  - internal/esclient/client_test.go
  - internal/cmd/search_test.go
  - internal/cmd/exit_test.go
-->