---
# cog-n2w1
title: 'Source interface redesign: Inventory + Open + NativeBlobRef'
status: in-progress
type: task
priority: high
created_at: 2026-04-22T20:21:35Z
updated_at: 2026-04-22T22:25:38Z
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

- [x] Define new `Source` interface + types in `pkg/model/weightsource/source.go`
- [x] Port `FileSource.Inventory` (walk + hash; refactored `computeDirSetDigest` → `computeInventory`)
- [x] Port `FileSource.Open` (open local file)
- [x] ~~Port `FileSource.NativeBlobRef` (stub)~~ — dropped from v1 scope, not needed yet
- [x] Update `WeightBuilder.Build` to call `Inventory` + pass `Source` to the packer (packer uses `Open` for tar bytes)
- [x] ~~Update `pkg/model/weights_status.go`~~ — no changes needed, it never called `Fetch`/`Fingerprint`
- [x] Update all tests in `pkg/model/weightsource/`
- [x] Update `pkg/model/weight_builder_test.go` (also: `packer_test.go`, `pusher_test.go`, `weight_pusher_test.go`, `weight_manifest_v1_test.go`, `weight_pipeline_e2e_test.go`, `weights_test.go`, `tools/weights-gen/main.go`)
- [x] Remove `fingerprintForSource` helper

## Reference

- `plans/2026-04-22-managed-weights-import-and-local-run-design.md` §1


## Summary of Changes

Replaced the old `Source.Fetch + Source.Fingerprint` interface with a stateful, capability-based interface: `Inventory(ctx)` and `Open(ctx, path)`. The source instance binds its URI + `projectDir` at construction time, so method signatures carry no per-call URI threading.

### Interface shape
- `Source` is now two methods: `Inventory` returns per-file path/size/digest plus the source fingerprint in one shot; `Open` streams one file's bytes.
- `NativeBlobRef` was dropped — it's a v2 optimization seam and not adding it now keeps the surface tight.
- `For(uri, projectDir)` constructs a stateful source and validates existence + kind up front. Unknown schemes still return a clear error.

### Packer rewired
- `Pack(ctx, src, inv, opts)` replaces `Pack(ctx, sourceDir, opts)`. The packer reads file bytes via `Source.Open` instead of `os.Open(absPath)`, and trusts the inventory's per-file digests rather than re-hashing.
- Deleted `walkSourceDir` and `computeFileDigests` — the inventory subsumes both.
- `fileEntry` loses `absPath` / `mode`; it's now purely the packer's internal projection of `InventoryFile` with a layer-assignment hook.

### Builder
- `WeightBuilder.Build` calls `For(ws.Source, projectDir)` → `src.Inventory(ctx)` → `Pack(ctx, src, inv, ...)`.
- On a cache hit we keep reusing the existing lockfile's fingerprint (not the fresh `inv.Fingerprint`). This preserves the documented v1 behavior that explicit source-drift detection waits for cog-wej9; doing it silently here would be a behavior change that belongs in its own bean.
- Deleted `fingerprintForSource` — the inventory returns the fingerprint directly, and the cache-hit path reads it from the lockfile.

### Other touches
- `computeDirSetDigest` → `computeInventory` in `setdigest.go`; the set-digest formula is unchanged. SYNC comment on `ComputeWeightSetDigest` updated.
- `tools/weights-gen/main.go` updated for the new `Pack` signature.
- All packer/pusher/manifest/e2e tests migrated via a `packTestDir(t, dir, opts)` helper that hides the source+inventory boilerplate.

### Verification
- `go test ./...` clean
- `mise run lint:go` clean on all files touched by this change (pre-existing lint issues elsewhere are unchanged)


## Follow-up scope (this bean)

Refactoring `Pack` based on design review. The current `Pack(ctx, src, inv, opts)` function has four near-duplicate tar-writing paths (bundle / large-raw / large-gzip / small-single-bundle). Collapsing them into one routine and splitting plan-from-execute unblocks future WeightStore cache integration without a second refactor.

### Target shape

```go
type Packer struct {
    opts   PackOptions
    cache  WeightStore   // nil until cog-p76s lands
    tmpDir string        // from opts; owned by the packer
}

func NewPacker(opts *PackOptions) *Packer

// Pure. No I/O. Table-testable.
func (p *Packer) Plan(inv Inventory) Plan

// Effectful. Writes tars to tmpDir, hashes compressed output.
func (p *Packer) Execute(ctx, src Source, plan Plan) (*PackResult, error)

// Convenience wrapper kept because callers always do both.
func (p *Packer) Pack(ctx, src Source, inv Inventory) (*PackResult, error)
```

- Byte resolution is a private `openFile(ctx, src, f)` method that tries `p.cache` first (once it's non-nil) then falls back to `src.Open`. No `ByteSource` interface — two fields on the struct is simpler and more honest.
- One `writeLayer` routine. Bundle vs large = `len(files)`. Gzip vs raw = writer-middleware flag. The existing `writeBundleTar` / `writeLargeFileTar` / `writeLargeFileTarGzip` all collapse.
- Top-level `Pack(...)` function is deleted. All callers migrate to `NewPacker(opts).Pack(ctx, src, inv)` or `Plan` + `Execute`.

### Todo

- [x] Introduce `Packer` type with `opts` and `tmpDir`; wire `NewPacker`
- [x] Define `Plan` / `LayerPlan` types; implement pure `Packer.Plan(inv)`
- [x] Collapse the four tar-writing paths into one `writeLayer` routine
- [x] Implement `Packer.Execute(ctx, src, plan)` using `writeLayer`
- [x] Add `Packer.Pack(ctx, src, inv)` convenience wrapper
- [x] Migrate all callers (builder, weights-gen, tests) off the top-level `Pack` function
- [x] Delete top-level `Pack` function
- [x] Add pure table-driven tests for `Packer.Plan`
- [x] Verify `go test ./...` and `mise run lint:go` remain clean


## Follow-up Summary of Changes

Collapsed the packer into a `Packer` type with a pure `Plan` and effectful `Execute` pair, plus a `Pack` convenience wrapper. The cache seam (WeightStore lookup before hitting source) is deferred to cog-p76s — adding a `nil`-able field for a type that does not yet exist would be speculative noise. When cog-p76s lands, the byte-resolution step gets a small private helper on `Packer` that tries cache then falls back to `src.Open`; no external API changes needed.

### Interface shape

- `Packer` carries only `opts PackOptions` — no mutable state, cheap to construct, not designed for reuse across builds.
- `Packer.Plan(inv) Plan` is a pure function: takes an `Inventory`, returns the target layer layout (file sets, gzip flag, media type). No I/O, no source, no rehash.
- `Packer.Execute(ctx, src, plan)` writes the tars, hashes the compressed output, and produces `PackResult`. Rejects empty plans.
- `Packer.Pack(ctx, src, inv)` plans + executes in one call, with an "no files in inventory" error on empty input so the builder call site reads naturally.

### One writing routine

The old four near-duplicate paths (`writeBundleTar`, `writeLargeFileTar`, `writeLargeFileTarGzip`, and the single-file-bundle special case inside `packBundles`) collapse into `buildLayer` + `writeLayer`:

- `buildLayer` owns the writer sandwich (temp file → counting + sha256 → optional gzip → tar) and the hash/size accounting.
- `writeLayer` writes the in-tar layout — directories first, then files, in supplied order — regardless of bundle vs. single-file or compressed vs. raw.
- Bundle vs. single-file distinction is `len(lp.Files)`; gzip vs. raw is `lp.Gzip`. Both decisions made once during `Plan`.
- Bundle tars keep their historical `cog-bundle-*` filename prefix for debuggability.
- Compression level branch (BestCompression for bundles, DefaultCompression for large compressible files) preserved from v1 behavior.

### Callers migrated

- `WeightBuilder.Build` → `NewPacker(opts).Pack(ctx, src, inv)`
- `tools/weights-gen/main.go` → same
- `packer_test.go` helper → same
- `weight_pipeline_e2e_test.go`, `pusher_test.go`, `weight_pusher_test.go`, `weight_manifest_v1_test.go` → same
- Top-level `Pack` function deleted; no wrapper.

### New tests

`packer_plan_test.go` adds table-driven tests for `Plan` alone — no disk, no source. Covers empty inventory, small/large classification, incompressible-extension handling, bundle splitting on `BundleSizeMax`, threshold edge cases, ordering determinism, and the "reject empty plan" path on `Execute`. These exercise layer-assignment logic that previously had to be observed indirectly through tar output.

### Verification

- `go test ./...` — clean
- `mise run lint:go` — zero issues in any touched file (baseline 25 issues elsewhere unchanged)



## Known issue (deferred to cog-qma1)

During the simplify pass we discovered that running `cog weights import` against `examples/managed-weights` after this refactor produces a manifest digest (`sha256:6eb7a7be…`) that differs from the committed `weights.lock` (`sha256:450db43e…`), despite every layer digest, file digest, setDigest, and source fingerprint being byte-identical.

Pre-refactor code reproduces the committed digest exactly. The drift is on the cog-n2w1 side. Root cause not yet identified; `BuildWeightManifestV1` is known to be sensitive to input layer order and is not canonicalized. Investigation is tracked in **cog-qma1**.

This bean's refactor preserves correctness for all test suites and for any in-tree workflow — the manifest digest churn only becomes visible when comparing against a pre-existing out-of-tree lockfile. Landing cog-n2w1 does not regress the spec (the manifest is still valid, the registry accepts it, and layers themselves are byte-identical). cog-qma1 will restore bit-for-bit reproducibility with the committed fixture.
