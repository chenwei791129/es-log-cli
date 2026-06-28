# es-log

A **read-only**, agent-friendly Elasticsearch log query CLI. `es-log` is a single
static binary that, by construction, can only issue a fixed whitelist of read
endpoints — it is impossible to trigger destructive operations (`DELETE`,
`_rollover`, `_reindex`, ILM, etc.) through it. Output defaults to JSONL for easy
`jq`/pipe consumption, with layered exit codes for reliable scripting.

Designed for AI agents first, humans and CI second. Supports Elasticsearch 8.x+.

## Features

- **Read-only by design** — the client exposes only `ListAliases`,
  `ListDataStreams`, `Search`, `GetMaxResultWindow`, and `Ping`; there is no
  generic request method that accepts an arbitrary verb or path.
- **Explicit context** — every query is bound to a context via `--context/-c` or
  `$ES_LOG_CONTEXT`. No hidden `current-context` state.
- **Agent-friendly output** — `search` emits bare `_source` JSONL by default;
  `json` carries `total` and per-hit metadata.
- **Two-layer filtering** — Lucene `--query` narrows on the ES side; repeatable
  `--include`/`--exclude` regexes filter on the client side.
- **Unbounded fetch** — `--size 0` pages through all matches with `search_after`,
  automatically respecting `max_result_window`.
- **Secret hygiene** — `${ENV_VAR}` expansion, `config view` redaction, no
  Authorization headers in output.

## Installation

Download the pre-built binary for your platform from the
[latest release](https://github.com/chenwei791129/es-log-cli/releases/latest).
No Go toolchain required — each binary is a CGO-free static executable.

```bash
# Pick the asset matching your OS/arch:
#   es-log-linux-amd64   es-log-linux-arm64
#   es-log-darwin-amd64  es-log-darwin-arm64

# Example: Linux x86_64
curl -L -o es-log \
  https://github.com/chenwei791129/es-log-cli/releases/latest/download/es-log-linux-amd64
chmod +x es-log
sudo mv es-log /usr/local/bin/   # put it on your PATH

# macOS Apple Silicon
curl -L -o es-log \
  https://github.com/chenwei791129/es-log-cli/releases/latest/download/es-log-darwin-arm64
chmod +x es-log
sudo mv es-log /usr/local/bin/
```

Verify the install with `es-log version`.

Building from source instead is covered under [Development](#development).

## Configuration

`es-log` reads `~/.config/es-log/config.yaml` by default (honoring
`$XDG_CONFIG_HOME`). Override with `--config <path>` or `$ES_LOG_CONFIG`.

The file is a flat list of contexts (no `current-context`). Secrets may reference
environment variables with `${VAR}`, expanded only when a connection is made.
Recommended file mode: `0600`.

Run `es-log config init` to print the canonical, commented template — the
authoritative source for the file format — and redirect it into place, e.g.
`es-log config init > ~/.config/es-log/config.yaml`. The snippet below is a
condensed, human-readable version of that template.

```yaml
contexts:
  - name: prod
    server: https://es-prod.example.com:9200
    auth:
      type: apikey            # apikey | basic
      api-key: ${ES_PROD_API_KEY}
    tls:
      ca-cert: /etc/es/ca.pem
      insecure-skip-verify: false

  - name: staging
    server: https://es-staging.example.com:9200
    auth:
      type: basic
      username: elastic
      password: ${ES_STAGING_PASSWORD}
```

## Usage

```bash
# List contexts and inspect configuration (secrets redacted)
es-log config get-contexts
es-log config view

# Check connectivity / authentication
es-log -c prod ping

# List targets (aliases + datastreams)
es-log -c prod ls
es-log -c prod ls datastreams -o table

# Search: errors in the last hour (default JSONL of bare _source)
es-log -c prod search -t app-logs -q 'level:error' --since 1h

# Field projection + client-side regex filtering
es-log -c prod search -t app-logs \
  -q 'service:checkout' \
  --fields '@timestamp,level,message' \
  --include 'timeout|deadline' --exclude 'healthcheck' \
  --since 24h

# Fetch everything matching (paged with search_after)
es-log -c prod search -t app-logs -q 'status:500' --size 0 > errors.jsonl

# Full result document with metadata
es-log -c prod search -t app-logs -q 'level:error' -o json | jq '.total'
```

### Global flags

| Flag | Description |
| ---- | ----------- |
| `-c, --context` | Context to use (or `$ES_LOG_CONTEXT`). Required for ES commands. |
| `-o, --output` | `jsonl` \| `json` \| `table`. `search` defaults to `jsonl`, others to `json`. |
| `--quiet` | Suppress warnings/progress; keep stdout clean. |
| `--config` | Config file path (or `$ES_LOG_CONFIG`). |

### Output formats

- **jsonl** (search default): one bare `_source` object per line — no `_id`,
  `_index`, or `_score` envelope. Ideal for `jq -c`, `grep`, `head`, streaming.
- **json**: search → `{"total":N,"hits":[{_id,_index,_score,_source}]}`; `ls` → an
  array of row objects; `config get-contexts` → an array of name strings;
  `config view` → the resolved config object.
- **table**: human-readable aligned columns.

`--include`/`--exclude` always match against the serialized `_source` JSON,
independent of `-o`, so the same patterns yield the same filtered set in every
format.

### Exit codes

| Code | Meaning |
| ---- | ------- |
| 0 | Success |
| 2 | Argument/config error (missing context, conflicting time flags, missing target, unset secret variable) |
| 3 | Connection or authentication failure |
| 4 | Target not found (ES 404 `index_not_found`) |

Errors go to stderr as plain text; stdout never contains partial output on failure.

## Development

Requires Go 1.25+. To build from source instead of downloading a release binary:

```bash
make build    # produces a CGO-free static ./es-log binary
make test     # run the test suite
make lint     # golangci-lint (pinned as a go tool dependency)
make fmt      # gofmt
```

## License

Released under the MIT License — see [LICENSE](LICENSE) for the full text.
