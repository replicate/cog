---
# cog-6b5a
title: cog weights status command
status: in-progress
type: task
priority: normal
created_at: 2026-04-17T21:31:40Z
updated_at: 2026-04-22T18:02:17Z
parent: cog-66gt
---

Implement `cog weights status`. For every declared weight, shows its state across config (`cog.yaml`), lockfile (`weights.lock`), and registry. Subsumes the original list (6b5a) and check (wej9) beans — they are the same command.

## Design rationale

This follows the dependency-manager mental model: weights are **declared** (config), **resolved** (lockfile via build), and **published** (registry via push/import). `status` shows where each weight sits in that pipeline. Unlike a separate `list` + `check`, one command gives a single coherent view without the user needing to mentally merge two outputs.

Analogy: `git status` not `git list-files` + `git check-remote`.

## Status values

| Status | Config? | Lockfile? | Registry? | Config matches lockfile? |
|--------|---------|-----------|-----------|-------------------------|
| `ready` | yes | yes | yes (digest match) | yes |
| `incomplete` | yes | yes | no (missing/partial) | yes |
| `stale` | yes | yes | don't care | no |
| `pending` | yes | no | n/a | n/a |
| `orphaned` | no | yes | don't care | n/a |

Every non-`ready` status is fixed by `cog weights import`.

**Stale detection:** compare config's `target`, `source.uri`, `source.include`, `source.exclude` against the lockfile entry's corresponding fields.

**Registry check:** reuse `fetchRemoteWeight` pattern from `weights_inspect.go`. Concurrent per-weight, errors per-weight are soft (weight shows as `incomplete`, not a command failure).

## Text output

```
NAME             TARGET            STATUS       SIZE     LAYERS  DIGEST
llama-3-base     /weights/base     ready        4.2GB   3       sha256:a1b2c3…
lora-adapter     /weights/lora     stale        12.1MB  1       sha256:d4e5f6…
new-weights      /weights/new      pending      -       -       -
```

## Flags

- `--json` — structured output (full digests, raw byte sizes, source metadata)
- No `--remote` / `--local` — always checks all tiers. If registry unreachable, degrade gracefully (show `incomplete` with a warning, don't hard-fail).

## Exit code

0 if all weights are `ready`, non-zero otherwise. Replaces `check`'s exit-code contract for CI use.

## JSON output shape

```json
{
  "weights": [
    {
      "name": "llama-3-base",
      "target": "/weights/base",
      "status": "ready",
      "size": 4500000000,
      "sizeCompressed": 3200000000,
      "layerCount": 3,
      "fileCount": 12,
      "digest": "sha256:a1b2c3d4e5f6...",
      "source": {
        "uri": "hf://meta-llama/Llama-3-8B",
        "fingerprint": "commit:abc123..."
      }
    }
  ]
}
```

## Existing code to reuse

- `model.WeightLockEntry` / `model.WeightLockLayer` for lockfile data
- `fetchRemoteWeight` / `resolveWeightsByTag` in `pkg/cli/weights_inspect.go` for registry checks
- `formatSize` helper in `pkg/cli/weights.go`
- `model.NewSource(configFilename)` + `model.LoadWeightsLock(lockPath)` for loading

## Implementation plan

- [x] Create `pkg/cli/weights_status.go` with `newWeightsStatusCommand()` and `weightsStatusCommand()`
- [x] Define output types: `WeightsStatusOutput`, `WeightStatusEntry`
- [x] Load config + lockfile, match declarations to lockfile entries
- [x] Stale detection: compare config fields against lockfile source fields
- [x] Registry check: concurrent per-weight tag resolution (soft errors)
- [x] Text output: tabular with NAME, TARGET, STATUS, SIZE, LAYERS, DIGEST columns
- [x] JSON output: full structured data
- [x] Exit code: 0 = all ready, 1 = otherwise (sentinel error, not os.Exit)
- [x] Register in `newWeightsCommand()` (`weights.go`)
- [x] Tests in `pkg/cli/weights_status_test.go` (17 tests)

Reference: plans/2026-04-16-managed-weights-v2-design.md §3 (User-facing commands)

## Review feedback (2026-04-22)

Status determination logic was incorrectly placed in `pkg/cli/`. The CLI was reading config + lockfile + doing field-by-field comparison including URI normalization -- all domain logic that belongs in `pkg/model`. This caused a bug: `isStale` compared raw config URIs ("weights") against normalized lockfile URIs ("file://./weights"), always returning stale.

Fix: move status computation into `pkg/model/weights_status.go` with proper tests. CLI becomes thin presentation layer that calls model, merges registry state, and formats output.

## Implementation summary

### Architecture
- **`pkg/model/weights_status.go`** — domain logic. `ComputeWeightsStatus(ctx, cfg, lock, checker)` returns a `*WeightsStatus` struct with `Results()`, `AllReady()`, `HasProblems()`, `ByStatus()` methods. Owns URI normalization (via `weightsource.NormalizeURI`), stale detection, orphan detection, concurrent registry checks with errgroup + context cancellation.
- **`pkg/model/weights_status_test.go`** — 24 tests covering all status transitions, URI normalization edge cases (bare path, dot-slash, file://), registry integration (found/not-found/error/cancellation), stale skips registry, pending skips registry, ordering, helpers.
- **`pkg/cli/weights_status.go`** — thin presentation layer. Loads config/lockfile, constructs a `WeightRegistryChecker` adapter around `fetchRemoteWeight`, calls `ComputeWeightsStatus`, formats output (tabular or JSON), returns sentinel error for exit code 1.
- **`pkg/cli/weights_status_test.go`** — 4 tests for presentation: result-to-entry mapping, JSON round-trip, digest formatting, text output smoke test.
- **`pkg/cli/weights.go`** — one line: `cmd.AddCommand(newWeightsStatusCommand())`

### Bug fixed
Config URI "weights" was compared directly against lockfile URI "file://./weights", always showing stale. Now `isStale` in `pkg/model` normalizes the config URI before comparison.
