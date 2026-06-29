## MODIFIED Requirements

### Requirement: Read-only endpoint whitelist

The Elasticsearch client SHALL expose ONLY the following methods, each bound to a fixed HTTP verb and path template. The client SHALL NOT expose any generic request method that accepts an arbitrary HTTP verb or path.

| Method | HTTP verb | Path template |
| ------ | --------- | ------------- |
| ListAliases | GET | `/_alias` |
| ListDataStreams | GET | `/_data_stream` |
| Search | POST | `/<target>/_search` |
| GetMaxResultWindow | GET | `/<target>/_settings/index.max_result_window?include_defaults=true&flat_settings=true` |
| GetMapping | GET | `/<target>/_mapping` |
| Ping | GET | `/_cluster/health` |

The client SHALL NOT issue any request using a write or maintenance verb (`PUT`, `DELETE`, `PATCH`) or any path outside the templates above. Pagination for unbounded search SHALL be implemented using the `search_after` field of the `_search` request body, and SHALL NOT introduce a Point-in-Time (`_pit`) endpoint or any `DELETE` request.

#### Scenario: Only whitelisted requests are issued

- **WHEN** any sequence of client operations runs against a request-recording server
- **THEN** every captured request matches one of the six whitelisted verb/path templates
- **AND** no captured request uses `PUT`, `DELETE`, or `PATCH`

#### Scenario: No generic request method exists

- **WHEN** the client's exported method set is enumerated
- **THEN** there is no exported method that accepts a caller-supplied HTTP verb or arbitrary path

##### Example: exported method set

- **GIVEN** the esclient package
- **WHEN** its exported methods are listed
- **THEN** the set is exactly `{ListAliases, ListDataStreams, Search, GetMaxResultWindow, GetMapping, Ping}`
- **AND** there is no `Do`, `Request`, `Raw`, or similar method taking a verb/path argument

## ADDED Requirements

### Requirement: Mapping retrieval and cross-index type aggregation

The `GetMapping` method SHALL issue `GET /<target>/_mapping` and return the target's fields as a flattened, cross-index-aggregated list so the command layer need not parse the raw mapping or reach into per-index shapes. Each returned field SHALL carry its dotted path name, the distinct set of types observed across all resolved indices ordered ascending, and a per-index type breakdown that is populated ONLY when the distinct type set has size two or more (a type conflict). The returned list SHALL be ordered by field path. Flattening SHALL produce one entry per leaf field and per multi-field, SHALL recurse into objects using a dotted prefix without emitting the intermediate object node, and SHALL type a node carrying neither an explicit type nor sub-properties as `object`. This method introduces no write or maintenance verb and no generic request capability.

#### Scenario: Flattened fields returned for a single index

- **WHEN** `GetMapping` is called for a single-index target whose mapping has `user.id` of type `keyword` and `message` of type `text`
- **THEN** the returned list contains a `user.id` field with type set `["keyword"]` and a `message` field with type set `["text"]`, ordered by path
- **AND** neither field carries a per-index breakdown

#### Scenario: Divergent types across indices aggregated as a conflict

- **WHEN** `GetMapping` is called for a target resolving to two indices where `tags` is `text` in one and `keyword` in the other
- **THEN** the `tags` field's type set is `["keyword", "text"]`
- **AND** its per-index breakdown maps each index name to its observed type
