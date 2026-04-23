---
# cog-cvai
title: Remove weights-gen and unexport dead symbols
status: completed
type: task
priority: normal
created_at: 2026-04-23T04:54:30Z
updated_at: 2026-04-23T14:08:46Z
---

Remove tools/weights-gen (matches PR #2959), then unexport ~22 symbols in pkg/model/ and pkg/model/weightsource/ that were only exported for that tool.

## Plan
- [x] Delete tools/weights-gen/ and .gitignore entry
- [x] Unexport packer.go: defaultBundleFileMax, defaultBundleSizeMax, mediaTypeOCILayerTar, mediaTypeOCILayerTarGzip, packOptions, packedLayer, packResult, packedFile, plan, layerPlan, packer, newPacker, packer.planLayers, packer.execute, packer.pack
- [x] Unexport artifact_weight.go: buildWeightArtifact
- [x] Unexport weights.go: newWeightLockEntry
- [x] Unexport weights_lock.go: weightsLockVersion
- [x] Delete weightsource/fingerprint.go: ParseFingerprint; unexport Fingerprint.value, Fingerprint.isZero
- [x] Unexport weightsource/file.go: FileSource.sourceDir (renamed from Dir to avoid field collision)
- [x] Lint passes (0 issues), tests pass

## Summary of Changes
Removed tools/weights-gen/ (matching PR #2959) and unexported 22 symbols in pkg/model/ and pkg/model/weightsource/ that were only exported for the tool. Deleted ParseFingerprint (zero production callers). All test files updated. Build, lint, and tests pass.



## Simplify Review Fixes
Code review caught additional issues:
- Unexported BuildWeightManifestV1, NewWeightArtifact, BuildWeightConfigBlob, ComputeWeightSetDigest (all had no external callers but were left exported with unexported parameter types)
- Fixed ~15 stale comments still referencing old uppercase names (Plan, Execute, Pack, PackedFile, etc.)
- Fixed test function names broken by replaceAll (Go requires uppercase after 'Test')
