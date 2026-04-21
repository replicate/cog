---
# cog-kgd7
title: cog predict works with managed weights locally
status: todo
type: milestone
priority: normal
created_at: 2026-04-17T19:34:49Z
updated_at: 2026-04-17T21:51:27Z
parent: cog-9qcd
blocked_by:
    - cog-by3m
    - cog-1pm2
---

Validation checkpoint. At this point you should be able to:

1. Clone a model repo that uses managed weights
2. Run: cog weights pull (downloads weight image from registry into Docker)
3. Run: cog predict -i prompt="a cat" and get a prediction back
4. The predictor's setup() sees weight files at the target path as if they were baked into the image
5. No weight data is duplicated on disk (layers exist once in Docker's content store)

This is the local dev workflow working end-to-end.

## Verification

```bash
# From a model repo with managed weights configured and imported
cog weights pull
cog predict -i prompt="test"

# Verify no duplication: check Docker disk usage
docker system df -v | grep cog-wt

# Verify weights are mounted read-only
docker inspect <running-container> | jq '.Mounts[] | select(.Destination=="/src/weights")'
```

## Also verify

- cog predict without prior pull: should prompt in TTY, error in non-TTY
- Second cog predict reuses the weight container (no re-creation)
- cog weights purge frees disk space
