## ADDED Requirements

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
