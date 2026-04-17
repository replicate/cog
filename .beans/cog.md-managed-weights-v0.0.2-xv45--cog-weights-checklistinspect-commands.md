---
# cog.md-managed-weights-v0.0.2-xv45
title: cog weights check/list/inspect commands
status: todo
type: task
priority: normal
created_at: 2026-04-17T19:27:28Z
updated_at: 2026-04-17T19:33:22Z
parent: cog.md-managed-weights-v0.0.2-9qcd
blocked_by:
    - cog.md-managed-weights-v0.0.2-2gv9
---

Implement the weight discovery and validation commands.

## Scope and internal ordering

These are three commands with different complexity. Implement in this order:
1. list (trivial: read lockfile, print table)
2. inspect (moderate: format layer details, optional --remote registry query)
3. check (moderate without --source: HEAD request per manifest digest vs lockfile)

check --source (compare source fingerprints) depends on the source fingerprinting task (s5fy) and only works for sources that have resolvers. Defer --source until source fingerprinting lands.

cog weights check [name]:
- Validate lockfile digests match registry (HEAD request per manifest)
- --source flag: also check if source fingerprint has changed since last import
- Exit code 0 = ok, non-zero = mismatch
- For CI pipelines

cog weights list:
- Default: read from cog.yaml + weights.lock, show name/size/layers/imported date
- --remote: query registry for what's actually stored
- Table output

cog weights inspect [name]:
- Detailed view: layers, sizes, digests, source provenance, compression
- --json for machine-readable output
- --remote to inspect registry state

Existing v0: pkg/cli/weights.go has a hidden weights command with build/push/inspect subcommands. Refactor into the new command structure and unhide.

Reference: plans/2026-04-16-managed-weights-v2-design.md §3
