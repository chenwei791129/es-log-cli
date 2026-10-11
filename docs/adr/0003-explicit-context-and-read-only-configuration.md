---
status: accepted
---

# Explicit context selection; the CLI never writes its configuration

The obvious design for a multi-cluster CLI is kubectl-style: a
`current-context` stored in the configuration file plus `use-context` /
`set-context` commands to change it. We rejected that. Every invocation names
its context explicitly (`--context` or `$ES_LOG_CONTEXT`), and the CLI has no
command that creates or modifies the configuration file; `config init` only
prints a template to stdout. Hidden state would make agent invocations
non-reproducible across sessions and machines, and a config-writing command
would be a mutation surface in a tool whose value is that it cannot change
anything.
