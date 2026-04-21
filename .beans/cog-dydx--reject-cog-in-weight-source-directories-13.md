---
# cog-dydx
title: Reject .cog/ in weight source directories (§1.3)
status: todo
type: task
priority: low
created_at: 2026-04-21T00:01:11Z
updated_at: 2026-04-21T17:01:35Z
parent: cog-66gt
---

Spec §1.3: producers MUST reject source directories containing a .cog/ subdirectory, which is reserved for the runtime state protocol (§3.2).

## Todo

- [ ] Add check in `Pack()` or source resolution: if source directory contains a `.cog/` subdirectory, return a descriptive error
- [ ] Unit test: packing a directory with `.cog/` inside fails with clear message
- [ ] Unit test: packing a directory without `.cog/` succeeds as before

## Context

The `.cog/` directory within a weight target is reserved for runtime state markers (`ready`, `failed`, `downloading`). If a user's source weights directory already contains `.cog/`, those files would pollute the weight artifact and conflict with the runtime protocol.
