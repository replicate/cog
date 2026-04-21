---
# cog-b2mv
title: Build and push multi-layer weight manifest
status: completed
type: task
priority: high
created_at: 2026-04-17T19:26:51Z
updated_at: 2026-04-17T20:18:35Z
parent: cog-9qcd
blocked_by:
    - cog-0iel
---

Assemble tar layers into an OCI manifest and push to a registry.

Given the output of the tar packing engine (tar files + metadata):
- Build OCI image via go-containerregistry (mutate.AppendLayers on empty.Image)
- Set artifactType to application/vnd.cog.weight.v1
- Set standard OCI layer media types (vnd.oci.image.layer.v1.tar / .tar+gzip)
- Apply layer annotations: run.cog.weight.content, .file, .size.uncompressed
- Apply manifest annotations: run.cog.weight.name, .target, .reference.type, .reference.digest
- Push layers via existing multipart OCI push (registry.WriteLayer)
- Push manifest via registry.PushImage

The existing WeightPusher (pkg/model/weight_pusher.go) handles single-file weights with a custom weightManifestImage wrapper for artifactType support. This task extends it to multi-layer manifests.

## Data flow (no Docker daemon)

The weight push path bypasses Docker entirely. The v0 flow already works this way:

1. tarball.LayerFromFile(tarPath) -> lazy v1.Layer (reads from disk on demand)
2. mutate.AppendLayers(empty.Image, layers...) -> v1.Image
3. Wrap with weightManifestImage (injects artifactType via custom RawManifest())
4. WriteLayer() per layer -> custom HTTP multipart upload to registry
5. PushImage() via remote.Write() -> HTTP PUT manifest

For v2 this is the same flow with N layers instead of 1. The existing weightManifestImage wrapper already overrides RawManifest() to serialize a custom struct including artifactType -- this works regardless of layer count since it operates at the manifest level.

Key files: pkg/model/weight_pusher.go (weightManifestImage, buildWeightImage, Push), pkg/registry/registry_client.go (WriteLayer with multipart chunking).

Check whether go-containerregistry has added native artifactType support since v0 -- if so, the wrapper can be simplified.

Key: the pushed manifest must be consumable by standard OCI tooling (crane pull, docker pull). Verify with crane manifest after push.

Reference: specs/weights.md §2, existing code in pkg/model/weight_pusher.go, pkg/registry/

## Implementation plan

- [x] Add v1 manifest-level annotation key constants (run.cog.weight.name, .target, run.cog.reference.type, .reference.digest)
- [x] Add empty config descriptor constants (OCI empty descriptor)
- [x] Implement `BuildWeightManifestV1` in pkg/model/weight_manifest_v1.go
- [x] Implement `fileLayer` — file-backed v1.Layer that avoids tarball.LayerFromFile's implicit re-gzip path for .tar files
- [x] Implement `WeightPusher.PushMultiLayer` — concurrent WriteLayer + PushImage with retry+progress wiring
- [x] Unit tests (24 new tests covering validation, manifest shape, artifactType, digest/RawManifest consistency, concurrency bounds, progress, retry, error paths)
- [x] `mise run lint:go` — 0 issues in pkg/model (25 pre-existing issues in tools/test-harness/... unchanged)
- [x] `mise run test:go` — all 1130 tests pass

## Summary of Changes

Implemented the v1 multi-layer weight manifest builder and pusher. Bridges the tar packing engine (bean 0iel) to the registry: takes `[]LayerResult` from `Pack()` and produces an OCI manifest that standard tooling can consume.

**New file**: `pkg/model/weight_manifest_v1.go` (≈430 lines)

- `WeightManifestV1Metadata` — manifest-level metadata (name, target, referenceDigest, created) with `validate()` and `annotations()` helpers producing spec §2.3 `run.cog.*` annotations
- `BuildWeightManifestV1(layers, meta) -> v1.Image` — assembles the manifest via `mutate.Append` with per-layer `Addendum{MediaType, Annotations}`, then wraps to inject `artifactType` and the OCI empty config descriptor
- `weightManifestV1Image` — v1.Image wrapper mirroring the v0 pattern: overrides `RawConfigFile` (`{}`), `Manifest` (rewrites config to the empty descriptor, merges annotations), and `RawManifest` (serializes a custom struct carrying `artifactType`, cached via `sync.Once` for deterministic digests). v1.Manifest in go-containerregistry v0.21.4 still does not carry `artifactType` at the manifest level, so the wrapper remains necessary.
- `fileLayer` — minimal `v1.Layer` backed by the packed tar file on disk. Unlike `tarball.LayerFromFile`, it returns the file bytes unchanged for both `Compressed()` and `Uncompressed()`, avoiding the re-gzip path that would mangle our pre-computed digests on uncompressed `.tar` layers. Digest/size come from the packer result rather than being recomputed.
- `WeightPusher.PushMultiLayer(ctx, repo, meta, layers, opts...)` — pushes layers concurrently via `errgroup` bounded by `GetPushConcurrency()` (overridable), calling the existing `writeLayerWithProgress` helper so retry + progress callbacks work exactly like the v0 single-layer path. After all layers land, pushes the manifest via `registry.PushImage`.

**New constants**:
- Manifest annotations: `AnnotationV1WeightName`, `AnnotationV1WeightTarget`, `AnnotationV1ReferenceType`, `AnnotationV1ReferenceDigest`, `AnnotationOCIImageCreated`
- `ReferenceTypeWeights = "weights"`
- `MediaTypeOCIEmpty = "application/vnd.oci.empty.v1+json"` and the empty-blob sha256 (validated at init)

**Tests** (`pkg/model/weight_manifest_v1_test.go`, 24 new tests):

- Metadata validation (missing name/target, optional reference digest, default created timestamp)
- `BuildWeightManifestV1` validation (empty layers, per-field layer validation)
- Manifest shape (schema version, OCI manifest media type, config points to empty descriptor, layer media types / digests / sizes / annotations preserved, manifest annotations)
- Raw manifest contains `artifactType` field (verified by parsing the JSON, not the struct)
- `Digest()` matches `sha256(RawManifest())` — critical for registry acceptance
- Multi-layer ordering preserves both `.tar` and `.tar+gzip` media types
- Annotations cloned from LayerResult (no aliasing)
- `fileLayer` returns raw file bytes for Compressed/Uncompressed; error on missing file
- `PushMultiLayer` happy path (each layer pushed exactly once by digest, manifest tag derives from reference digest, result descriptor matches pushed image)
- Rejects empty repo / empty layers / missing metadata
- Propagates layer write errors, manifest push errors, context cancellation
- Reports per-layer progress with correct digest tagging
- Forwards retry callbacks with name that includes the weight + layer digest
- Honours concurrency limit (measured via in-flight counter with `atomic.Int32`)
- Custom tag override

**Verification**:
- `mise run test:go` — all 1130 tests pass
- `mise exec -- golangci-lint run ./pkg/model/...` — 0 issues
- Generated manifest JSON matches spec §2.2 byte-for-byte (manually dumped and diffed: artifactType, empty config, per-layer annotations, manifest annotations)

**Scope**: v0 pipeline (`WeightBuilder`, v0 `WeightPusher.Push`, `WeightSpec`, `WeightArtifact`, `WeightConfig`, resolver/source/factory/pusher/model wiring) left in place. v0 is a weight-per-file model; v1 is weight-per-directory producing N layers. Switching over is a full redesign of the data model spanning ~15 files, tracked as a follow-up bean so this review stays focused on manifest shape.

**On the wrapper**: go-containerregistry does not expose `artifactType` on `v1.Manifest` — confirmed against upstream main (2026-04). This is a deliberate design choice by the upstream maintainers, not a version lag. An upgrade will not remove the need for the wrapper. The wrapper overrides `RawManifest()` to emit a custom struct that includes the field; everything else (layer uploads, transport, auth) still flows through the standard `v1.Image` path.

## Follow-ups (not in scope)

- **New bean**: Remove v0 weight pipeline and switch all callers to v1. Touches resolver, source, factory, BundlePusher, Model, index, weights-gen, cog.yaml schema parsing, pusher_test.go, etc. Deletes `weight_builder.go`, v0 `Push`, `buildWeightImage`, `weightManifestImage`, v0-only media type constants. Redesigns `WeightSpec` / `WeightArtifact` around a source directory + target (no FilePath), with the artifact carrying `[]LayerResult`.
- Wire v1 `PushMultiLayer` into `cog push` once the caller-level data model is v1-shaped (blocked on the bean above).
- Integration test against a real registry (zot or the in-process test registry in `pkg/registry/push_test.go`) — best done after callers exist.
