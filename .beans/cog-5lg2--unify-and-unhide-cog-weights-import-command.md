---
# cog-5lg2
title: Unify cog weights import command
status: completed
type: task
priority: critical
created_at: 2026-04-17T21:32:13Z
updated_at: 2026-04-21T23:42:25Z
parent: cog-66gt
blocked_by:
    - cog-4fg4
---

The plumbing for `cog weights import` (directory source → packed layers → registry) is done as of 4fg4. Two separate hidden commands currently exist:

- `cog weights build` — packs the source directory and writes `weights.lock`
- `cog weights push` — pushes the packed layers + manifest to the registry

Bean 2gv9 calls for unifying these into a single `cog weights import` that performs both steps atomically and unhides the command.

Scope:
- [x] Add `cog weights import [name...]` — runs build then push for the listed weights (or all if none specified)
- Unhiding moved to cog-gxqs (deferred until release)
- [x] Keep `build` and `push` as visible subcommands for advanced use (build without pushing, push after manual build)
- [x] Update `docs/cli.md` via `mise run docs:cli` (no visible change since parent command is still hidden)
- [x] Update the 2026-04-16 design doc if the flag names drift

Out of scope (separate beans):
- Source fingerprinting (s5fy)
- Non-file:// source schemes (9vfd)
- Include/exclude filters (6wm0)

Split out from 2gv9. 2gv9's "end-to-end wiring" work is done; this is the user-facing surface polish.

## Summary of Changes

Added `cog weights import [name...]` command that performs build + push in a single step. Refactored the existing `build` and `push` commands to use shared helpers, eliminating duplication.

**New command**: `cog weights import [name...]`
- Accepts optional weight names to import a subset; imports all if none specified
- `--image` flag overrides the cog.yaml image field
- Build phase: packs source directories into tar layers, updates lockfile
- Push phase: concurrent layer upload with progress display and retry

**Refactored helpers** (shared by import/build/push):
- `collectWeightSpecs` — extracts weight specs with optional name filtering and deduplication
- `buildWeightArtifacts` — builds specs into artifacts sequentially
- `parseRepoOnly` — validates a bare repository reference (rejects tags/digests)
- `pushWeightArtifacts` — concurrent push with progress and retry display

**Design doc updated**: `plans/2026-04-16-managed-weights-v2-design.md` now reflects `[name...]` (variadic, all-by-default) instead of `<name>` + `--all`, and `--image` flag instead of removed `--all`.

Code review caught: redundant no-weights guards in push, missing duplicate-name dedup in filter, double pw.Close(). Fixed.

## Summary of Changes

Added `cog weights import [name...]` command that performs build + push atomically. Refactored existing `build` and `push` commands to share helpers, eliminating duplication.

Files changed:
- `pkg/cli/weights.go` — new import command + extracted helpers (collectWeightSpecs, buildWeightArtifacts, parseRepoOnly, pushWeightArtifacts)
- `pkg/cli/weights_inspect.go` — uses shared parseRepoOnly, dropped unused import
- `pkg/model/artifact_weight.go` — added TotalSize() method
