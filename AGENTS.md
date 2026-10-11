# es-log-cli — agent notes

Read `CONTEXT.md` first for the project vocabulary. Design decisions live in
`docs/adr/`; read the relevant ADR before changing the client, the config
model, or the exit-code contract.

- Build: `make build` · Unit tests: `make test` · Lint: `make lint`
- Integration tests need a reachable Elasticsearch:
  `ES_LOG_TEST_URL=http://localhost:9200 go test -tags integration ./...`
  CI runs them against the support matrix in `.github/workflows/integration.yml`.
- Commits follow Conventional Commits; release-please cuts releases from `main`.
- The Elasticsearch client is a read-only endpoint whitelist (ADR 0002):
  never add a generic request method.
