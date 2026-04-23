---
# cog-410x
title: Per-layer uncompressed size annotation on weight manifests
status: completed
type: task
priority: high
created_at: 2026-04-23T23:28:19Z
updated_at: 2026-04-23T23:31:12Z
---

Add run.cog.weight.size.uncompressed annotation to each layer descriptor inside weight manifests.

## Motivation

Currently the annotation only appears on the weight-manifest descriptor in the outer OCI index (\u00a72.6). Spec \u00a72.5 forbids annotations on layer descriptors within the weight manifest itself. Hoisting uncompressed size down to each layer helps consumers make per-layer decisions (partial pulls, disk allocation, parallel extraction progress) without fetching the config blob.

## Scope

- Update spec \u00a72.5: replace 'layer descriptors carry no annotations' with a REQUIRED layer annotation: run.cog.weight.size.uncompressed.
- Code: weight_manifest_v1.go — pass Annotations in mutate.Addendum.
- Tests: flip TestBuildWeightManifestV1_LayerDescriptorsHaveNoAnnotations to assert per-layer annotation equals PackedLayer.UncompressedSize. Update the e2e test assertion similarly.
- Key: reuse AnnotationV1WeightSizeUncomp (same constant used on the index descriptor).
- Always set (REQUIRED), value = strconv.FormatInt(UncompressedSize, 10).

## Out of scope

- Index-descriptor version of this annotation (already exists, unrelated bug in cog-4lgn).

## Todo

- [ ] Update specs/weights.md \u00a72.5
- [ ] Update weight_manifest_v1.go to emit annotations
- [ ] Update unit tests
- [ ] Update e2e test
- [ ] Regenerate docs if needed
- [ ] Verify mise run lint + test:go pass


## Summary of Changes

Added `run.cog.weight.size.uncompressed` annotation to each layer descriptor inside weight manifests. Previously only present on the outer OCI index descriptor (§2.6); now also present per-layer inside the manifest (§2.5).

### Files

- `specs/weights.md` §2.5 — replaced "Layers carry no annotations" with a REQUIRED layer-descriptor annotation table
- `pkg/model/weight_manifest_v1.go` — buildWeightManifestV1 now emits the annotation via mutate.Addendum.Annotations; added validation that UncompressedSize > 0; expanded constant's doc to describe both usage sites
- `pkg/model/packer.go` — updated packedLayer doc comment
- `pkg/model/weight_manifest_v1_test.go` — inverted the old "no annotations" assertions to assert the uncompressed-size annotation appears with the correct value
- `pkg/model/weight_pipeline_e2e_test.go` — same inversion end-to-end through push/pull

### Verification

- `mise run test:go` — passes
- `mise run lint:go` — 0 issues
