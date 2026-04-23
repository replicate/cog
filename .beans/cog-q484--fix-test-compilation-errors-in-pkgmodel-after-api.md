---
# cog-q484
title: Fix test compilation errors in pkg/model/ after API refactor
status: in-progress
type: task
priority: critical
created_at: 2026-04-23T03:06:30Z
updated_at: 2026-04-23T03:07:26Z
---

Update all test files in pkg/model/ to match renamed types and changed function signatures: LayerResult→PackedLayer, WeightManifestV1Metadata removed, NewWeightArtifact/NewWeightLockEntry/BuildWeightConfigBlob/ComputeWeightSetDigest signature changes.

## Todo\n- [ ] Fix weight_manifest_v1_test.go (defaultMeta→defaultEntry, BuildWeightManifestV1 calls)\n- [ ] Fix artifact_weight_test.go (NewWeightArtifact calls, field access)\n- [ ] Fix model_test.go (NewWeightArtifact calls)\n- [ ] Fix weights_test.go (ComputeWeightSetDigest, BuildWeightConfigBlob, NewWeightLockEntry)\n- [ ] Fix weight_pusher_test.go (newTestWeightArtifact helper, BuildWeightManifestV1, field access)\n- [ ] Fix pusher_test.go (bundleWeightFixture helper)\n- [ ] Fix weight_pipeline_e2e_test.go (full pipeline)\n- [ ] Fix index_test.go (NewWeightArtifact call)\n- [ ] Fix weight_builder_test.go (wa.Target, wa.SetDigest, wa.ConfigBlob)\n- [ ] Verify with go vet ./pkg/model/...
