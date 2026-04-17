---
# cog.md-managed-weights-v0.0.2-4fg4
title: Reshape weight factory + artifact types around v1 multi-layer model
status: todo
type: task
priority: high
created_at: 2026-04-17T20:19:04Z
updated_at: 2026-04-17T20:20:00Z
parent: cog.md-managed-weights-v0.0.2-9qcd
blocked_by:
    - cog.md-managed-weights-v0.0.2-b2mv
---

Rework the weight data model inside the existing resolver/factory/artifact pipeline so that v1 (multi-layer, directory-as-weight) manifests flow through the same common types that `pull` / `inspect` / `build` / `push` already use. Delete v0-specific code once callers are moved over.

## Architectural principle

The resolver/factory/`Artifact`/`ArtifactSpec` abstraction is load-bearing: it gives `pull`, `inspect`, `build`, and `push` a single consistent representation of a Model and its components. The v1 code added in b2mv must **integrate into this pipeline**, not live alongside it as a sideloaded system. The new work is effectively a **weight factory** that produces a v1-shaped `WeightArtifact` the same way `ImageFactory` produces an `ImageArtifact`.

## Why a redesign (not a rename)

v0 and v1 differ structurally:

- **v0**: a weight is one file. `WeightSpec{Source, Target}` is a single file path. `WeightArtifact.FilePath` is a single file. `WeightBuilder` hashes one file. Lockfile entries are per-file. Manifest has one layer with a typed config blob.
- **v1**: a weight is a directory. `Pack()` walks the directory and produces N layers (bundled small files + one-per large file). The manifest has N layers with per-layer annotations and uses the OCI empty descriptor as its config. No per-file lockfile entry — the manifest digest is the unit of identity.

## Keep (the pipeline)

- `Resolver` / `Factory` interface / `ArtifactSpec` / `Artifact` interface — the common shape
- `BundlePusher` orchestration — still the entry point for a multi-component push; it just hands `WeightArtifact` instances to a v1-aware pusher method
- `Model.WeightArtifacts` / `Model.Artifacts` — the contract with callers is unchanged
- `Source` walking over `cog.yaml` — same entry point; produces v1-shaped `WeightSpec` instances
- Index builder — still composes an OCI index from image + weight artifacts
- `weights.lock` — same file, same role; schema evolves to v1 (manifest digest + layer digests/sizes per weight)
- `cog.yaml` user-facing fields — unchanged

## Change (within the pipeline)

- `WeightSpec` — source is a directory (or resolvable-to-directory reference), not a single file
- `WeightArtifact` — carries `[]LayerResult` + manifest digest + target, instead of a single `FilePath` + `WeightConfig`
- `WeightBuilder` — the "weight factory": given a `WeightSpec`, calls `Pack()` on the source directory, returns a `WeightArtifact` holding the layer results. Implements `Factory` the same way `ImageFactory` does.
- `WeightPusher.PushMultiLayer` — becomes the (renamed) `WeightPusher.Push` once v0 `Push` goes away; called by `BundlePusher.pushWeights` with a `WeightArtifact` from the factory
- `weights.lock` schema — v1 shape per spec §3.6 / the `/.cog/weights.json` model image metadata

## Delete (v0-specific leaves)

- The v0 single-file code path inside `WeightBuilder` and `WeightPusher` (but not the types themselves — we reshape them)
- `buildWeightImage`, `weightManifestImage`, `weightOCIManifest` — superseded by `BuildWeightManifestV1` + `weightManifestV1Image` in b2mv's file
- v0-only media types: `MediaTypeWeightLayer`, `MediaTypeWeightConfig`, `MediaTypeWeightLayerGzip`, `MediaTypeWeightLayerZstd`
- v0-only annotation keys in `artifact_weight.go`: `AnnotationWeight{Name,Dest,DigestOriginal,SizeUncompressed}` (the `AnnotationV1*` keys in `packer.go` / `weight_manifest_v1.go` replace them)
- `WeightConfig` struct (v1 uses the OCI empty descriptor; no typed config blob)
- References to the deleted media types in `tools/weights-gen/main.go`

## Files touched

Inside `pkg/model/`:
- Keep + edit: `resolver.go`, `source.go`, `factory.go`, `pusher.go`, `model.go`, `index.go`, `artifact_weight.go`, `weights.go`, `weights_lock.go`, `weight_builder.go`, `weight_pusher.go`, `weight_manifest_v1.go`
- Corresponding `_test.go` files: update fixtures, not structure
- Delete or fold: `buildWeightImage` + `weightManifestImage` code inside `weight_pusher.go`

Elsewhere:
- `tools/weights-gen/main.go` — swap v0 media types for v1
- `integration-tests/` — any test referring to v0 layer shape

## Verification

- `mise run lint:go` — zero new issues
- `mise run test:go`
- `mise run test:integration`
- Manual round-trip: cog.yaml with a weights entry → `cog build` → `cog push` → `crane manifest` on the pushed weight ref. Confirm it matches spec §2.2. Then `cog inspect` (or equivalent) on the pushed reference and confirm the resolver/factory path round-trips the same manifest structure back through the common `Artifact` types.
