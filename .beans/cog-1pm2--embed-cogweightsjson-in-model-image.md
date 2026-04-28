---
# cog-1pm2
title: Embed /.cog/weights.json in model image
status: completed
type: task
priority: high
created_at: 2026-04-17T19:28:15Z
updated_at: 2026-04-28T00:15:01Z
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

- [x] Define `RuntimeWeightsManifest` / `RuntimeWeightEntry` types (`name`, `target`, `setDigest`)
- [x] `WeightsLock.RuntimeManifest()` projection function
- [x] At `cog build` time: read `weights.lock`, project to runtime manifest, serialize to `/.cog/weights.json`, COPY into image
- [x] `pkg/dockerfile/` generator: emit the COPY directive (not needed — bundled via final Docker layer, same pattern as schema)
- [x] `pkg/image/build.go`: hook the generation step before Docker build
- [x] Round-trip test against spec §3.3 example

## Summary of Changes

Added runtime weights manifest generation to `cog build`. When managed weights are configured (`weights` stanza in cog.yaml) and a lockfile exists, the build pipeline now:

1. Loads `weights.lock` from the project directory
2. Projects it to the minimal spec §3.3 runtime manifest (name, target, setDigest per weight)
3. Writes `.cog/weights.json` into the build context
4. COPYs it into the final image layer alongside the OpenAPI schema

**New types** (`pkg/weights/lockfile/lockfile.go`):
- `RuntimeWeightsManifest` and `RuntimeWeightEntry` — deliberately separate from the lockfile types; only the three fields coglet needs.
- `(*WeightsLock).RuntimeManifest()` — projection function.

**Build wiring** (`pkg/image/build.go`):
- `writeRuntimeWeightsManifest(dir)` — loads lockfile, projects, writes `.cog/weights.json`. Errors with actionable guidance if lockfile is missing.
- `buildBundleDockerfile()` — shared helper for the skipLabels and full-build paths to emit COPY directives for all `.cog/` bundled files.
- `BuildAddLabelsAndSchemaToImage` refactored from single `bundledSchemaFile string` to `bundleFiles []string` so both schema and weights are bundled in a single final Docker layer.
- Stale `.cog/weights.json` cleaned up at Build() entry, same as the schema file.

**Tests**: 9 new tests covering projection (spec §3.3 field exactness, round-trip, multi-weight, empty), build function (writes correct file, missing lockfile error), and Dockerfile generation (schema-only, nothing, with-weights-file).
