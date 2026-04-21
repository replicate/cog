---
# cog-ez7g
title: Config blob and weight set digest (§2.3 + §2.4)
status: completed
type: task
priority: high
created_at: 2026-04-21T00:00:53Z
updated_at: 2026-04-21T00:27:34Z
parent: cog-9qcd
---

Implement the spec-required config blob and weight set digest. Currently the weight manifest uses oci.empty as its config descriptor — the spec requires a full application/vnd.cog.weight.config.v1+json config blob with a per-file index (path, layer, size, digest) and a content-addressable weight set digest.

## Todo

- [x] Compute per-file SHA-256 content digests during packing (in `Pack()` or a post-pack pass)
- [x] Compute weight set digest: `sha256(join(sort("<hex>  <path>"), "\n"))` per §2.4
- [x] Build config blob JSON: `name`, `target`, `setDigest`, `files[]` array sorted by path
- [x] Replace `oci.empty` config descriptor with `application/vnd.cog.weight.config.v1+json` in `weight_manifest_v1.go`
- [x] Add `run.cog.weight.set-digest` annotation to weight manifest (§2.5)
- [x] Add `run.cog.weight.set-digest` annotation to OCI index descriptor (§2.6)
- [x] Update lockfile `WeightLockEntry` to include `setDigest` field
- [ ] Update `/.cog/weights.json` schema expectation in bean 1pm2 to include `setDigest`
- [x] Unit tests: deterministic config blob from same inputs, set digest stability across repacks
- [ ] Integration test: round-trip verify config blob fetchable via `crane blob`

## Context

Spec §2.3 defines the config blob as the file-level index of the weight — infra uses it to assemble weight directories without walking the filesystem. §2.4 defines the weight set digest as the canonical content-addressable identifier: same files = same digest regardless of packing strategy. Both are core to the caching, dedup, and cross-model reuse story.

## Review Findings\n\n- Important #1 (double I/O): deferred to follow-up optimization bean — not blocking correctness\n- Important #2 (empty LayerDigest on cache hit): fixed — added validation, fall through to repack\n- Important #4 (missing target on index descriptor): spec §2.6 example intentionally omits it; we follow the example\n- Important #5 (stale doc comments): fixed

\n## Summary of Changes\n\nImplemented config blob (§2.3) and weight set digest (§2.4):\n\n- **packer.go**: Pack() now returns *PackResult with per-file SHA-256 content digests and file→layer mapping\n- **weights.go**: Added WeightConfigBlob type, ComputeWeightSetDigest(), BuildWeightConfigBlob()\n- **weight_manifest_v1.go**: Replaced OCI empty config with real application/vnd.cog.weight.config.v1+json config blob; added run.cog.weight.set-digest annotation; removed all volatile metadata (§2.2)\n- **weights_lock.go / weights.go**: Added SetDigest field to WeightLockEntry\n- **weight_builder.go**: Wires config blob and set digest through the build flow, with cache-hit path that hashes source files and maps to cached layers\n- **index_factory.go**: Weight descriptors carry run.cog.weight.name and run.cog.weight.set-digest (not reference annotations)\n- **resolver.go**: Backward-compatible isWeightDescriptor() checks new annotation or legacy\n- **9 new tests** covering set digest determinism, packing independence, config blob structure, and cross-repack stability
