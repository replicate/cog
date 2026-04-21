---
# cog-1pm2
title: Embed /.cog/weights.json in model image
status: todo
type: task
priority: critical
created_at: 2026-04-17T19:28:15Z
updated_at: 2026-04-21T17:11:38Z
parent: cog-kgd7
blocked_by:
    - cog-2gv9
    - cog-ez7g
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



## Scope reduction (4fg4, 2026-04-17)

4fg4 aligned the in-repo `weights.lock` shape with spec §3.6. `WeightsLock.Weights` (`[]WeightLockEntry`) is now a byte-for-byte match for the `/.cog/weights.json` schema described in this bean. That collapses most of the work here to:

- [ ] At `cog build` time: read `weights.lock`, serialize `lock.Weights` to `/.cog/weights.json`, COPY it into the image
- [ ] `pkg/dockerfile/` generator: emit the COPY directive
- [ ] `pkg/image/build.go`: hook the generation step before Docker build

No translation layer needed. The `model.WeightLockEntry` type is the shape. Write a round-trip test against the spec §3.6 example to catch drift.


## Spec update (2026-04-20)

Per spec §3.3, the `/.cog/weights.json` schema now requires a `setDigest` field per weight entry — the weight set digest from §2.4. This field comes from the config blob work (tracked separately). The schema is:

```json
{
  "weights": [{
    "name": "z-image-turbo",
    "target": "/src/weights",
    "setDigest": "sha256:def456..."
  }]
}
```

This means the lockfile's `WeightLockEntry` must also carry `setDigest` before this bean can be completed — dependency on the config blob + set digest bean.
