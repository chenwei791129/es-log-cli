# es-log-cli

A read-only Elasticsearch log query CLI built for AI agents first, humans and CI
second. This glossary covers the vocabulary shared by the CLI, its
configuration, and its compatibility policy toward Elasticsearch releases.

## Language

### Connection and targets

**Context**:
A named cluster connection configuration (server URL, authentication, TLS).
Every command is explicitly bound to one context.
_Avoid_: profile, cluster, environment

**Target**:
The index, alias, or data stream name that a query or inspection is addressed
to.
_Avoid_: index (when the thing may also be an alias or a data stream)

**Hidden target**:
A target whose name begins with a dot; listings omit it unless explicitly asked
for.
_Avoid_: system index, dot index

**Type conflict**:
A field path whose backing indices under one target disagree on its mapping
type.
_Avoid_: mapping conflict, schema drift

### Results

**Hard failure**:
A command outcome (exit code 2, 3, or 4) in which nothing is written to stdout.
_Avoid_: fatal, error exit

**Partial shard failure**:
A search that Elasticsearch answered with `200 OK` while reporting failed
shards; the returned data is still emitted and exit code 5 marks it incomplete.
_Avoid_: incomplete results, partial failure

### Aggregation

**Structured aggregation**:
An aggregation expressed through the CLI's dedicated terms, date-histogram, and
metric options.
_Avoid_: structured mode, flag mode

**Raw aggregation**:
An aggregation expressed as an Elasticsearch `aggs` JSON object that the CLI
passes through unchanged.
_Avoid_: passthrough, `--aggs` mode

### Compatibility

**Server version**:
The Elasticsearch version reported by the single node that answered the root
info request. It is a node-level fact: during a rolling upgrade, different nodes
may report different versions.
_Avoid_: cluster version

**Version gate**:
A code path selected by the major component of the server version. When nodes
disagree, a gate follows the path that is also valid for the older major.
_Avoid_: compat shim, compatibility branch

**Support matrix**:
The set of Elasticsearch majors continuously verified by CI against a real
cluster. An Elasticsearch major is supported if and only if it is in the matrix.
_Avoid_: compatibility list, supported versions

**Deprecation warning**:
A deprecation notice that Elasticsearch returns in the `Warning` response header
of an individual request.
_Avoid_: ES warning, header warning
