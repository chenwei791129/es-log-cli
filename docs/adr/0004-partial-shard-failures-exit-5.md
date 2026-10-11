---
status: accepted
---

# Partial shard failures emit partial results and exit 5

When Elasticsearch answers a search with `200 OK` but reports failed shards,
the result is incomplete. Instead of failing hard (discarding data that did
arrive) or succeeding silently (hiding the gap), the CLI writes the returned
hits or buckets to stdout, prints a diagnostic naming the failed-shard count and
reason to stderr, and exits with code 5. Callers get the usable data together
with an unambiguous machine-readable signal that it is incomplete, so a script
must check the exit code before trusting the output. This is the only exit code
for which stdout is not empty on failure, which is why it is kept distinct from
the hard-failure codes 2, 3, and 4.
