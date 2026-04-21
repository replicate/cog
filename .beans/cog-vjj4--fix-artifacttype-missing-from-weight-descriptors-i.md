---
# cog-vjj4
title: Fix artifactType missing from weight descriptors in OCI index
status: completed
type: bug
priority: normal
created_at: 2026-04-21T22:10:36Z
updated_at: 2026-04-21T23:15:38Z
---

go-containerregistry's mutate.computeDescriptor doesn't copy ArtifactType from IndexAddendum.Descriptor, and our descriptorAppendable doesn't implement the withArtifactType interface. Result: ArtifactType is silently dropped from weight descriptors in the serialized index, even though we set it in index_factory.go:87. Fix: add ArtifactType() method to descriptorAppendable.

## Todo
- [x] Fix artifactType dropped from index descriptors (descriptorAppendable missing ArtifactType method)
- [x] Hoist run.cog.weight.target to index descriptor annotations
- [x] Hoist run.cog.weight.size.uncompressed (total) to index descriptor annotations
- [x] Update tests to assert all hoisted fields
- [x] Update spec \u00a72.2 example to show per-layer annotations (content, file, size.uncompressed)
- [x] Update spec \u00a72.5 to document layer descriptor annotations and index descriptor annotations separately
- [x] Update spec \u00a72.6 example and add index descriptor annotation table with size.uncompressed
- [x] Document why target is omitted from index and clarify descriptor size vs weight size
- [x] Remove target from index (not needed for scanning/scheduling)

## Summary of Changes\n\n**Root cause:** `go-containerregistry`'s `mutate.computeDescriptor` builds the final descriptor by calling `partial.Descriptor(ia.Add)` on the appendable, then selectively overriding fields from `IndexAddendum.Descriptor`. However, `ArtifactType` is not in the override list. And our `descriptorAppendable` type only implemented `MediaType()`, `Digest()`, and `Size()` — not the `withArtifactType` interface that `partial.Descriptor` checks. So the `ArtifactType` we set on the addendum descriptor was silently dropped.\n\n**Fix:**\n1. Added `ArtifactType()` method to `descriptorAppendable` so `partial.Descriptor` picks it up\n2. Set `ArtifactType` on the descriptor passed to `descriptorAppendable` (not just on the addendum)\n3. Added test assertion for `ArtifactType` on weight descriptors in the index\n\n**Files changed:**\n- `pkg/model/index_factory.go` — fix + new method\n- `pkg/model/index_factory_test.go` — regression test

- [ ] Remove content/file annotations from packer code
- [ ] Update weight_builder.go to not depend on content/file annotations
- [ ] Update all affected tests
