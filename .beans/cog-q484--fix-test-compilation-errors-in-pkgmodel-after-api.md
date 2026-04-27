---
# cog-q484
title: Fix test compilation errors in pkg/model/ after API refactor
status: completed
type: task
priority: critical
created_at: 2026-04-23T03:06:30Z
updated_at: 2026-04-24T15:39:32Z
---

Update all test files in pkg/model/ to match renamed types and changed function signatures: LayerResult→PackedLayer, WeightManifestV1Metadata removed, NewWeightArtifact/NewWeightLockEntry/BuildWeightConfigBlob/ComputeWeightSetDigest signature changes.

## Todo\n- [x] Fix weight_manifest_v1_test.go (defaultMeta→defaultEntry, BuildWeightManifestV1 calls)\n- [x] Fix artifact_weight_test.go (NewWeightArtifact calls, field access)\n- [x] Fix model_test.go (NewWeightArtifact calls)\n- [x] Fix weights_test.go (ComputeWeightSetDigest, BuildWeightConfigBlob, NewWeightLockEntry)\n- [x] Fix weight_pusher_test.go (newTestWeightArtifact helper, BuildWeightManifestV1, field access)\n- [x] Fix pusher_test.go (bundleWeightFixture helper)\n- [x] Fix weight_pipeline_e2e_test.go (full pipeline)\n- [x] Fix index_test.go (NewWeightArtifact call)\n- [x] Fix weight_builder_test.go (wa.Target, wa.SetDigest, wa.ConfigBlob)\n- [x] Verify with go vet ./pkg/model/...\n\n## Summary of Changes\n\nAll test files in pkg/model/ were updated to match the refactored API. Verified clean with go vet ./pkg/model/...
