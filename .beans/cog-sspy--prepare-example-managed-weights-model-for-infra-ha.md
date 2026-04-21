---
# cog-sspy
title: Prepare example managed-weights model for infra handoff
status: completed
type: task
priority: critical
created_at: 2026-04-21T17:12:15Z
updated_at: 2026-04-21T17:24:19Z
parent: cog-hc35
---

Rename examples/test-weights to examples/managed-weights. Update the predictor to serve as an infra verification tool:

## Tasks
- [x] Rename examples/test-weights -> examples/managed-weights (directory, README, cog.yaml references)
- [x] Update predict.py setup(): log weight file inventory on boot — for each file under the weight target, log path, size, sha256 digest
- [x] Update predict.py predict(): return find-style output — list all files under weight target with path, size, sha256 digest
- [x] Optional: warn on setup if weight directory is missing (logs warning instead of erroring — avoids container reaping before logs flush)
- [x] Push to local registry and verify full artifact shape:
  - OCI index: model image (linux/amd64) + weight descriptor (unknown/unknown) with run.cog.weight.name, run.cog.weight.set-digest, artifactType
  - Weight manifest: artifactType=application/vnd.cog.weight.v1, config blob mediaType=application/vnd.cog.weight.config.v1+json, three run.cog.weight.* annotations (name, target, set-digest)
  - Config blob: file-level index with files[] array (path, layer digest, size, content digest), setDigest, name, target
  - Layer annotations: run.cog.weight.content (bundle/file), run.cog.weight.size.uncompressed, run.cog.weight.file (for standalone layers)
  - Sizes and digests all match actual content

## Existing state
- examples/test-weights already has a hand-written .dockerignore excluding weights/ and .cog/weights-cache/
- weights.lock exists with parakeet weights (3 layers, ~5GB total)
- Current predictor is a minimal stub that stats a single file

## Verification
```bash
cd examples/managed-weights
docker run -d --rm -p 5000:5000 --name cog-test-registry registry:3
cog push localhost:5000/managed-weights

# Verify index
crane manifest localhost:5000/managed-weights:latest | jq '.manifests[]'

# Verify weight manifest
crane manifest localhost:5000/managed-weights:latest --platform unknown/unknown | jq .

# Verify config blob
crane blob localhost:5000/managed-weights@$(crane manifest localhost:5000/managed-weights:latest --platform unknown/unknown | jq -r .config.digest) | jq .
```


## Summary of Changes

- Renamed `examples/test-weights/` to `examples/managed-weights/`
- Updated `.gitignore` to reference new directory name
- Rewrote `predict.py`: `setup()` logs full weight inventory (path, size, sha256); `predict()` returns find-style tab-separated listing; warns (not errors) if weight dir missing
- Updated `cog.yaml` and `README.md` references from test-weights to managed-weights
- Verified push to local registry — OCI index, weight manifest, config blob, and layer annotations all match spec
