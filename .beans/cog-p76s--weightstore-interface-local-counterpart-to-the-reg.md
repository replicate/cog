---
# cog-p76s
title: 'WeightStore interface: local counterpart to the registry'
status: todo
type: task
priority: high
created_at: 2026-04-22T20:22:41Z
updated_at: 2026-04-22T20:38:09Z
parent: cog-kgd7
---

Define the `WeightStore` interface that abstracts local-side layer storage and weight assembly. Enables swapping backends (file cache now, Docker daemon later) without changing callers in `cog weights pull` / `cog predict`.

## Why

We want the "remote Docker daemon with GPUs and huge disks" workflow to drop in later without rewriting `cog predict` or `cog weights pull`. Locking the shape of the abstraction now means every WeightStore backend talks the same contract: "tell me what you have, fetch what you don't, give me a path to bind-mount."

## Scope

This bean is interface + types + docs only. Implementations are separate beans.

```go
type WeightStore interface {
    HasSet(ctx context.Context, setDigest string) (bool, error)
    HasLayer(ctx context.Context, contentsDigest string) (bool, error)
    Fetch(ctx context.Context, setDigest string, layers []LayerRef, means FetchMeans) error
    Mount(ctx context.Context, setDigest string, layers []LayerRef) (MountHandle, error)
}

type LayerRef struct {
    ContentsDigest string
    BlobDigest     string
    MediaType      string
    Size           int64
    Files          []LayerFileRef // path + size + digest, from lockfile.Files
}

type LayerFileRef struct {
    Path   string
    Size   int64
    Digest string // sha256:<hex>
}

type FetchMeans struct {
    Source   weightsource.Source // nil if source unavailable
    URI      string
    Registry registry.Client     // nil if offline
    Repo     string
}

type MountHandle interface {
    Path() string       // bind-mount source (read-only)
    Release() error     // decrement ref count / cleanup
}
```

### Key contract points

- `HasLayer` is keyed on `ContentsDigest`, not `BlobDigest`, because the store operates on extracted files (v1) or tars (v2); `BlobDigest` is only meaningful for the registry-fetch fallback.
- `Fetch` is provider-driven: the store decides whether to use `means.Source`, `means.Registry`, or delegate (e.g. `DockerWeightStore` may ignore both and ask its daemon to `docker pull`).
- `Mount` returns a path that can be bind-mounted `:ro` into a container. Caller must `Release` the handle when the container exits.

## Scope (code)

- New package: `pkg/model/weightstore/` (or similar).
- Interface + types in one file.
- Doc comments on every exported symbol. These are the contract.
- No implementations in this bean — subsequent beans provide `FileWeightStore` (v1) and `DockerWeightStore` (v2, deferred).

## Out of scope

- `FileWeightStore` implementation (separate bean).
- Wiring into `cog weights pull` or `cog predict` (separate beans).
- `DockerWeightStore` (deferred, separate bean).

## Todo

- [ ] Create `pkg/model/weightstore/` package
- [ ] Define `WeightStore` interface
- [ ] Define `LayerRef`, `LayerFileRef`, `FetchMeans`, `MountHandle`
- [ ] Doc comments explaining the contract (especially `ContentsDigest` vs `BlobDigest`)
- [ ] Helper: `LayerRefsFromLockEntry(entry *WeightLockEntry) []LayerRef` — bridges the lockfile and the store

## Reference

- `plans/2026-04-22-managed-weights-import-and-local-run-design.md` §4



## Update 2026-04-22: WeightStore also serves the push path

Import populates the WeightStore as a side effect of packing — so after a successful import, `cog weights pull` is a no-op and `cog predict` can assemble immediately.

This requires the interface to support producer writes, not just consumer reads. Add:

```go
// PutFile stores a file's bytes in the store under its content-addressed
// digest. The reader is consumed and verified against expected-digest
// as it streams. Size is informational (for progress / preallocation).
//
// PutFile is idempotent: if the file already exists at the digest, the
// reader is discarded and no error is returned.
PutFile(ctx context.Context, expectedDigest string, size int64, r io.Reader) error

// PutLayerMembership records which files belong to a layer (the .list
// sidecar in FileWeightStore). Called after all files in a layer have
// been PutFile'd.
PutLayerMembership(ctx context.Context, contentsDigest string, files []LayerFileRef) error
```

The import executor calls `PutFile` as it reads from `Source.Open` (single-pass streaming: source → hash/verify → write to WeightStore), then packs the tar from the stored files (which are now known-good and local), then pushes the tar. No re-read of the source, no orphan tars.

This means cog-gbse (FileWeightStore) has to land before cog-3p4a (plan/execute split), not just before cog-xhpw/cog-40ed. Updated dependencies reflect this.



## Update 2026-04-22 (ContentsDigest dropped)

Following cog-vs3r being scrapped, the interface simplifies:

- Remove `HasLayer(contentsDigest)` — no contentsDigest exists; use `HasFiles([]LayerFileRef)` (or collapse it into `Fetch`'s internal cache check).
- Remove `PutLayerMembership` — no `.list` sidecar needed; layer membership is in the lockfile's `Files[]`.
- `LayerRef` drops `ContentsDigest`. Layers are referenced by `BlobDigest` (for registry operations) and their `Files []LayerFileRef` (for store operations).

### Revised interface

```go
type WeightStore interface {
    HasSet(ctx context.Context, setDigest string) (bool, error)

    // HasFiles reports whether all referenced files are present in the store.
    // Store inspects each file's content digest against its local index.
    HasFiles(ctx context.Context, files []LayerFileRef) (bool, error)

    // Fetch makes the store self-populate with whatever it's missing for
    // (setDigest, layers). Provider-driven: store may use means.Source,
    // means.Registry, or delegate entirely.
    Fetch(ctx context.Context, setDigest string, layers []LayerRef, means FetchMeans) error

    // PutFile stores a file under its content-addressed digest.
    // Streams + verifies. Idempotent.
    PutFile(ctx context.Context, expectedDigest string, size int64, r io.Reader) error

    // Mount returns a path bindable into a container at the weight's target.
    Mount(ctx context.Context, setDigest string, layers []LayerRef) (MountHandle, error)
}

type LayerRef struct {
    BlobDigest string          // registry-facing, from lockfile.Layers[].Digest
    MediaType  string
    Size       int64
    Files      []LayerFileRef  // from lockfile.Files filtered by this layer
}

type LayerFileRef struct {
    Path   string
    Size   int64
    Digest string // sha256:<hex>
}
```

### Revised todo

- [ ] `WeightStore` interface with `HasSet`, `HasFiles`, `Fetch`, `PutFile`, `Mount`
- [ ] `LayerRef`, `LayerFileRef`, `FetchMeans`, `MountHandle` types
- [ ] Doc comments (especially: store operates on file granularity; layers are a lockfile view)
- [ ] Helper: `LayerRefsFromLockEntry(entry *WeightLockEntry) []LayerRef` — splits `entry.Files` by `entry.Files[i].Layer`, builds one `LayerRef` per distinct `BlobDigest`
