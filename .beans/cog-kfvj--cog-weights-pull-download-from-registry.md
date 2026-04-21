---
# cog-kfvj
title: cog weights pull (download from registry)
status: todo
type: task
priority: low
created_at: 2026-04-17T19:27:55Z
updated_at: 2026-04-21T19:49:02Z
parent: cog-kgd7
blocked_by:
    - cog-b2mv
---

Implement cog weights pull to download weight layers from a registry into Docker.

Flow:
- Read weights.lock for manifest digests
- docker pull <registry>/<model>/weights/<name>@sha256:<digest>
- Tag locally as cog-weights/<name>:<short-digest>
- Skip layers already cached (Docker handles this via content-addressable storage)
- Multiple weights pull in parallel (4 default, --concurrency=N)
- Progress output per weight

This is a prerequisite for local running. Can be implemented as soon as multi-layer weight manifests exist in a registry (after manifest push task).

Reference: plans/2026-04-16-managed-weights-v2-design.md §5.5
