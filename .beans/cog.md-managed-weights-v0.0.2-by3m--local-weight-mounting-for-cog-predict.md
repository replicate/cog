---
# cog.md-managed-weights-v0.0.2-by3m
title: Local weight mounting for cog predict
status: todo
type: task
priority: normal
created_at: 2026-04-17T19:28:06Z
updated_at: 2026-04-17T19:33:57Z
parent: cog.md-managed-weights-v0.0.2-9qcd
blocked_by:
    - cog.md-managed-weights-v0.0.2-kfvj
---

Mount pulled weight images into model containers for cog predict/run.

## Spike first: validate overlay2 on Docker Desktop

Before implementing the full flow, validate the MergedDir approach works on Docker Desktop for Mac. Minimal test:
1. docker create --read-only --name test-wt <any-multi-layer-image>
2. docker inspect test-wt -> read GraphDriver.Data["MergedDir"]
3. docker run -v <MergedDir>:/mnt:ro alpine ls /mnt
If step 3 fails (path not accessible from host), the overlay2 approach needs a fallback (--volumes-from, named volumes, etc.). Do this spike before building the full integration.

Strategy (v1, local Docker daemon):
1. Verify overlay2 storage driver (docker info)
2. docker create --read-only --name cog-wt-<name>-<short-digest> cog-weights/<name>:<short-digest>
3. docker inspect -> GraphDriver.Data["MergedDir"]
4. Bind-mount MergedDir at weight target path (read-only) when running model container

Integration with cog predict:
- Check weights cached locally before starting container
- TTY: prompt to pull if missing. Non-TTY: error with instructions.
- Multiple weights = multiple bind mounts
- Reuse existing weight containers across predict runs
- Handle 409 Conflict on concurrent container creation

Needs Docker Desktop (Mac) verification: MergedDir is inside the Linux VM, but bind mounts are resolved by the daemon so should work. Test this explicitly.

cog weights purge: remove weight containers and images.

Reference: plans/2026-04-16-managed-weights-v2-design.md §5.5, §5.6
