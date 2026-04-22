---
# cog-qma1
title: Weight manifest digest drifts after source interface refactor
status: todo
type: bug
priority: high
created_at: 2026-04-22T22:25:18Z
updated_at: 2026-04-22T22:25:18Z
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

## Out of scope

This bug existed (or was going to exist) independent of cog-n2w1 in a different form: the `cachedLayers` path reads layers in lockfile-canonical (digest-sorted) order, while a fresh `Pack` emits plan order. Those two paths never agreed, and would produce different manifest digests for the same content depending on whether the cache was warm. Canonicalizing manifest layer order (e.g. sort by digest before building) is a natural fix for both the refactor drift **and** the pre-existing warm/cold-cache inconsistency. But: decide that in this bean, not cog-n2w1.

## Todo

- [ ] Confirm the hypothesis: diff the byte stream of the manifest JSON before vs. after cog-n2w1 for the parakeet fixture.
- [ ] Identify which bytes differ (layer order? annotation order? config descriptor?).
- [ ] Decide on canonicalization policy for manifest layer order (leading candidate: sort by digest, same as lockfile).
- [ ] Apply the fix and verify the committed `examples/managed-weights/weights.lock` reproduces exactly.
- [ ] Add a regression test that locks in the canonical form (e.g. `TestBuildWeightManifestV1_CanonicalLayerOrder` shuffling inputs and asserting equal digests).
- [ ] Consider whether the cold-vs-warm-cache digest-mismatch bug is worth a separate bean or lives here.

## Context

- Bean where this was discovered: cog-n2w1 (source interface redesign + packer refactor)
- Relevant files: `pkg/model/weight_manifest_v1.go`, `pkg/model/weight_builder.go` (`cachedLayers`), `pkg/model/packer.go` (`Packer.Execute`, `Packer.Plan`)
- Related: the lockfile's own `layers` array is canonicalized to digest-sorted order via `canonicalizeEntry` in `pkg/model/weights_lock.go`.
