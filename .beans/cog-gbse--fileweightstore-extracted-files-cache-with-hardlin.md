---
# cog-gbse
title: 'FileWeightStore: content-addressed file cache'
status: completed
type: task
priority: critical
created_at: 2026-04-22T20:23:05Z
updated_at: 2026-04-24T00:18:47Z
parent: cog-kgd7
blocked_by:
    - cog-p76s
---

Implement `WeightStore` against a local directory under `$XDG_CACHE_HOME/cog/weights/` using content-addressed file storage. Zero duplication across weight sets that share files — hardlink assembly happens outside the store (in `WeightManager`).

## Why

The v1 backend. No Docker dependency. Cross-model dedup is automatic (shared files collapse to one on-disk blob). Swappable with a future `DockerWeightStore` without changing callers.

## On-disk layout

```
$XDG_CACHE_HOME/cog/weights/
    files/sha256/<ab>/<abcdef...>    # content-addressed file blobs
```

One directory, two-char prefix bucketing for filesystem sanity on large stores. Resolved via `pkg/paths.WeightsStoreDir()` which wraps `os.UserCacheDir()` + `COG_CACHE_DIR` override.

## Method implementations

**`Exists(ctx, digest)`**
- Parse `digest` into algorithm + hex; reject non-sha256 for v1.
- `os.Stat(files/sha256/<ab>/<abcdef...>)`.
- `(true, nil)` on success, `(false, nil)` on `os.ErrNotExist`, `(false, err)` otherwise.

**`PutFile(ctx, expectedDigest, size, r)`**
- Parse digest. Reject non-sha256.
- Fast path: if `Exists(digest)` → discard reader via `io.Copy(io.Discard, r)`, return nil. Idempotent.
- Slow path:
    1. Create temp file in `files/sha256/<ab>/` (same dir as final so rename is atomic).
    2. Wrap reader with `io.TeeReader` into `sha256.New()` hasher; copy to temp file.
    3. Honor `ctx.Err()` (periodic check or cancelable reader).
    4. After copy: compare computed digest to `expectedDigest`. On mismatch: `os.Remove(tempfile)`, return error with both digests.
    5. On match: `os.Rename(tempfile, final)`.
- Atomic rename. Concurrent PutFile of the same digest: both succeed, bytes are identical, last rename wins.

**`Open(ctx, digest)`**
- `os.Open(files/sha256/<ab>/<abcdef...>)`. Wraps `fs.ErrNotExist` on not-found.

**`Path(ctx, digest)`**
- `Exists` check first; return computed path on success, `fs.ErrNotExist` otherwise.

**`List(ctx)`**
- Walk `files/sha256/` two levels deep (prefix dir → file).
- Yield `FileInfo{Digest, Size}` per entry.
- Respect `ctx.Done()` between yields.

**`Delete(ctx, digest)`**
- `os.Remove(files/sha256/<ab>/<abcdef...>)`.
- Treat `os.ErrNotExist` as success.

## Constructor

```go
func NewFileStore(root string) (*FileStore, error) {
    if err := os.MkdirAll(filepath.Join(root, "files", "sha256"), 0o755); err != nil {
        return nil, fmt.Errorf("create store: %w", err)
    }
    return &FileStore{root: root}, nil
}
```

Prefix dir (`files/sha256/<ab>/`) created lazily in `PutFile`.

## Edge cases

- **Concurrent writers of same digest**: atomic rename; both succeed.
- **Interrupted write**: temp file left in `files/sha256/<ab>/`. Deferred cleanup to future `cog weights gc`.
- **Filesystem full**: `io.Copy` errors, temp file removed, PutFile returns error.
- **Digest collision (modulo SHA-256)**: idempotent Put discards reader; no corruption possible.

## Pruning (future)

Not in this bean. Documented approach: when we need pruning, touch-on-link in the Manager side (assembly step), then a periodic `cog weights gc` walks the store using `List` and deletes entries older than N or beyond a size budget.

## Scope (code)

- `pkg/weights/store/file.go`: `FileStore` type + six method impls.
- `pkg/paths/` helper (new pkg): `WeightsStoreDir()` wraps `os.UserCacheDir()` with `COG_CACHE_DIR` env override.
- Unit tests in `pkg/weights/store/file_test.go`:
    - Round-trip Put/Exists/Open/Path
    - Digest mismatch rejection + temp file cleanup
    - Idempotent PutFile on existing digest
    - Open/Path/Delete on non-existent digest → `fs.ErrNotExist` / no error
    - List yields all entries, respects context cancel
    - Concurrent PutFile (two goroutines, same digest) both succeed
    - Interrupted write leaves no visible final file

## Out of scope

- WeightManager orchestrator (cog-xhpw / cog-40ed)
- `cog weights pull` wiring (cog-xhpw)
- `cog predict` wiring (cog-40ed)
- Pruning / GC
- Cross-filesystem hardlink fallback (Manager-level concern, not store)

## Todo

- [x] New `pkg/paths/` package with `WeightsStoreDir()` and `COG_CACHE_DIR` override
- [x] `pkg/weights/store/file.go` with `FileStore` type
- [x] Implement `Exists`
- [x] Implement `PutFile` (streaming hash-verify, atomic rename, idempotent)
- [x] Implement `Open`
- [x] Implement `Path`
- [x] Implement `List` (iter.Seq2)
- [x] Implement `Delete`
- [x] Unit tests covering all paths + edge cases
- [x] Doc comments

## Reference

- cog-p76s (interface)
- Session design discussion: 2026-04-23 (chat log)

## Summary of Changes

- `pkg/paths/paths.go` — `WeightsStoreDir()` with `COG_CACHE_DIR` override, falling back to `os.UserCacheDir`/cog/weights. Unit tests for both paths.
- `pkg/weights/store/file.go` — `FileStore` implementing `WeightStore` against `<root>/files/sha256/<ab>/<hex>`. `PutFile` is streaming-hash-verified, atomic-rename, idempotent (drains the reader on cache hit so tar iteration stays in sync). Digests are validated at the boundary — only `sha256:<64 lowercase hex>` is accepted.
- `pkg/weights/store/file_test.go` — round-trip, digest-mismatch rejection + temp cleanup, idempotency, Open/Path/Delete on absent digests, List (populated/empty/cancel/stray-temp skip), concurrent writers of the same digest, interrupted write leaves no final file, invalid digest rejection.

Interface conformance asserted via `var _ WeightStore = (*FileStore)(nil)`.

`mise run lint:go` → 0 issues; `go test ./...` → all green.
