---
# cog-r2yj
title: Remove build, push, and inspect weight commands and dead code
status: completed
type: task
priority: high
created_at: 2026-04-22T20:34:00Z
updated_at: 2026-04-22T20:39:49Z
parent: cog-66gt
---

Remove cog weights build, push, and inspect commands since import subsumes build+push and status subsumes inspect. Clean up any dead code left behind.

## Summary of Changes

Removed three subcommands from `cog weights`:

- **`build`** — lockfile generation is handled internally by `import`
- **`push`** — was functionally identical to `import` (it rebuilt artifacts before pushing anyway)
- **`inspect`** — fully subsumed by `status`, which checks config/lockfile/registry in one pass

### Files changed
- **Deleted** `pkg/cli/weights_inspect.go` (349 lines) — entire inspect command and all its types
- **Modified** `pkg/cli/weights.go` — removed `newWeightsBuildCommand`, `weightsBuildCommand`, `newWeightsPushCommand`, `weightsPushCommand`, and their `AddCommand` registrations. Updated `import` long description to not reference removed commands.

### Dead code check
No dead code was left behind. All shared helpers (`collectWeightSpecs`, `buildWeightArtifacts`, `parseRepoOnly`, `pushWeightArtifacts`, `formatSize`) are still used by the `import` command. All model-layer types (`WeightPusher`, `WeightTag`, etc.) are still used by both the CLI and the combined `cog push` pipeline.

### Remaining subcommands
- `cog weights import [name...]` — build lockfile + push to registry
- `cog weights status` — show per-weight readiness across config/lockfile/registry
