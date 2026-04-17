---
# cog.md-managed-weights-v0.0.2-iy3e
title: coglet readiness protocol works in container
status: todo
type: milestone
priority: normal
created_at: 2026-04-17T19:35:01Z
updated_at: 2026-04-17T21:51:27Z
parent: cog.md-managed-weights-v0.0.2-9qcd
blocked_by:
    - cog.md-managed-weights-v0.0.2-7sne
---

Validation checkpoint. At this point you should be able to:

1. Build a model image with managed weights (/.cog/weights.json embedded)
2. Start the container with weight volumes mounted and .cog/ state protocol active
3. Coglet detects managed weight mode, waits for .cog/ready
4. When .cog/ready appears, coglet calls setup() and the model starts serving
5. If .cog/failed appears, coglet reports the error and fails startup with an actionable message

This validates the cog <-> infra contract end-to-end. The same coglet code works in both local dev (weights pre-mounted) and production (weights delivered by infra).

## Verification

```bash
# Build image with weights
cog build

# Simulate infra delivery: run container with a volume, write state markers manually
docker run -d --name test-coglet \
  -v /tmp/test-weights:/src/weights:ro \
  <model-image>

# In another terminal, simulate the provider:
mkdir -p /tmp/test-weights/.cog/layers
echo '{"version":"1","name":"test","digest":"sha256:...","layers":[]}' > /tmp/test-weights/.cog/manifest.json
touch /tmp/test-weights/.cog/downloading
# ... extract weight files ...
touch /tmp/test-weights/.cog/ready

# Coglet should proceed to setup() after .cog/ready appears
docker logs test-coglet  # should show setup starting
```
