---
# cog.md-managed-weights-v0.0.2-ng36
title: Auto-generate .dockerignore for cog.yaml weights entries
status: todo
type: task
priority: critical
created_at: 2026-04-17T23:12:10Z
updated_at: 2026-04-21T15:59:04Z
parent: cog.md-managed-weights-v0.0.2-9qcd
---

When cog.yaml has v1 managed `weights:` entries, `cog build` / `cog push` currently copy the entire weight source directory into the docker build context via `COPY . /src`. Weights can be multi-GB per project, so every build transfers gigabytes for no reason — the weights land in the image at runtime via OCI layer extraction, not at build time.

## Repro

```bash
cd examples/test-weights
rm .dockerignore  # undo the manual fix
COG_OCI_INDEX=1 cog push localhost:5000/test-weights
```

Observe in buildkit output:

```
#9 [internal] load build context
#9 transferring context: 9.87GB 64.4s
```

With a hand-written `.dockerignore` that excludes `weights/`, the same context transfer is **11.97 MB in 0.1 s**.

## Proposed fix

In `pkg/image/build.go`, on the `!separateWeights` branch (the one taken by v1 managed weights), auto-generate a `.dockerignore` that excludes every `cfg.Weights[].Source` path. Existing machinery to crib from:
- `backupDockerignore` / `restoreDockerignore` (lines 650–677)
- `writeDockerignore` (line 637) — already merges with existing user-written `.dockerignore`
- `makeDockerignoreForWeights` in `pkg/dockerfile/standard_generator.go` — builds an exclude list, but hard-codes the legacy HF auto-detect path. We want something analogous that walks `cfg.Weights` instead of `FindWeights(fileWalker)`.

Also needed: exclude `.cog/weights-cache/` (the packed-tar cache produced by `cog weights build`) but NOT `.cog/tmp/` (cog's own Dockerfile stages wheels + CA cert through there — excluding the whole `.cog/` breaks the build). Learned the hard way.

## Tasks
- [ ] Decide whether to do this unconditionally when `cfg.Weights` is non-empty, or gate on `COG_OCI_INDEX` / v1 managed-weights feature flag. Probably unconditional — there is no scenario where weights declared in `cog.yaml` should end up in the image.
- [ ] Factor a `makeDockerignoreForManagedWeights(cfg.Weights) string` helper
- [ ] Wire `backupDockerignore` → `writeDockerignore(managedWeightsExcludes)` → docker build → `restoreDockerignore` around the `!separateWeights` image build in `pkg/image/build.go`
- [ ] Add unit tests covering: no weights (no-op), some weights (excludes appear), existing user `.dockerignore` (merged correctly)
- [ ] Add integration test that asserts a multi-GB weights dir does not inflate the build context
- [ ] Update `examples/test-weights/` to delete the hand-written `.dockerignore` once this is fixed

## Out of scope
- General `include`/`exclude` filters on weight packing (that's bean `6wm0`). This task is just about keeping weights out of the docker build context.
