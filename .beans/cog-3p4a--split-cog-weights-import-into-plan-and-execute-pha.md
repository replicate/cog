---
# cog-3p4a
title: Split cog weights import into plan and execute phases
status: todo
type: task
priority: high
created_at: 2026-04-22T20:22:18Z
updated_at: 2026-04-27T19:01:48Z
parent: cog-66gt
blocked_by:
    - cog-n2w1
    - cog-4rmi
    - cog-p76s
    - cog-gbse
---

Refactor the import pipeline into a plan phase (inventory → layer plan, writes pending state) and an execute phase (iterates pending state, packs + pushes layers, updates pending state). Manifest assembly + lockfile promotion becomes a third phase gated on all-layers-pushed.

## Why

Enables resumption of interrupted imports at per-layer granularity. Enables disk-constrained imports: pack one layer, push it, delete the tar, move to the next. Required for Kimi K2.5 (~600 GB) on laptops (~200 GB free).

## Scope

### Plan phase (no registry, no payload downloads beyond source inventory)

1. Resolve `Source` from the URI scheme.
2. Call `Source.Inventory` → file list with digests + fingerprint.
3. Apply `include`/`exclude` filters.
4. Classify files by `bundle_file_max`; assign to layers.
5. Compute `setDigest` and per-layer `contentsDigest`.
6. Write pending state with all layers in `planned` state.

### Execute phase (per layer, bounded concurrency)

For each planned layer:

1. Transition `planned` → `packing`. Call `Source.Open` for each member file, pack into a deterministic tar in `.cog/weights-cache/<name>/<contentsDigest>.tar[.gz]`, compute tar blob digest.
2. Verify re-pack's `contentsDigest` matches plan (source drift check).
3. Transition `packing` → `pushing`. Upload via `registry.WriteLayer`.
4. Transition `pushing` → `pushed`. Update pending state with `blobDigest`, `size`, `sizeUncompressed`.
5. Optional `--purge-after`: delete cached tar after push.

### Manifest phase

1. Once all layers are `pushed`, assemble OCI manifest (existing code).
2. Push manifest.
3. Promote pending state into `weights.lock`. Save atomically.
4. Delete pending state file.

### Resumption

On subsequent `cog weights import`:

- If pending state exists and fingerprint + plan match current inventory → skip planning, resume from per-layer state.
- If inventory drifted → discard pending state, re-plan. Log loudly.

Cached tars in `.cog/weights-cache/` whose `contentsDigest` matches the plan are reused (current cache-hit path, re-keyed on `contentsDigest`).

### CLI

- `--dry-run` runs plan only, prints the plan, writes pending state, exits without executing.
- Otherwise the externally-observed CLI behavior is unchanged: `cog weights import [name...]` still does the whole thing in one invocation.

## Out of scope

- `Source` interface changes (separate bean — prerequisite).
- Pending state file format (separate bean — prerequisite).
- HuggingFace source implementation (separate bean).
- Progress UX (covered by cog-66fc).

## Todo

- [ ] Refactor `WeightBuilder` into `Planner` + `Executor` (or equivalent split)
- [ ] Planner writes pending state on completion
- [ ] Executor reads pending state, processes layers in bounded pool
- [ ] Per-layer state transitions write pending state atomically
- [ ] Manifest phase gated on all `pushed`
- [ ] Lockfile promotion deletes pending state on success
- [ ] Resumption path: detect compatible pending state, skip re-planning
- [ ] Resumption path: detect incompatible pending state, discard + re-plan
- [ ] Key `.cog/weights-cache/` on `contentsDigest` instead of current blob digest
- [ ] `--dry-run` flag on `cog weights import`
- [ ] Update `pkg/cli/weights.go` import command
- [ ] Tests: plan → execute → manifest path, resumption, source drift during execute, `--dry-run`

## Dependencies

Blocked by:
- Source interface redesign
- Pending state file
- Lockfile v2 (ContentsDigest field)

## Reference

- `plans/2026-04-22-managed-weights-import-and-local-run-design.md` §3



## Update 2026-04-22: Import populates WeightStore during packing

The executor no longer reads source bytes twice (once to hash/pack, once to populate WeightStore later). Revised per-layer flow:

1. Transition `planned` → `packing`.
2. For each member file:
   - Open `Source.Open(uri, path)`.
   - Wrap reader with hash-verify.
   - Call `store.PutFile(ctx, expectedDigest, size, reader)`. Streams source → WeightStore's `files/sha256/<digest>`, verified against inventory's expected digest.
3. Pack the tar by reading from the WeightStore's `files/` paths (not source). Writes tar to `.cog/weights-cache/<name>/<contentsDigest>.tar[.gz]`. Verifies re-pack's `contentsDigest`.
4. Call `store.PutLayerMembership(ctx, contentsDigest, files)` to record the `.list` sidecar.
5. Transition to `pushing`; upload tar via `registry.WriteLayer`.
6. Transition to `pushed`; update pending state.
7. Optional `--purge-after`: delete `.cog/weights-cache/<...>.tar`. Files in WeightStore `files/` are preserved.

Consequences:
- After import, WeightStore is warm. `cog weights pull` is a no-op. `cog predict` just needs Mount to hardlink-assemble.
- If PutFile detects the file already exists in the store (same digest from a prior import or another weight), source re-read is skipped.
- Source drift (source file hashes differently than inventory promised) fails PutFile and fails the import loudly.

Blocked by: cog-p76s + cog-gbse (in addition to cog-n2w1, cog-4rmi, cog-vs3r).



## Update 2026-04-22 (second): no tar cache, stream-pack to registry

The previous update still referenced `.cog/weights-cache/<name>/<contentsDigest>.tar`. That directory is removed entirely in the new design — it was conflating a build-phase scratch with the durable WeightStore.

Revised per-layer execute flow:

1. Transition `planned` → `packing`.
2. `PutFile` each member file into WeightStore (streaming source → hash-verify → `files/sha256/<digest>`). Idempotent.
3. `PutLayerMembership` records the layer's `.list`.
4. Transition → `pushing`. Build a `v1.Layer` via `tarball.LayerFromOpener(openerFn)` where `openerFn` streams the tar on demand by reading from the WeightStore's `files/` paths. Tar never hits disk.
5. `registry.WriteLayer` uploads; if it retries, it calls the opener again, which re-packs deterministically (local IO, no source re-read).
6. Transition → `pushed`; update pending state with blob digest / sizes.

Consequences:
- Remove all code that writes or reads `.cog/weights-cache/` tar files (in `WeightBuilder`, `layerCachePath`, `cachedLayers`, `resetCacheDir`, etc.).
- Remove `WeightsCacheDir` constant and its filesystem operations.
- `--purge-after` semantics shift from "delete cached tars after push" to "delete WeightStore files for this weight after push" (useful in CI).
- Resumption no longer needs to reconcile cached tars with pending state — just re-runs per-layer work from the pending state; WeightStore's `files/` and pending state are the only persistent artifacts.

Updated todo list:
- [x] (dropped) Key `.cog/weights-cache/` on `contentsDigest` — no such cache anymore
- [ ] Remove `.cog/weights-cache/` code paths entirely
- [ ] Remove `WeightsCacheDir`, `layerCachePath`, `cachedLayers`, `resetCacheDir`
- [ ] Replace with streaming `v1.Layer` (tarball.LayerFromOpener) that reads from WeightStore
- [ ] `--purge-after` now targets WeightStore `files/` for the just-imported weight



## Update 2026-04-22 (third): PutLayerMembership dropped

Following cog-vs3r being scrapped and the WeightStore interface simplifying:

- No `PutLayerMembership` call — the `.list` sidecar doesn't exist.
- No `contentsDigest` computation during planning.
- Plan shape drops `contentsDigest` from each `plannedLayer` (pending state format in cog-4rmi updates accordingly).

Revised per-layer execute flow (final):

1. Transition `planned` → `packing`.
2. For each member file: `Source.Open` → `store.PutFile(ctx, file.Digest, file.Size, reader)`. Streams source → `files/sha256/<digest>`, hash-verified at ingress.
3. Transition → `pushing`. Build `v1.Layer` via `tarball.LayerFromOpener(openerFn)` where `openerFn` streams the tar by reading from `files/sha256/...` paths in sorted order.
4. `registry.WriteLayer` uploads; retries re-call the opener, which re-packs deterministically from local files.
5. Transition → `pushed`; update pending state with `blobDigest`, `size`, `sizeUncompressed`.

No sidecar writes. No per-layer membership recording. Lockfile's `Files[]` remains the source of truth for layer membership.

Simpler pending state: just the plan + per-layer state machine + resulting blob digest.



## Update 2026-04-27: scope narrowed by cog-i12u

cog-i12u (Warm local WeightStore during cog weights import) supersedes the parts of this bean that handled WeightStore population, the `.cog/weights-cache/` removal, and the streaming-tar-to-registry shape. Specifically subsumed:

- "Import populates WeightStore during packing" — landed in cog-i12u.
- "Remove `.cog/weights-cache/` code paths entirely" — landed in cog-i12u.
- "Replace persistent tar scratch with streaming `v1.Layer` (tarball.LayerFromOpener) reading from WeightStore" — landed in cog-i12u (both for the digest-only path via `io.Discard` and the push path via streaming opener).
- "PutFile each member file into WeightStore (streaming source → hash-verify → files/sha256/<digest>)" — landed in cog-i12u.

The lockfile gained an `envelopeFormat` digest in cog-i12u, which provides a coarser cache-bust mechanism than per-layer pending state. Combined with `BlobExists`-gated push, the "no-op import is fast" property is achieved without per-layer resumability.

### What's left for this bean

The remaining scope is purely about resumable imports for disk-constrained or interruption-prone scenarios:

- Plan/execute phase split (planner writes pending state; executor iterates)
- Pending state file (cog-4rmi prerequisite)
- Per-layer state machine: planned → packing → pushing → pushed
- Resumption from pending state (skip re-planning if compatible, discard + re-plan if drifted)
- `--purge-after` flag for CI use cases (delete WeightStore files for the just-imported weight after push)
- `--dry-run` (plan-only, prints plan, writes pending state, exits)

### Status

Marking with a note rather than scrapping: the resumability story is genuinely valuable for very large weights (Kimi K2.5–class, ~600GB) on disk-constrained machines. But:

- v2 (BuildKit + DockerStore) has its own resumability story via cache mounts and content-addressed exec, which would obviate this bean.
- For the v1 lifecycle, no production users today need resumable imports.
- The cog-i12u envelopeFormat + BlobExists shape covers the common "I ran import, it pushed half my layers, my laptop died" recovery — re-running import streams already-pushed layers' digests via local recompute and skips push for them.

Recommend: revisit at v2 plan time. If v1 ships without users hitting Kimi-K2.5–class problems, scrap. If a real user hits it, do this work.

### Updated todo

- [ ] (subsumed by cog-i12u) ~~Refactor `WeightBuilder` into `Planner` + `Executor`~~ — partial, comparison-first flow already split planning from layer-digest computation
- [ ] Planner writes pending state on completion
- [ ] Executor reads pending state, processes layers in bounded pool
- [ ] Per-layer state transitions write pending state atomically
- [ ] Manifest phase gated on all `pushed`
- [ ] Lockfile promotion deletes pending state on success
- [ ] Resumption path: detect compatible pending state, skip re-planning
- [ ] Resumption path: detect incompatible pending state, discard + re-plan
- [ ] (subsumed by cog-i12u) ~~Remove `.cog/weights-cache/` code paths entirely~~
- [ ] (subsumed by cog-i12u) ~~Replace with streaming `v1.Layer` (tarball.LayerFromOpener) that reads from WeightStore~~
- [ ] `--dry-run` flag on `cog weights import`
- [ ] `--purge-after` flag (delete WeightStore files for this weight after push)
- [ ] Update `pkg/cli/weights.go` import command
- [ ] Tests: plan → execute → manifest path, resumption, source drift during execute, `--dry-run`
