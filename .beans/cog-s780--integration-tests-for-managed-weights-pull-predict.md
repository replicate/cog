---
# cog-s780
title: Integration tests for managed-weights pull + predict flow
status: completed
type: task
priority: normal
created_at: 2026-04-24T02:46:06Z
updated_at: 2026-04-24T06:03:23Z
parent: cog-kgd7
---

No integration tests exist for the pull → predict flow — pkg/weights unit tests use in-memory fixtures for the registry and the predictor isn't exercised at all. When someone later changes the OCI media types, the registry client, the tar format, the lockfile schema, the Docker volume wiring, or PredictorOptions, unit tests won't catch it.

Write two txtar integration tests mirroring the `weights_push_inspect.txtar` pattern (local registry, real cog binary):

1. **weights_pull.txtar** — focused on `cog weights pull`. Import weights to a local registry, isolate the cache via `COG_CACHE_DIR=$WORK/cache`, run pull and verify cache populated, run pull again and verify cached behavior, test `--verbose` output, test unknown-name error.

2. **weights_pull_predict.txtar** — end-to-end. Import → pull → predict. Verify the predictor sees the weight files at the configured target inside the container, and verify `.cog/mounts/` is cleaned up after predict exits.

Both skip under `[short]` since they need a local registry and Docker.

## Summary of Changes

Two txtar integration tests added under `integration-tests/tests/`:

**`weights_pull.txtar`** — Focused on `cog weights pull` orchestration. Against a local registry:
- `cog weights import` populates the registry + weights.lock
- `cog weights pull` on a cold cache fetches every weight (asserts "Pulling ... done" per weight, "Pulled" summary, cache directory populated with the sha256 layout)
- Second pull hits "cached" / "All N weight(s) already cached."
- `--verbose` surfaces cache path, lockfile path, per-layer / per-file lines
- Partial cache recovery: delete one blob, re-pull, verify only the missing one is fetched (verifies per-file Exists fast-path)
- Unknown weight names error with every missing name listed in a single error
- Name filter pulls only the requested weight

**`weights_pull_predict.txtar`** — End-to-end `cog weights import` → `cog weights pull` → `cog predict`. The Python predictor opens the weight file at its configured mount target (`/src/mounted-weights/greeting/greeting.txt`) and returns its size, proving the bind mount landed correctly inside the container. Asserts the stdout prediction carries the right size, and that `.cog/mounts/` is empty/absent after predict exits (proves `Predictor.Stop → Mounts.Release` cleanup works on the real Docker path).

Both tests use `env COG_CACHE_DIR=$WORK/cache` so the cache is isolated to the test workdir — no touching `$XDG_CACHE_HOME/cog`, reliable cross-run state.

Both skipped under `[short]` since they need a local registry (first test) and full Docker build + run (second test).

Confirmed passing locally against a real registry + Docker. Pre-existing `weights_push_inspect.txtar` and `weights_build.txtar` failures are unrelated — they reference a `cog weights build` command that doesn't exist on this branch.
