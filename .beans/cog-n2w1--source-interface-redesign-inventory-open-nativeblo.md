---
# cog-n2w1
title: 'Source interface redesign: Inventory + Open + NativeBlobRef'
status: todo
type: task
priority: high
created_at: 2026-04-22T20:21:35Z
updated_at: 2026-04-22T20:37:47Z
parent: cog-66gt
---

Replace the current `Source.Fetch(ctx, uri, projectDir) → localDir` interface with a capability-based interface that lets the weights subsystem drive the import pipeline one file/layer at a time without requiring full source materialization.

## Why

Today's pipeline forces `Fetch → localDir → walk → hash → pack` in sequence. That's fine for `file://` but impossible for a 600 GB HuggingFace repo on a 200 GB laptop.

The new interface separates "tell me about the source" (Inventory, cheap for hf/oci, unavoidable walk for file) from "give me bytes for one file" (Open, on-demand). The weights subsystem owns the packing strategy.

## New interface

```go
type Source interface {
    Inventory(ctx context.Context, uri, projectDir string) (Inventory, error)
    Open(ctx context.Context, uri, path string) (io.ReadCloser, error)
    NativeBlobRef(uri, path string) (BlobRef, bool)
}

type Inventory struct {
    Files       []InventoryFile
    Fingerprint weightsource.Fingerprint
}

type InventoryFile struct {
    Path   string // relative to the weight target
    Size   int64
    Digest string // "sha256:<hex>", supplied by the source
}

type BlobRef struct {
    // Scheme-specific reference (future: cross-repo blob mount, etc.)
    // v1 implementations leave this empty.
}
```

## Scope

- Define the new interface in `pkg/model/weightsource/source.go`.
- Port `FileSource` to the new shape:
  - `Inventory`: walks source dir, hashes files, returns `Inventory` with `sha256:<setDigest>` fingerprint (same computation as today's `Fingerprint`).
  - `Open`: opens local file by path, returns `io.ReadCloser`.
  - `NativeBlobRef`: returns `(BlobRef{}, false)`.
- Update `pkg/model/weight_builder.go` and callers to use `Inventory` + `Open` instead of `Fetch`.
- Keep `NormalizeURI` and `Fingerprint` type unchanged.
- Remove `fingerprintForSource` helper (no longer needed — `Inventory` returns the fingerprint directly).

## Out of scope

- hf://, s3://, http:// implementations (separate beans).
- Changes to the packer's tar writing — it still reads from file handles, just via `Source.Open` instead of by path.
- Pending state or plan/execute split (separate beans); this bean keeps the current single-phase flow but with the new interface shape.

## Todo

- [ ] Define new `Source` interface + types in `pkg/model/weightsource/source.go`
- [ ] Port `FileSource.Inventory` (walk + hash; reuse `computeDirSetDigest`)
- [ ] Port `FileSource.Open` (open local file)
- [ ] Port `FileSource.NativeBlobRef` (stub)
- [ ] Update `WeightBuilder.Build` to call `Inventory` + use `Open` where `absSource` / direct file paths were used
- [ ] Update `pkg/model/weights_status.go` if it called `Fetch`/`Fingerprint`
- [ ] Update all tests in `pkg/model/weightsource/`
- [ ] Update `pkg/model/weight_builder_test.go`
- [ ] Remove `fingerprintForSource` helper

## Reference

- `plans/2026-04-22-managed-weights-import-and-local-run-design.md` §1
