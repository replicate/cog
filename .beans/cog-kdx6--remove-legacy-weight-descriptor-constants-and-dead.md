---
# cog-kdx6
title: Remove legacy weight descriptor constants and dead code
status: todo
type: task
priority: high
created_at: 2026-04-23T04:01:40Z
updated_at: 2026-04-23T04:01:40Z
parent: cog-66gt
---

Remove the pre-v1 weight descriptor backward compat that nobody needs:

- Delete `AnnotationV1ReferenceType`, `AnnotationV1ReferenceDigest`, `ReferenceTypeWeights` constants from `weight_manifest_v1.go`
- Remove the legacy fallback branch in `isWeightDescriptor` (`resolver.go:460`) — the v1 `AnnotationV1WeightSetDigest` check is sufficient
- Delete `findWeightsManifest` (`resolver.go:437-446`) — dead code, only called from tests
- Delete the test for `findWeightsManifest` (`resolver_test.go:1222-1246`)
- Delete the test assertions for absent reference annotations (`weight_manifest_v1_test.go:123-126`)
- Update the `resolver_test.go` mock that constructs a legacy descriptor with `AnnotationV1ReferenceType` (`resolver_test.go:1230`)

Small, mechanical cleanup. No behavioral change for any format produced in the last 6 months.
