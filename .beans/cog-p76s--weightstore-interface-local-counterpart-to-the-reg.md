---
# cog-p76s
title: 'WeightStore interface: content-addressed file store'
status: completed
type: task
priority: critical
created_at: 2026-04-22T20:22:41Z
updated_at: 2026-04-24T00:04:29Z
parent: cog-kgd7
---

Define a narrow `WeightStore` interface: a content-addressed store for individual weight files. The store knows only digests; filenames, layers, URIs, and orchestration live elsewhere.

## Why

We need a swappable local backend for weight bytes. v1 is a file-system cache (cog-gbse); a future `DockerWeightStore` can drop in without changing callers. Keeping the store narrow and digest-only prevents it from accumulating orchestration concerns — that belongs in the `WeightManager` (cog-wmgr or folded into cog-40ed / cog-xhpw).

## Interface

```go
// Package pkg/weights/store

type WeightStore interface {
    // Exists reports whether a file with the given digest is in the store.
    Exists(ctx context.Context, digest string) (bool, error)

    // PutFile stores r under its content-addressed digest. The reader is
    // consumed and verified against expectedDigest as it streams; a
    // mismatch returns an error and leaves the store unchanged.
    // Idempotent: if a file already exists at the digest, the reader is
    // discarded and no error is returned.
    PutFile(ctx context.Context, expectedDigest string, size int64, r io.Reader) error

    // Open returns a reader for the file at the given digest. Caller closes.
    // Returns fs.ErrNotExist if the digest is not in the store.
    Open(ctx context.Context, digest string) (io.ReadCloser, error)

    // Path returns an on-disk path for the file, suitable for hardlinking.
    // Returns fs.ErrNotExist if the digest is not in the store.
    Path(ctx context.Context, digest string) (string, error)

    // List iterates all files in the store.
    List(ctx context.Context) iter.Seq2[FileInfo, error]

    // Delete removes a file. Idempotent on already-missing digests.
    Delete(ctx context.Context, digest string) error
}

type FileInfo struct {
    Digest string
    Size   int64
}
```

Six methods, all digest-keyed. No layer concept. No fetching. No knowledge of source URIs, registries, or lockfiles — those are Manager-level concerns.

## Design decisions

- **No `Fetch` method.** Pulling bytes from a registry or source is orchestration, not storage. Lives in `WeightManager.Pull` (cog-xhpw).
- **No `HasSet` / set digest awareness.** Store operates at file granularity. "Is this weight set fully present?" is computed by the Manager iterating `entry.Files` and calling `Exists` per digest.
- **No layer awareness.** Layers are a registry-transport concept; the store doesn't model them.
- **`Open` and `Path` both.** `Open` for stream-through-another-store use cases; `Path` for hardlink assembly. FileWeightStore supports both natively.
- **`iter.Seq2` for List.** Modern Go; cheap for large stores.

## Scope (code)

- New package: `pkg/weights/store/`
- `store.go`: interface + `FileInfo` type + doc comments
- Sibling file: implementation (cog-gbse)

## Out of scope

- `FileWeightStore` implementation (cog-gbse)
- `WeightManager` orchestrator (folded into cog-xhpw / cog-40ed — the Manager type lives in `pkg/weights/` and wraps this interface)
- BulkFetcher / remote-Docker optimization (deferred; would add a type-assertable optional interface once `DockerWeightStore` is designed)
- `DockerWeightStore` (deferred)

## Todo

- [x] Create `pkg/weights/store/` package
- [x] Define `WeightStore` interface with the six methods above
- [x] Define `FileInfo` type
- [x] Doc comments explaining each method's contract
- [x] Document that `Open` / `Path` return `fs.ErrNotExist` on missing digests

## Reference

- `plans/2026-04-22-managed-weights-import-and-local-run-design.md` §4 (historical; this bean supersedes the layered design)
- Session design discussion: 2026-04-23 (chat log)

## Summary of Changes

Added `pkg/weights/store/` with `store.go` defining the `WeightStore` interface (six methods: Exists, PutFile, Open, Path, List, Delete) and the `FileInfo` struct. Doc comments spell out the not-found-wraps-fs.ErrNotExist contract and note that Path may not be supported by every backend (e.g. a future containerd-backed store). No implementation in this bean — FileWeightStore lands in cog-gbse.
