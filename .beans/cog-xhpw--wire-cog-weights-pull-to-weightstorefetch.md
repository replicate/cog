---
# cog-xhpw
title: Wire cog weights pull to WeightStore.Fetch
status: todo
type: task
priority: high
created_at: 2026-04-22T20:23:52Z
updated_at: 2026-04-22T20:43:46Z
parent: cog-kgd7
blocked_by:
    - cog-gbse
    - cog-3p4a
---

Rewire `cog weights pull` to synthesize runnable weights via `WeightStore.Fetch` instead of being a thin `docker pull` wrapper. The store's `Fetch` priority chain handles cache-hit / source-reconstruct / registry-fallback transparently.

## Why

`cog weights pull` after `cog weights import` should be a no-op (cache warm from the import). `cog predict` after `git clone` on a file:// weight should reconstruct from source, never touching the registry. Only cold-cache + no-source should fall back to the registry.

The store already implements this priority order via `Fetch`; this bean wires the CLI to delegate.

## Scope

### Flow

1. Load `cog.yaml` + `weights.lock` + (optionally) pending state.
2. For each weight in the lockfile (or the names passed as args):
   - Build `LayerRef`s from the lockfile entry.
   - Build `FetchMeans`:
     - `Source` + `URI` from the lockfile entry's source block (nil if URI can't be resolved, e.g. relative path from a different checkout).
     - `Registry` + `Repo` from config.
   - Call `store.Fetch(ctx, setDigest, layers, means)`.
3. Progress UX delegated to the store (or a callback passed in via `FetchMeans`).

### CLI

- `cog weights pull [name...]` — pulls specified weights or all.
- `--from-registry` flag (optional): skip source reconstruction, always pull from registry. For explicit "I want the canonical bits" use case.
- `--from-source` flag (optional): error if source is unavailable instead of falling back to registry. For debugging / CI.

### Error handling

- Source drift (reconstructed file digest mismatch): log at warn, fall through to registry.
- No source and no registry: error clearly, suggest `--from-registry` or checking credentials.
- Partial failure (some weights succeed, some fail): report per-weight status, non-zero exit.

## Scope (code)

- Update `pkg/cli/weights.go` (or new `weights_pull.go`) with the pull command.
- Register under `cog weights` group.
- Tests: cache hit, source reconstruct path, registry fallback, `--from-registry`, `--from-source` without source.

## Out of scope

- Auto-pull on `cog predict` (separate bean, deferred).
- `cog weights purge` (separate follow-up, not blocking this).

## Dependencies

Blocked by:
- WeightStore interface
- FileWeightStore implementation
- Lockfile v2

## Todo

- [ ] New `cog weights pull` command in `pkg/cli/weights.go`
- [ ] Build `LayerRef`s from lockfile entries
- [ ] Build `FetchMeans` from config + lockfile source block
- [ ] Call `store.Fetch` per weight
- [ ] `--from-registry` / `--from-source` flags
- [ ] Progress reporting (delegates to store or callback)
- [ ] Per-weight error aggregation + clear final status
- [ ] Tests

## Reference

- `plans/2026-04-22-managed-weights-import-and-local-run-design.md` §5
- Supersedes / updates existing `cog-kfvj` (this is the actual work; cog-kfvj will be retitled to match).



## Update 2026-04-22: Post-import pull is cache-hit, not registry round-trip

Clarification following the push-path update to cog-p76s / cog-gbse / cog-3p4a: after a successful `cog weights import`, the WeightStore is already populated (import calls `PutFile` as it packs). So `cog weights pull` following an import hits the cache-hit branch for every layer and returns immediately without touching the registry.

No change to the Fetch priority order or this bean's scope — just noting that "cache warm" is the expected common case, not an edge case.
