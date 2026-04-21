---
# cog-hc35
title: cog push produces valid bundle index
status: completed
type: milestone
priority: critical
created_at: 2026-04-17T19:34:37Z
updated_at: 2026-04-21T19:39:08Z
parent: cog-9qcd
blocked_by:
    - cog-m4e8
    - cog-sspy
---

Validation checkpoint. At this point you should be able to:

1. Have a model with managed weights (weights imported, lockfile present)
2. Run: cog build (produces model image)
3. Run: cog push (produces OCI index)
4. Run: crane manifest <registry>/<model>:latest and see an OCI image index with:
   - Model image manifest (linux/amd64 platform)
   - Weight manifest descriptor(s) (unknown/unknown platform)
   - Weight descriptors carrying run.cog.weight.* annotations
   - Weight manifest digests matching weights.lock
5. Run: crane manifest <registry>/<model>:latest --platform unknown/unknown and resolve to the weight manifest

This is the point where the full push pipeline works end-to-end: import weights -> build image -> push bundle.

## Verification

```bash
# Full pipeline
cog weights import z-image-turbo
cog build
cog push

# Verify index structure
crane manifest <registry>/<model>:latest | jq '.manifests[] | {mediaType, platform, annotations}'

# Verify weight manifest is reachable from index
crane manifest <registry>/<model>:latest --platform unknown/unknown | jq .artifactType
# Should output: application/vnd.cog.weight.v1
```



## Required tasks
- [x] OCI index assembly (m4e8) -- remove COG_OCI_INDEX gate
- [x] Prepare example managed-weights model and push to registry (sspy)

## Verification

```bash
cd examples/managed-weights
cog push localhost:5000/managed-weights

# OCI index: model image + weight descriptor with correct annotations
crane manifest localhost:5000/managed-weights:latest | jq '.manifests[] | {mediaType, platform, annotations, artifactType}'

# Weight manifest: config blob with file index, set digest, layer annotations
crane manifest localhost:5000/managed-weights:latest --platform unknown/unknown | jq '{artifactType, config, annotations}'

# Config blob: file-level index with path, layer, size, digest
crane blob localhost:5000/managed-weights@$(crane manifest localhost:5000/managed-weights:latest --platform unknown/unknown | jq -r .config.digest) | jq .
```


## Summary of Changes

Milestone verified complete. `cog push` produces a valid OCI bundle index with model image + weight manifests. Verified end-to-end with the examples/managed-weights model against a local registry -- OCI index, weight manifest, config blob, and layer annotations all match the v1 spec. The example predictor validates delivered weights against a baked-in manifest and returns a structured diff from predict().
