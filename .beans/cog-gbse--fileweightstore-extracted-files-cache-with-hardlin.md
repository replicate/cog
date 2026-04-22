---
# cog-gbse
title: 'FileWeightStore: extracted-files cache with hardlinked assembly'
status: todo
type: task
priority: high
created_at: 2026-04-22T20:23:05Z
updated_at: 2026-04-22T20:38:24Z
parent: cog-kgd7
blocked_by:
    - cog-p76s
---

Implement `WeightStore` against a local directory (`~/.cache/cog/weights/`) using content-addressed extracted files and hardlink-based assembly. Zero duplication across weight sets that share files.

## Why

The v1 backend. No Docker dependency for weight storage. Cross-model dedup is automatic. Source-drift detection falls out naturally (reconstructed file digests get verified). Works offline if source is a local directory.

## Scope

### Layout

```
~/.cache/cog/weights/
  files/sha256/<ab>/<abcdef...>         # content-addressed file blobs
  layers/sha256/<ab>/<abcdef...>.list   # contentsDigest → sorted "<file-digest>  <path>" lines
  assembled/<set-digest>/               # hardlinks into files/, bind-mount target
```

### Operations

- `HasLayer(contentsDigest)`: read the `.list` sidecar; confirm every referenced file exists under `files/`.
- `HasSet(setDigest)`: stat `assembled/<setDigest>/.cog/ready` (spec §3.2 readiness marker, atomically written last).
- `Fetch`, per missing layer, in priority order:
  1. `HasLayer` already → skip.
  2. `means.Source != nil` → reconstruct: `Source.Open` each file, stream to `files/<digest>` (write via tmp + rename, hash as we go, verify against expected digest), write `.list` atomically last.
  3. `means.Registry != nil` → fall back: stream blob, decompress, extract into `files/`, verify each file's digest, write `.list`.
  4. Neither → error with clear message.
- `Mount`: if `HasSet`, return existing path. Otherwise build `assembled/<setDigest>/` by hardlinking each file from `files/` into the weight target tree, write `.cog/ready` atomically last, return the path.

### Constraints

- `files/` and `assembled/` must be on the same filesystem (for hardlinks). Error if violated; don't silent-fallback to copy — bind mounts inside containers don't follow symlinks reliably.
- Atomic writes everywhere: tmp + rename for files, `.list`, `.cog/ready`.
- Per-`setDigest` ref counting for `Mount`/`Release`: multiple concurrent `cog predict` runs share the same assembled dir; `Release` decrements; last release can trigger cleanup (or defer to `cog weights purge`).

### GC

- `cog weights purge` removes specified assembled dirs, then files with zero remaining hardlinks.
- LRU / size-budget eviction is a future enhancement; v1 is purge-on-demand.

## Scope (code)

- `pkg/model/weightstore/filestore.go` (or similar): implementation.
- Helpers: open-and-verify reader wrapper (hashes while streaming), atomic `.list` writer, hardlink assembly.
- Tests: cache hit, cache miss → source fetch, source → registry fallback, source drift (wrong digest in source), concurrent `Mount` ref counting, same-filesystem check, `HasSet` / `HasLayer`.

## Out of scope

- `cog weights pull` wiring (separate bean).
- `cog predict` wiring (separate bean).
- LRU eviction.
- Cross-filesystem copy fallback.

## Dependencies

Blocked by:
- WeightStore interface
- Lockfile v2 (ContentsDigest)

## Todo

- [ ] Create `pkg/model/weightstore/filestore.go`
- [ ] Implement `HasLayer` (read `.list`, check files exist)
- [ ] Implement `HasSet` (stat `.cog/ready`)
- [ ] Implement `Fetch` priority chain (cache → source → registry)
- [ ] Implement streaming file verification (hash while writing)
- [ ] Implement atomic `.list` writer
- [ ] Implement `Mount` hardlink assembly
- [ ] Implement `MountHandle` with `Release` / ref counting
- [ ] Same-filesystem check for `files/` + `assembled/`
- [ ] `Purge(setDigest)` or equivalent for `cog weights purge`
- [ ] Unit tests covering all paths
- [ ] Doc comments

## Reference

- `plans/2026-04-22-managed-weights-import-and-local-run-design.md` §4 (FileWeightStore layout and operations)



## Update 2026-04-22: PutFile + PutLayerMembership for the push path

FileWeightStore also serves import (not just pull/predict). Implementation additions:

- `PutFile(ctx, expectedDigest, size, r)`: stream reader → tmp file, hash while writing, verify digest matches, rename to `files/sha256/<digest>`. Idempotent: if `files/sha256/<digest>` already exists, discard the reader and return nil.
- `PutLayerMembership(ctx, contentsDigest, files)`: atomic write of `layers/sha256/<contentsDigest>.list` with sorted `<file-digest>  <path>` lines, same format Fetch produces.

Tests:
- PutFile happy path
- PutFile with digest mismatch (reject, clean up tmp)
- PutFile idempotent (second call no-ops)
- PutLayerMembership atomic replace
- Concurrent PutFile of the same digest doesn't corrupt

The tar packer in import now reads from `files/sha256/<digest>` rather than from source directly — files are guaranteed local and verified by the time we pack. Packing becomes deterministic and cache-friendly: if we ran the import before for this setDigest, every file is already in the store and packing is pure local I/O.



## Update 2026-04-22 (ContentsDigest dropped)

Following cog-vs3r / cog-p76s updates:

- Remove `layers/sha256/<contentsDigest>.list` sidecar from the on-disk layout.
- Remove `HasLayer` and `PutLayerMembership` from the implementation.
- `HasFiles` replaces `HasLayer`: iterate `files` argument, `stat` each `files/sha256/<digest>` under the cache root.

### Revised on-disk layout

```
~/.cache/cog/weights/
  files/sha256/<ab>/<abcdef...>    # content-addressed file blobs (unchanged)
  assembled/sha256/<set-digest>/    # hardlinks into files/, bind-mount target (unchanged)
```

Only two dirs. Cleaner.

### Revised `Fetch` per-layer flow

For each layer in the request:

1. For each file in `layer.Files`:
   - `stat files/sha256/<file.Digest>` → if present, skip.
   - `means.Source != nil`: `Source.Open(uri, file.Path)` → `PutFile(ctx, file.Digest, file.Size, reader)`.
   - `means.Registry != nil` (fallback, layer already missed per-file cache): stream the layer blob from the registry, decompress, extract tar, for each entry `PutFile(...)`.
   - Neither available → error.

Consequences:
- Registry fallback operates per-layer (can't do per-file from the registry — layers are the transport unit). Source reconstruction operates per-file.
- Per-file cache hit skips registry pulls entirely when any prior import populated the file (cross-model dedup is automatic).

### Revised todo

- [ ] `files/sha256/<ab>/<abcdef...>` layout
- [ ] `assembled/<set-digest>/` layout with `.cog/ready` marker
- [ ] `HasSet` (stat `.cog/ready`)
- [ ] `HasFiles` (stat each file in the list)
- [ ] `Fetch`: per-file check → source-reconstruct → registry-layer-fallback
- [ ] `PutFile` (streaming hash-verify, idempotent, tmp + rename)
- [ ] `Mount` (hardlink files/ → assembled/; atomic .cog/ready last)
- [ ] `MountHandle` with `Release` / ref counting
- [ ] Same-filesystem check for files/ + assembled/
- [ ] `Purge(setDigest)` for `cog weights purge`
- [ ] Unit tests covering all paths
