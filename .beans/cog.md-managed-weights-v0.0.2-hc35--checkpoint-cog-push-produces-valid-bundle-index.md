---
# cog.md-managed-weights-v0.0.2-hc35
title: cog push produces valid bundle index
status: todo
type: milestone
priority: critical
created_at: 2026-04-17T19:34:37Z
updated_at: 2026-04-21T16:00:17Z
parent: cog.md-managed-weights-v0.0.2-9qcd
blocked_by:
    - cog.md-managed-weights-v0.0.2-m4e8
    - cog.md-managed-weights-v0.0.2-jx8l
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



## Status (4fg4, 2026-04-17)

Checklist infrastructure is ready. One code blocker remains before this checkpoint can be verified cleanly:

- [ ] Remove the `COG_OCI_INDEX=1` env gate (tracked in m4e8). Without this, `cog push` produces a single-manifest push unless the caller sets the env var. After removal, presence of `weights:` in `cog.yaml` triggers bundle format automatically.

Amended verification steps:

```bash
# Full pipeline (current command names; to be renamed to `cog weights import` in 5lg2)
cog weights build
cog weights push
cog build
COG_OCI_INDEX=1 cog push    # env gate still required pre-m4e8

# Verify index structure (model image + weight descriptors with annotations)
crane manifest <registry>/<model>:latest | jq '.manifests[] | {mediaType, platform, annotations}'

# Verify weight manifest resolves from index
crane manifest <registry>/<model>:latest --platform unknown/unknown | jq .artifactType
# Expected: application/vnd.cog.weight.v1
```

Once m4e8 lands, drop the `COG_OCI_INDEX=1` prefix and run this checklist unmodified.
