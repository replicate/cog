---
# cog-xv45
title: cog weights check/list/inspect commands
status: scrapped
type: task
priority: normal
created_at: 2026-04-17T19:27:28Z
updated_at: 2026-04-17T21:32:00Z
parent: cog-9qcd
blocked_by:
    - cog-2gv9
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



## Partial progress (4fg4 + simplification pass, 2026-04-17)

`cog weights inspect` was reshaped for v1 as part of 4fg4 and parallelized as part of the simplification pass:

- [x] Reads `WeightLockEntry` / `WeightLockLayer` from the v1 lockfile
- [x] Shows per-layer digests + sizes under each weight
- [x] `--remote` lookup fans out with bounded concurrency (one goroutine per weight, capped at `GetPushConcurrency`)
- [x] JSON output (`WeightsInspectOutput`) uses a unified `WeightInspectLayer` type (not split into local/remote)
- [x] Status values: `synced`, `local-only`, `remote-only`, `digest-mismatch`, `missing-lockfile`

`inspect` functional scope is essentially done for v1. Outstanding bean scope:

- [ ] `cog weights list` — trivial table from lockfile (`--remote` variant queries registry)
- [ ] `cog weights check` — HEAD per manifest digest, exit 0 / non-zero
- [ ] `cog weights check --source` — blocked on s5fy fingerprinting
- [ ] `cog weights inspect --remote` flag (currently `inspect` always tries remote; decide whether to gate it)

Bean should be split. See child beans for `list` and `check`.



## Reasons for Scrapping

The three subcommands have been split into separate beans with clear scope:

- **inspect** — absorbed by 4fg4 (done).
- **list** — new bean `cog-6b5a`.
- **check** — new bean `cog-wej9`. `--source` mode deferred to s5fy.

Keeping this bean open would double-book work. Close it.
