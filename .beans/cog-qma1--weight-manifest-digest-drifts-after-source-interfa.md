---
# cog-qma1
title: Weight manifest digest drifts after source interface refactor
status: completed
type: bug
priority: high
created_at: 2026-04-22T22:25:18Z
updated_at: 2026-04-23T01:48:05Z
---

## Summary

After landing cog-n2w1 (source interface + packer refactor), running `cog weights import` against the `examples/managed-weights` fixture produces a manifest digest that differs from the committed `weights.lock` — even though every layer digest, file digest, `setDigest`, and source fingerprint is byte-identical to what's in git.

- Committed digest: `sha256:450db43e78234d6fc4a23877d44e6287bf768a237685350b86b5e6f70ba20c4b`
- Refactor branch digest: `sha256:6eb7a7bed9e5b55792989f530877dba707be9ac2fdf60f64e1b017edeadf0b06`

## Evidence

During the cog-n2w1 simplify pass we verified:

1. **Unchanged inputs.** Every layer digest, file digest, setDigest, fingerprint, and set of files in the produced lockfile matches the committed one exactly.
2. **Pre-refactor code reproduces the committed digest.** Stashing the refactor, clearing `.cog/weights-cache` and `weights.lock`, and running the pre-refactor binary against `examples/managed-weights` produces `450db43e` exactly — matches git.
3. **Manifest digest is order-sensitive.** A diagnostic test confirmed `BuildWeightManifestV1(layers, meta)` produces different digests when `layers` is passed in different orders. This is the obvious lever, and it is not canonicalized today.

So the drift is caused by something in the cog-n2w1 branch that changes either:
- The order `BuildWeightManifestV1` receives layers, or
- Some byte in the manifest JSON (annotations map key ordering, config descriptor, etc.) that isn't an order issue.

## What's suspected

Most likely: layer order from `Packer.Execute` vs. `cachedLayers` (builder) paths disagree, and one of the two paths quietly changed ordering in the refactor. The `Packer.Plan` large-file iteration uses `inv.Files` (path-sorted) order; the old `walkSourceDir` used `filepath.WalkDir` lexical order. For the parakeet fixture those happen to produce the same order, so this shouldn't matter — but it's worth confirming the manifest actually gets layers in the same order now as before, with a direct byte-level comparison of the pre- and post-refactor manifest JSON.

The fact that pre-refactor reproduces `450db43e` means the suspect is unambiguously on the cog-n2w1 side.

## Context

- Bean where this was discovered: cog-n2w1 (source interface redesign + packer refactor)
- Relevant files: `pkg/model/weight_manifest_v1.go`, `pkg/model/weight_builder.go` (`cachedLayers`), `pkg/model/packer.go` (`Packer.Execute`, `Packer.Plan`)
- Related: the lockfile's own `layers` array is canonicalized to digest-sorted order via `canonicalizeEntry` in `pkg/model/weights_lock.go`.

## Root cause

Manifest layer order differs between cold and warm paths.

- **Cold path** (fresh Pack, no cache): `Packer.Plan` emits layers in pack order: `[small-bundle, large-files-in-inventory-order]`. For parakeet: `[bundle, model.safetensors, parakeet-tdt-0.6b-v3.nemo]`. Manifest digest: **450db43e**.
- **Warm path** (`cachedLayers`): reads `entry.Layers` from the lockfile, which is **digest-sorted** (via `canonicalizeEntry` in `weights_lock.go`). For parakeet: `[nemo(2c99), bundle(33e9), safetensors(65f1)]`. Manifest digest: **6eb7a7be**.

The pre-refactor reproduction in the bean cleared both `.cog/weights-cache` and `weights.lock` before running — so it took the cold path. The current refactor branch is running against an existing warm cache, so it takes the sorted path. The refactor did **not** change the manifest-build code at all; the drift is exposing the pre-existing cold/warm-cache inconsistency.

Reproduced via diagnostic test (see `pkg/model/diagnostic_qma1_test.go`):

```
# plan order [bundle, safetensors, nemo] -> sha256:450db43e... (matches committed)
# digest-sorted order [nemo, bundle, safetensors] -> sha256:6eb7a7be... (current warm)
```

Layer digests, file digests, setDigest, config blob, annotations — all byte-identical between the two runs. Only the `layers[]` array order in the manifest JSON differs.

## Fix

Canonicalize layer order inside `BuildWeightManifestV1` by sorting input layers by digest before appending. This makes manifest digest a pure function of content (layer set) and metadata, independent of the path that produced the layers. It matches the existing lockfile canonical form and means the warm digest (`6eb7a7be`) becomes the one true digest going forward.

## Todo

- [x] Confirm the hypothesis: diff the byte stream of the manifest JSON before vs. after cog-n2w1 for the parakeet fixture.
- [x] Identify which bytes differ (layer order? annotation order? config descriptor?).
- [x] Decide on canonicalization policy for manifest layer order (leading candidate: sort by digest, same as lockfile).
- [x] Apply the fix in `BuildWeightManifestV1`.
- [x] Regenerate the committed `examples/managed-weights/weights.lock` with the canonical digest.
- [x] Add a regression test (`TestBuildWeightManifestV1_InputOrderDoesNotAffectDigest`) that shuffles inputs and asserts equal digests.
- [x] Remove the diagnostic test (`pkg/model/diagnostic_qma1_test.go`).

## Summary of Changes

- `pkg/model/weight_manifest_v1.go`: `BuildWeightManifestV1` copies the input layers and sorts by digest before appending, so the manifest layer order is a pure function of the layer set plus metadata. The caller's slice is never reordered.
- `pkg/model/weight_manifest_v1_test.go`: renamed `TestBuildWeightManifestV1_MultiLayerPreservesOrder` → `_LayersCanonicallySortedByDigest` (input order is no longer preserved; that was the bug). Added `_InputOrderDoesNotAffectDigest` (permutation invariance) and `_DoesNotMutateInputSlice` (no side effect).
- `examples/managed-weights/weights.lock`: regenerated with the canonical manifest digest `sha256:6eb7a7be…`. Verified cold (cleared cache + lockfile) and warm (existing cache + lockfile) imports now both produce the same digest.

Also resolves the pre-existing cold-vs-warm-cache digest mismatch called out in the original bean's "Out of scope" note — the same canonicalization fixes both.
