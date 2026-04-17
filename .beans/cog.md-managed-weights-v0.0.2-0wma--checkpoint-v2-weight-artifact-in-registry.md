---
# cog.md-managed-weights-v0.0.2-0wma
title: 'CHECKPOINT: v2 weight artifact in registry'
status: todo
type: task
priority: high
created_at: 2026-04-17T19:34:21Z
updated_at: 2026-04-17T19:34:21Z
parent: cog.md-managed-weights-v0.0.2-9qcd
blocked_by:
    - cog.md-managed-weights-v0.0.2-2gv9
---

Validation checkpoint. At this point you should be able to:

1. Have a local directory of weight files (e.g., downloaded z-image-turbo or synthetic test data)
2. Run: cog weights import z-image-turbo
3. See the weight manifest in the registry
4. Run: crane manifest <registry>/<model>/weights/z-image-turbo:latest and see a valid OCI manifest with:
   - artifactType: application/vnd.cog.weight.v1
   - Multiple layers with standard OCI media types
   - run.cog.weight.* annotations on manifest and layers
   - Layer digests that match weights.lock
5. Run: crane pull and get a tarball with extractable weight files
6. Inspect weights.lock and see per-layer entries with digests matching the manifest

This is the infra handoff point. They can start building extraction, placement, and delivery systems against a real artifact.

## Verification

```bash
# Synthetic test: create a directory with mixed small/large files
mkdir -p /tmp/test-weights
echo '{"model": "test"}' > /tmp/test-weights/config.json
dd if=/dev/urandom of=/tmp/test-weights/model.safetensors bs=1M count=100

# Import (assumes cog.yaml is configured with source pointing to /tmp/test-weights)
cog weights import test-weights

# Verify manifest
crane manifest <registry>/test-model/weights/test-weights:latest | jq .

# Verify layers are pullable
crane pull <registry>/test-model/weights/test-weights:latest /tmp/test-pull.tar

# Verify lockfile
cat weights.lock | jq .
```
