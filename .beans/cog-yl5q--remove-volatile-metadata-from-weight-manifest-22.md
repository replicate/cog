---
# cog-yl5q
title: Remove volatile metadata from weight manifest (§2.2)
status: completed
type: task
priority: normal
created_at: 2026-04-21T00:00:59Z
updated_at: 2026-04-21T00:23:37Z
parent: cog-9qcd
---

The spec says weight manifests contain no timestamps, source URIs, or producer metadata — identical inputs must produce identical manifest digests. Current implementation violates this.

## Todo

- [x] Remove `org.opencontainers.image.created` annotation from weight manifest (`weight_manifest_v1.go`)
- [x] Remove `run.cog.reference.type` annotation (not in spec)
- [x] Remove `run.cog.reference.digest` annotation (not in spec)
- [x] Remove `Created` field from `WeightManifestV1Metadata` struct
- [x] Verify determinism: same source files + same packing params → identical manifest digest
- [x] Update `index_factory.go` to stop copying reference annotations to index descriptors
- [x] Update tests that assert on these annotations

## Context

Spec §2.2: "The manifest contains no timestamps, source URIs, or producer version metadata. This makes the manifest a pure function of the weight content (files), the packing strategy (layers), and the cog.yaml config (name, target)."


## Summary of Changes

Removed all volatile metadata from weight manifests per §2.2: `org.opencontainers.image.created`, `run.cog.reference.type`, `run.cog.reference.digest` annotations, and the `Created`/`ReferenceDigest` fields from `WeightManifestV1Metadata`. Updated `index_factory.go` to use `run.cog.weight.set-digest` instead of reference annotations on weight descriptors. Updated `resolver.go` with backward-compatible weight descriptor detection (`isWeightDescriptor`) that checks for the new set-digest annotation or the legacy reference-type.
