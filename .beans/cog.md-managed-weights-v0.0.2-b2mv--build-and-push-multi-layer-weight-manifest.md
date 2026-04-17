---
# cog.md-managed-weights-v0.0.2-b2mv
title: Build and push multi-layer weight manifest
status: todo
type: task
priority: high
created_at: 2026-04-17T19:26:51Z
updated_at: 2026-04-17T19:38:16Z
parent: cog.md-managed-weights-v0.0.2-9qcd
blocked_by:
    - cog.md-managed-weights-v0.0.2-0iel
---

Assemble tar layers into an OCI manifest and push to a registry.

Given the output of the tar packing engine (tar files + metadata):
- Build OCI image via go-containerregistry (mutate.AppendLayers on empty.Image)
- Set artifactType to application/vnd.cog.weight.v1
- Set standard OCI layer media types (vnd.oci.image.layer.v1.tar / .tar+gzip)
- Apply layer annotations: run.cog.weight.content, .file, .size.uncompressed
- Apply manifest annotations: run.cog.weight.name, .target, .reference.type, .reference.digest
- Push layers via existing multipart OCI push (registry.WriteLayer)
- Push manifest via registry.PushImage

The existing WeightPusher (pkg/model/weight_pusher.go) handles single-file weights with a custom weightManifestImage wrapper for artifactType support. This task extends it to multi-layer manifests.

## Data flow (no Docker daemon)

The weight push path bypasses Docker entirely. The v0 flow already works this way:

1. tarball.LayerFromFile(tarPath) -> lazy v1.Layer (reads from disk on demand)
2. mutate.AppendLayers(empty.Image, layers...) -> v1.Image
3. Wrap with weightManifestImage (injects artifactType via custom RawManifest())
4. WriteLayer() per layer -> custom HTTP multipart upload to registry
5. PushImage() via remote.Write() -> HTTP PUT manifest

For v2 this is the same flow with N layers instead of 1. The existing weightManifestImage wrapper already overrides RawManifest() to serialize a custom struct including artifactType -- this works regardless of layer count since it operates at the manifest level.

Key files: pkg/model/weight_pusher.go (weightManifestImage, buildWeightImage, Push), pkg/registry/registry_client.go (WriteLayer with multipart chunking).

Check whether go-containerregistry has added native artifactType support since v0 -- if so, the wrapper can be simplified.

Key: the pushed manifest must be consumable by standard OCI tooling (crane pull, docker pull). Verify with crane manifest after push.

Reference: specs/weights.md §2, existing code in pkg/model/weight_pusher.go, pkg/registry/
