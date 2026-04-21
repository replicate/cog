---
# cog-jx8l
title: v1 spec-compliant push for infra
status: scrapped
type: milestone
priority: critical
created_at: 2026-04-21T15:56:18Z
updated_at: 2026-04-21T16:57:50Z
blocked_by:
    - cog-m4e8
    - cog-1pm2
    - cog-ng36
---

cog push produces a v1 spec-compliant OCI index that infra can consume: model image with /.cog/weights.json + weight manifests with config blob, set digest, and clean annotations. This is the handoff point where infra can begin building extraction/delivery support.

## Required tasks
- [ ] OCI index assembly (m4e8) -- remove COG_OCI_INDEX gate
- [ ] Embed /.cog/weights.json in model image (1pm2)
- [ ] Auto-generate .dockerignore for weights (ng36)

## Verification
```bash
cog weights import parakeet
cog build
cog push

# Verify index structure
crane manifest <registry>/<model>:latest | jq '.manifests[]'

# Verify weight manifest has config blob + set digest
crane manifest <registry>/<model>:latest --platform unknown/unknown | jq '{artifactType, config, annotations}'

# Verify model image contains weights.json
docker run --rm <registry>/<model>:latest cat /.cog/weights.json
```

## Reasons for Scrapping\n\nCollapsed into hc35 ("cog push produces valid bundle index"). hc35 now carries jx8l's task checklist and blockers. The two milestones were redundant — both gated on the same outcome.
