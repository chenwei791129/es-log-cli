---
status: accepted
---

# Read-only endpoint whitelist with no generic request method

es-log-cli is meant to be handed to AI agents, so "read-only" has to be a
property of the binary rather than a convention. The Elasticsearch client
exposes exactly one method per allowed read endpoint, each bound to a fixed
verb and path template, and deliberately offers no generic `Do`/`Request`
method that takes a caller-supplied verb or path. A reflection test locks the
exported method set, so adding an endpoint is a conscious change to the
whitelist and to that test, never a parameter. Unbounded search pages with
`search_after` rather than point-in-time so that no `DELETE` request is ever
needed.
