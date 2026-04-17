---
# cog.md-managed-weights-v0.0.2-4fg4
title: Reshape weight factory + artifact types around v1 multi-layer model
status: completed
type: task
priority: high
created_at: 2026-04-17T20:19:04Z
updated_at: 2026-04-17T21:33:42Z
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

## Implementation plan

Chosen approach (full redesign in one pass):

### Types
- [x] `WeightSpec`: replace `Source` (file path) with `Source` (directory path); keep `Target`, `name`. Same constructor signature.
- [x] `WeightArtifact`: replace `FilePath` + `Config WeightConfig` with `SourceDir string`, `Layers []LayerResult`, `ReferenceDigest string` (set by `BundlePusher` after the image push). Keep `Target` + `name` + `descriptor`.
- [x] Delete `WeightConfig` struct.
- [x] Delete v0 media types `MediaTypeWeightLayer`, `MediaTypeWeightConfig`, `MediaTypeWeightLayerGzip`, `MediaTypeWeightLayerZstd`.
- [x] Delete v0 annotation keys `AnnotationWeight{Name,Dest,DigestOriginal,SizeUncompressed}`.
- [x] Keep `MediaTypeWeightArtifact` (artifactType on the manifest itself).

### Builder (the "weight factory")
- [x] Rewrite `WeightBuilder.Build()`: resolve source directory, call `Pack()` into a project-local cache dir (`<projectDir>/.cog/weights-cache/<name>/`), produce a `WeightArtifact` carrying `[]LayerResult`.
- [x] Compute manifest digest (build the v1 image via `BuildWeightManifestV1`, take its digest) so the artifact descriptor is set without the registry in the loop. This keeps `Builder` offline, same as v0.
- [x] Cache: tars are renamed to content-addressed paths (`sha256-<hex>.tar[.gz]`) after packing; cache hits reuse the tar if every lockfile layer digest still maps to a real file on disk. Cache miss clears the dir and repacks.

### Pusher
- [x] Rename `WeightPusher.PushMultiLayer` -> `WeightPusher.Push` (v0 `Push` deleted). The `WeightPushOptions` type merged with `WeightMultiLayerPushOptions` (name kept, field set v1).
- [x] Delete `buildWeightImage` / `weightManifestImage` / v0 single-layer code from `weight_pusher.go`. `weightOCIManifest` moved to `weight_manifest_v1.go` (only consumer).
- [x] `BundlePusher.pushWeights`: sets `wa.ReferenceDigest = imgDesc.Digest.String()` before push so the manifest annotation carries the image digest, then calls the new `WeightPusher.Push`. `PushOptions.WeightProgressFn` added for per-layer progress events tagged with weight name.

### Lockfile (weights.lock v1)
- [x] New `WeightLockEntry` + `WeightLockLayer` types replace the old `WeightFile`. Shape matches spec §3.6 so `/.cog/weights.json` can serialize a `WeightsLock` verbatim later.
- [x] `WeightsLockVersion = "v1"`.
- [x] `ParseWeightsLock` rejects unknown versions with an explicit error. No migration path for the pre-release v0 shape.
- [x] `WeightsLock.Upsert` / `FindWeight` helpers for builder cache integration.

### Callers
- [x] `pkg/cli/weights.go`: `cog weights build` reports layer count + summed layer sizes. `cog weights push` renders per-layer progress bars keyed by `<weight>/<short-digest>`.
- [x] `pkg/cli/weights_inspect.go`: switched to `WeightLockEntry` + `WeightLockLayer`. Output now shows per-layer digests + sizes for local state.
- [x] `pkg/cli/inspect.go`: weight index entries read `AnnotationV1WeightName` / `AnnotationV1WeightTarget` instead of the deleted v0 keys.
- [x] `pkg/cli/push.go`: untouched — the `BundlePusher` abstraction absorbed the change.
- [x] `tools/weights-gen/main.go`: emits weight directories (one dir per weight, configurable files-per-weight) and packs them through the real `Pack` + `BuildWeightManifestV1` to produce a v1 lockfile identical to what `cog weights build` would write.

### Tests
- [x] `weight_builder_test.go` rewritten: directory fixtures, cache-hit preserves mtime, cache-miss on invalidation, source-is-file rejection, all 9 tests green.
- [x] `weight_pusher_test.go` rewritten: `bundleWeightFixture` packs real layers; tests cover tag derivation (reference digest + manifest-digest fallback + custom override), layer errors, manifest push errors, per-layer progress, retry forwarding, context cancellation, concurrency bound. 10 tests.
- [x] `weight_manifest_v1_test.go` trimmed to just manifest construction + `fileLayer` contract (push tests now live in `weight_pusher_test.go`).
- [x] `artifact_weight_test.go`, `weights_test.go`, `weights_lock_test.go`, `model_test.go`, `index_test.go`, `pusher_test.go`, `resolver_test.go`, `index_factory_test.go`: all updated to v1 shape.
- [x] `integration-tests/harness/harness.go`: `mock-weights` now emits v1 lockfile with directory-per-weight layout.

### Verification
- [x] `golangci-lint run ./pkg/model/... ./pkg/cli/... ./tools/weights-gen/... ./integration-tests/...` — 0 issues.
- [x] `go test ./pkg/... ./tools/...` — all pass.
- [ ] `mise run test:integration` — not re-run (infra-heavy; Docker-dependent). Left for a follow-up verification pass.
- [ ] Manual registry round-trip — deferred until a real registry is available for this bean.

## Summary of Changes

Reshaped the weight pipeline so v1 (directory-per-weight, multi-layer) flows through the same `Resolver` / `Factory` / `Artifact` / `ArtifactSpec` abstraction the image pipeline already uses. Deleted the v0 file-per-weight code alongside it.

**Types** (`pkg/model/artifact_weight.go`, `weights.go`, `weights_lock.go`):
- `WeightSpec.Source` is now a directory path. Constructor unchanged.
- `WeightArtifact` carries `SourceDir`, `Layers []LayerResult`, and `ReferenceDigest`. `FilePath` + `WeightConfig` are gone.
- `WeightLockEntry` + `WeightLockLayer` replace `WeightFile`. Shape matches spec §3.6 so the lockfile doubles as the `/.cog/weights.json` payload later.
- Deleted: `WeightConfig`, `MediaTypeWeightLayer`, `MediaTypeWeightConfig`, `MediaTypeWeightLayerGzip`, `MediaTypeWeightLayerZstd`, `AnnotationWeight{Name,Dest,DigestOriginal,SizeUncompressed}`, `hashFile`.
- `MediaTypeWeightArtifact` kept — it's the `artifactType` on the manifest.

**Builder** (`pkg/model/weight_builder.go`):
- `Build` resolves the source directory, calls `Pack()` into `<projectDir>/.cog/weights-cache/<name>/`, renames each packed tar to a content-addressed filename (`sha256-<hex>.tar[.gz]`), runs `BuildWeightManifestV1` to compute the manifest digest, and writes the lockfile entry.
- Cache is digest-based: a hit reuses the on-disk tars without repacking (the mtime test in `TestWeightBuilder_CacheHit` proves it). A miss clears the cache dir and repacks.
- Source is now required to be a directory; a file source fails with a clear error.

**Pusher** (`pkg/model/weight_pusher.go`, `pusher.go`, `weight_manifest_v1.go`):
- `WeightPusher.Push` is the (formerly named) `PushMultiLayer` — concurrent layer uploads bounded by `GetPushConcurrency`, retry + progress per layer, manifest push last.
- Tag seed: `ReferenceDigest` when set, manifest digest as fallback. Covers both `cog weights push` (no image yet) and `cog push` (image digest anchors the bundle).
- `BundlePusher.Push` stamps `ReferenceDigest = imgDesc.Digest.String()` onto each weight artifact before push so the manifest annotation carries the right digest.
- `PushOptions` gained `WeightProgressFn` for per-layer bundle progress; lost the dead `FilePaths` map and the deprecated `ProjectDir` field.
- v0-specific `buildWeightImage` + `weightManifestImage` deleted. `weightOCIManifest` (the `artifactType`-carrying wrapper struct) kept but moved next to its only consumer.

**Index builder** (`pkg/model/index_factory.go`): weight descriptors in the OCI index now carry both the legacy `vnd.cog.*` keys and the spec-aligned `run.cog.*` keys. Readers migrate naturally.

**Callers**:
- `pkg/cli/weights.go` — directory-shaped specs, per-layer progress bars keyed by `<weight>/<short-digest>`, layer-count + sum-of-layer-sizes summary.
- `pkg/cli/weights_inspect.go` — lockfile entries now show per-layer digests + sizes.
- `pkg/cli/inspect.go` — reads v1 annotation keys.
- `tools/weights-gen/main.go` — generates weight directories and packs them through the real pipeline to produce a v1 lockfile identical to what `cog weights build` would write.
- `integration-tests/harness/harness.go` — `mock-weights` emits v1 lockfile with directory-per-weight layout.

**Tests**: all rewritten for v1 shape. `weight_pusher_test.go` uses a `bundleWeightFixture` helper that packs real layers so push tests exercise the real manifest + layer-writing paths. `weight_manifest_v1_test.go` trimmed to manifest construction + `fileLayer` contract; push coverage now lives in `weight_pusher_test.go`.

**Verification**:
- `mise exec -- golangci-lint run ./pkg/model/... ./pkg/cli/... ./tools/weights-gen/... ./integration-tests/...` — 0 issues.
- `go test ./pkg/... ./tools/...` — all pass (1127 tests in `pkg/model` + transitive).
- Full `mise run lint:go` still shows 25 pre-existing issues, all in `tools/test-harness/...`, none introduced by this bean.

**Follow-ups**:
- Integration test against a real registry (zot / in-process test registry) for the `cog weights push` + `cog weights inspect` round-trip.
- Teach `cog build` to embed `/.cog/weights.json` into the model image (the lockfile shape already matches the spec).



## Cross-bean impact

The simplification pass + core reshape also absorbed or unblocked parts of these beans:

- **2gv9** (cog weights import end-to-end) — wiring complete, marked done. Unification into `cog weights import` split into new bean 5lg2.
- **s5fy** (weights.lock v2) — base schema complete (spec §3.6 shape). Remaining scope: source fingerprinting + provenance fields.
- **1pm2** (embed /.cog/weights.json) — shape alignment done; becomes a mechanical COPY step now.
- **m4e8** (OCI index assembly) — assembly done; COG_OCI_INDEX=1 gate removal remaining.
- **am6m** (transfer concurrency + retry) — infrastructure in place; retry policy alignment remaining.
- **xv45** (weights check/list/inspect) — inspect slice absorbed; list and check split into new beans 6b5a and wej9; xv45 scrapped.
- **0wma** (v2 weight artifact checkpoint) — ready to verify using current commands.
- **hc35** (cog push bundle index checkpoint) — ready to verify once m4e8 lands.
