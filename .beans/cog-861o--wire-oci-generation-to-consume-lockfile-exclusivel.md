---
# cog-861o
title: Wire OCI generation to consume lockfile exclusively
status: in-progress
type: task
priority: high
created_at: 2026-04-22T01:57:04Z
updated_at: 2026-04-23T03:31:46Z
parent: cog-66gt
blocked_by:
    - cog-s5fy
---

Rewire all downstream OCI generation (weight manifest, config blob, OCI index) to read from the lockfile's typed fields rather than from `LayerResult`/`PackedFile` or other intermediate state.

After s5fy, the lockfile carries everything needed to produce the spec-defined OCI artifacts. This bean closes the loop: nothing downstream reads the packer output directly. The lockfile is the single source that feeds manifest, config blob, and index assembly.

Reference: `specs/weights.md` §2.2 (manifest), §2.3 (config blob), §2.5 (no layer annotations), §2.6 (index descriptor annotations).

## What changes

**Weight manifest builder (`BuildWeightManifestV1`)**
- Takes `WeightLockEntry` (or relevant subset) instead of `[]LayerResult` + metadata struct
- Layer descriptors: `digest`, `mediaType`, `size` only — no annotations (spec §2.5)
- Manifest annotations: `name`, `target`, `setDigest` from entry fields
- Config blob descriptor: unchanged (already built separately)

**Config blob builder (`BuildWeightConfigBlob`)**
- Takes `WeightLockEntry.Files` instead of `[]PackedFile`
- The file index in the lockfile is already the §2.3 shape: `path`, `layer`, `size`, `digest`

**OCI index assembly**
- Index descriptor annotations per spec §2.6: `run.cog.weight.name` from `entry.Name`, `run.cog.weight.set-digest` from `entry.SetDigest`, `run.cog.weight.size.uncompressed` from `entry.Size`
- Audit existing index code to ensure it reads from lockfile fields, not from stale sources

**Dead code removal**
- `LayerResult.Annotations` map — no producers, no consumers after this bean
- Annotation constants (`AnnotationV1WeightContent`, `AnnotationV1WeightFile`, `AnnotationV1WeightSizeUncomp`) — deleted if nothing references them
- Any intermediate annotation-map construction in the packer that exists only to feed the manifest builder

## Tasks

- [x] Rename `LayerResult` -> `PackedLayer`
- [x] Rewire `BuildWeightConfigBlob` + `ComputeWeightSetDigest` to take `[]WeightLockFile`
- [x] Rewire `BuildWeightManifestV1` to take `WeightLockEntry` + `[]PackedLayer`; remove `WeightManifestV1Metadata`
- [x] `WeightArtifact` carries `Entry WeightLockEntry`; drop separate SetDigest/ConfigBlob
- [x] Rewire `BuildWeightConfigBlob` to consume `WeightLockEntry.Files`
- [x] Audit + fix OCI index descriptor annotations to use lockfile fields
- [x] Remove `LayerResult.Annotations` and all annotation-map construction (done in s5fy)
- [x] Delete unused annotation constants (done in s5fy)
- [x] Verify produced manifests/index match spec via tests
- [x] `mise run fmt:fix` → `mise run lint` → `mise run test:go` green

## Out of scope

- Lockfile schema and generation — done in cog-s5fy
- `/.cog/weights.json` generation — cog-1pm2
- `cog weights check` — cog-wej9

## Summary of Changes

Rewired all downstream OCI generation to consume the lockfile exclusively. The lockfile entry is now the single source of truth — nothing downstream reads packer output directly for metadata.

**Core API changes:**
- `LayerResult` renamed to `PackedLayer` for clarity
- `BuildWeightManifestV1(entry WeightLockEntry, layers []PackedLayer)` — entry provides all metadata (name, target, setDigest, file index); layers provide tar paths for push streaming. Config blob built internally from entry.Files. `WeightManifestV1Metadata` removed.
- `BuildWeightConfigBlob(name, target, setDigest string, files []WeightLockFile)` — takes lockfile types directly, setDigest pre-computed by lockfile. Returns `([]byte, error)` — no redundant setDigest recomputation.
- `ComputeWeightSetDigest([]WeightLockFile)` — uses lockfile file type
- `NewWeightLockEntry` computes setDigest internally from file index; manifestDigest filled in by caller after manifest assembly
- `WeightArtifact` carries `Entry WeightLockEntry` — replaces separate Target/SetDigest/ConfigBlob fields. `Name()` and `TotalSize()` read from entry.

**Pipeline change:**
- Builder cache-hit path uses lockfile entry directly (no `packedFilesFromLockEntry` bridge)
- Builder fresh-pack path: packer → lockfile entry → manifest (all derived from entry)
- Pusher reads `artifact.Entry` for metadata
- Index assembly reads `entry.Size` for uncompressed size annotation

**Removed:**
- `WeightManifestV1Metadata` struct
- `packedFilesFromLockEntry` bridge function

**Relocated:**
- `AnnotationV1WeightSizeUncomp` moved from packer.go to weight_manifest_v1.go with other annotation constants

Verification: fmt ✓, lint ✓, 1293 tests pass (5 pre-existing skips).
