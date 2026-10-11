---
status: accepted
---

# Single codebase with runtime version gates for Elasticsearch compatibility

es-log-cli talks to Elasticsearch through a small hand-written `net/http`
client that uses only a handful of core read endpoints (search, mapping,
aliases, data streams, index settings, root info). We keep one codebase for all
supported Elasticsearch majors: the client detects the server version at
runtime and, only where an actual incompatibility has been observed, selects
behaviour through a version gate. We do not adopt `go-elasticsearch` and we do
not maintain per-major branches.

## Considered Options

- **Per-major branches (`v8`, `v9`, ...)** — rejected. The branches would differ
  by almost nothing, while every feature and bug fix would have to be ported N
  times.
- **`go-elasticsearch` SDK** — rejected. Its major version is pinned to the
  Elasticsearch major, so it adds a mandatory bump per Elasticsearch release
  rather than removing one. The low-level `esapi` still leaves request bodies
  and response parsing to us, which are the only parts that realistically
  break; the typed API regenerates its types per major; and its product check
  refuses non-Elasticsearch backends that the raw client tolerates.
- **Single codebase with runtime version gates** — chosen. The API surface used
  here has been stable across majors, so compatibility is verified empirically
  by the CI support matrix and handled locally in the client when a difference
  appears.

## Consequences

- Compatibility claims are backed by the CI support matrix, not by a dependency
  version. A new major is supported by adding it to the matrix and fixing
  whatever turns red.
- Deprecation warnings returned by Elasticsearch are surfaced so that the next
  major's removals become visible one release ahead.
