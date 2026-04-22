---
# cog-vs3r
title: 'weights.lock v2: add per-layer ContentsDigest'
status: scrapped
type: task
priority: high
created_at: 2026-04-22T20:21:17Z
updated_at: 2026-04-22T20:37:35Z
parent: cog-66gt
---

Bump `WeightsLockVersion` from 1 to 2 and add a per-layer `ContentsDigest` field so the lockfile captures the content identity of each layer's file set independent of tar packing.

## Why

Three places need a single hash that identifies "the set of files that belong to this layer":

1. `cog weights pull` reconstructing a layer from source needs to verify inputs (file contents) separately from outputs (tar digest) to give useful drift errors.
2. The local WeightStore cache lookup needs a single-compare "do I have this layer's contents" check instead of N file-existence stats.
3. Cross-producer integrity: two producers packing the same files must agree on a stable identifier even if tar wrapping drifts.

Same construction as spec §2.4 weight set digest, scoped to a layer's member files:

```
contentsDigest = sha256(join(sort(entries), "\n"))
# entries: "<hex-sha256>  <path>" for each file assigned to this layer
```

## Scope

- Add `ContentsDigest string` to `WeightLockLayer` in `pkg/model/weights_lock.go`.
- Bump `WeightsLockVersion` from 1 to 2.
- Auto-migrate v1 lockfiles on load: compute `ContentsDigest` from `Files[]` and write on next save. Field is derivable, so migration is always safe.
- Compute `ContentsDigest` in the packer alongside existing digests (zero extra I/O; we already have file digests in `PackedFile`).
- Update canonicalization (`canonicalizeEntry`) and equality checks (`lockEntriesContentEqual`) to include the new field.
- Update all tests and golden files.

## Out of scope

- Changes to `specs/weights.md`. `ContentsDigest` is derivable from the OCI config blob by any consumer; cog stores it as producer-side bookkeeping only.
- Pending state file (separate bean).

## Todo

- [ ] Add `ContentsDigest` field to `WeightLockLayer`
- [ ] Bump `WeightsLockVersion` to 2
- [ ] Implement v1→v2 auto-migration on load
- [ ] Compute `ContentsDigest` in packer output
- [ ] Update `canonicalizeEntry` to preserve field
- [ ] Update `lockEntriesContentEqual` to compare field
- [ ] Update `BuildWeightManifestV1` / config blob path if it references layer fields
- [ ] Update tests: lockfile round-trip, migration from v1, packer output, equality
- [ ] Update `tools/weights-gen/main.go` if it constructs lockfiles directly

## Reference

- `plans/2026-04-22-managed-weights-import-and-local-run-design.md` §2
- `specs/weights.md` §2.4 (set digest construction, same algorithm)



## Update 2026-04-22 (scrapping ContentsDigest)

ContentsDigest is no longer pulling its weight after the WeightStore was redesigned to store files individually (not layers as directories). Rationale:

- **HasLayer cache check**: answered by iterating `lockfile.Files[]` filtered by layer and stat'ing each file under `files/sha256/<digest>`. No per-layer hash needed.
- **Source-drift detection**: caught earlier and at finer granularity by `PutFile`, which rejects any file whose streamed bytes don't match the inventory's expected digest.
- **Packer drift detection**: caught by the tar blob digest at push time (compare the streamed tar's sha256 to `lockfile.Layers[].Digest`).
- **Cross-producer integrity**: philosophical; not used by any code. Layers are producer-specific packing choices anyway.

ContentsDigest was redundant with three existing mechanisms. Dropping it.

### Revised scope

- No lockfile version bump. `WeightsLockVersion` stays at 1.
- No `ContentsDigest` field on `WeightLockLayer`.
- No migration logic.
- No packer changes (packer already records per-file digests in `PackedFile`).

### What might still be useful

If a callsite ever wants "the hash that uniquely identifies this layer's file set independent of tar wrapping," we can compute it from `lockfile.Files[]` on demand (helper function, no stored value). Zero call sites today.

### This bean's remaining scope

With ContentsDigest dropped, this bean is effectively empty — no schema change is needed. Closing as scrapped; any truly necessary lockfile work will surface from implementing the other beans (likely none, since v1 lockfiles are already sufficient).
