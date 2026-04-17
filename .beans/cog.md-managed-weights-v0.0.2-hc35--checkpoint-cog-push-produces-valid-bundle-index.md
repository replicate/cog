---
# cog.md-managed-weights-v0.0.2-hc35
title: 'CHECKPOINT: cog push produces valid bundle index'
status: todo
type: task
priority: high
created_at: 2026-04-17T19:34:37Z
updated_at: 2026-04-17T19:34:37Z
parent: cog.md-managed-weights-v0.0.2-9qcd
blocked_by:
    - cog.md-managed-weights-v0.0.2-m4e8
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
