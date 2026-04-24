---
# cog-1pm2
title: Embed /.cog/weights.json in model image
status: todo
type: task
priority: high
created_at: 2026-04-17T19:28:15Z
updated_at: 2026-04-23T22:22:36Z
parent: cog-kgd7
blocked_by:
    - cog-2gv9
    - cog-ez7g
    - cog-s5fy
---

During cog build, write /.cog/weights.json into the model image when weights are configured.

This file:
- Signals to coglet that managed weights are active (presence = managed mode)
- Contains the weight expectations: name, target, digest, layer digests and sizes
- Derived from weights.lock

Schema:
{
  "weights": [{
    "name": "z-image-turbo",
    "target": "/src/weights",
    "digest": "sha256:abc...",
    "layers": [
      {"digest": "sha256:aaa...", "size": 15000000},
      {"digest": "sha256:bbb...", "size": 3957900840}
    ]
  }]
}

Integration point: pkg/dockerfile/ generator adds a COPY for the generated weights.json. Build orchestration (pkg/image/build.go) generates the file from the lockfile before Docker build.

Reference: specs/weights.md §3.6



## Design update (s5fy lockfile redesign, 2026-04-21)

After s5fy, the lockfile carries far more than the runtime needs — provenance, file index, layer details. `/.cog/weights.json` is a **projection** from the lockfile, not a direct serialization of it. The types are deliberately different.

Per spec §3.3, the runtime file is minimal:

```json
{
  "weights": [{
    "name": "z-image-turbo",
    "target": "/src/weights",
    "setDigest": "sha256:def456..."
  }]
}
```

Three fields per entry. The lockfile's `WeightLockEntry` carries all of these plus source metadata, file index, layer details, etc. This bean defines a separate `RuntimeWeightsManifest` type and a projection function that extracts the three fields from each lockfile entry. A round-trip test verifies the projection against the spec §3.3 example.

Tasks:

- [ ] Define `RuntimeWeightsManifest` / `RuntimeWeightEntry` types (`name`, `target`, `setDigest`)
- [ ] `WeightsLock.RuntimeManifest()` projection function
- [ ] At `cog build` time: read `weights.lock`, project to runtime manifest, serialize to `/.cog/weights.json`, COPY into image
- [ ] `pkg/dockerfile/` generator: emit the COPY directive
- [ ] `pkg/image/build.go`: hook the generation step before Docker build
- [ ] Round-trip test against spec §3.3 example
