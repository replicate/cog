---
# cog.md-managed-weights-v0.0.2-9qcd
title: Managed Weights v2
status: in-progress
type: epic
priority: high
created_at: 2026-04-17T19:26:28Z
updated_at: 2026-04-17T19:26:28Z
---

Implement managed weights v2: OCI-based weight storage, import/push pipeline, local dev workflow, and runtime integration. See specs/weights.md for the OCI format spec and plans/2026-04-16-managed-weights-v2-design.md for the full design.

## Priority order

1. Race to a real managed weight image in the registry (infra can start testing)
2. Refine import/management commands for full functionality
3. Local dev workflow (running models with managed weights locally)
4. Polish, optimizations, extended source support

## Key spec decisions

- Layers are order-independent (disjoint file sets, parallel extract)
- Standard OCI layer media types + custom artifactType discriminator
- Runtime state protocol via .cog/ directory in weight target
- /.cog/weights.json in model image signals managed weight mode to coglet
