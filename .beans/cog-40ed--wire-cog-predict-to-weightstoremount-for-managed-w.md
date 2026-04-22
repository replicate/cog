---
# cog-40ed
title: Wire cog predict to WeightStore.Mount for managed weights
status: todo
type: task
priority: high
created_at: 2026-04-22T20:24:11Z
updated_at: 2026-04-22T20:43:46Z
parent: cog-kgd7
blocked_by:
    - cog-gbse
    - cog-xhpw
    - cog-3p4a
---

When `cog predict` / `cog run` targets a model with managed weights, call `WeightStore.Mount` for each weight and bind-mount the returned path `:ro` into the model container at the weight's target.

## Why

Closes the local-dev-workflow loop: after `cog weights pull`, a user can run `cog predict` and the predictor's `setup()` sees the weight files at the configured target path, with zero duplication on disk.

## Scope

### Flow

1. On `cog predict` / `cog run`, after parsing cog.yaml + weights.lock:
   - For each weight in the lockfile, call `store.HasSet(setDigest)`.
   - If any weight is missing, fail with clear error: `"Weights not cached locally. Run 'cog weights pull' first."` (v1: no auto-pull.)
2. For each weight, call `store.Mount(ctx, setDigest, layers) → MountHandle`.
3. Collect bind mounts: `-v <handle.Path()>:<weight.target>:ro` per weight.
4. Start model container with the bind mounts added to the existing docker run args.
5. After the container exits, call `Release` on each handle.

### Edge cases

- Multiple weights with disjoint targets (already enforced at config-validate time) → multiple bind mounts, no nesting.
- Model without managed weights (no `weights` stanza, no lockfile): no-op, current behavior.
- `Mount` failure (e.g. cross-filesystem hardlink error): fail with clear message, don't start container.

## Scope (code)

- Update `pkg/cli/predict.go` (and `run.go` if applicable) to wire in the store.
- Extract a helper: `prepareManagedWeightMounts(ctx, store, cfg, lock) → ([]MountSpec, ReleaseFn, error)` so `predict` and `run` share logic.
- Tests: managed-weights happy path, missing-weights error path, multi-weight case.

## Out of scope

- Auto-pull on missing weights (separate bean, deferred). V1 requires explicit `cog weights pull`.
- Model image `/.cog/weights.json` signal — handled by cog-1pm2.
- Readiness protocol markers — `FileWeightStore.Mount` writes `.cog/ready` as part of assembly; coglet readiness integration is cog-7sne / cog-iy3e.

## Dependencies

Blocked by:
- WeightStore interface
- FileWeightStore implementation
- cog weights pull wiring (practically: users need a way to populate the store)

## Todo

- [ ] Helper: `prepareManagedWeightMounts` in appropriate package
- [ ] `cog predict` wiring: check `HasSet`, call `Mount`, add bind mounts
- [ ] `cog run` wiring: same
- [ ] Release `MountHandle` after container exits (defer / signal handler)
- [ ] Error message for missing weights pointing at `cog weights pull`
- [ ] Tests
- [ ] Verify interaction with existing coglet readiness protocol (cog-7sne)

## Reference

- `plans/2026-04-22-managed-weights-import-and-local-run-design.md` §4, §5
- Supersedes / updates existing `cog-by3m` (this is the actual work; cog-by3m will be retitled to match).
