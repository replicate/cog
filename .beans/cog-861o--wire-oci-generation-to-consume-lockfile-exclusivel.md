---
# cog-861o
title: Wire OCI generation to consume lockfile exclusively
status: todo
type: task
priority: high
created_at: 2026-04-22T01:57:04Z
updated_at: 2026-04-22T01:57:04Z
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

- [ ] Rewire `BuildWeightManifestV1` to consume `WeightLockEntry`
- [ ] Rewire `BuildWeightConfigBlob` to consume `WeightLockEntry.Files`
- [ ] Audit + fix OCI index descriptor annotations to use lockfile fields
- [ ] Remove `LayerResult.Annotations` and all annotation-map construction
- [ ] Delete unused annotation constants
- [ ] Verify produced manifests/index match spec via tests
- [ ] `mise run fmt:fix` → `mise run lint` → `mise run test:go` green

## Out of scope

- Lockfile schema and generation — done in cog-s5fy
- `/.cog/weights.json` generation — cog-1pm2
- `cog weights check` — cog-wej9
