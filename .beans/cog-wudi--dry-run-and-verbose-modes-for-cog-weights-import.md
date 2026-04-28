---
# cog-wudi
title: Dry-run and verbose modes for cog weights import
status: completed
type: task
priority: high
created_at: 2026-04-27T22:28:57Z
updated_at: 2026-04-27T23:58:15Z
---

Add --dry-run and --verbose flags to `cog weights import`.

## Motivation

When iterating on include/exclude patterns or changing weight sources, there's no way to preview what an import will do without committing to a potentially lengthy download + push cycle. Users need to see what's changing before they run the full pipeline.

## Design

### --dry-run

Runs inventory + filter for each weight, prints a status summary showing what would change vs the current lockfile, then exits without modifying anything (no ingress, no pack, no push, no lockfile write).

Output for each weight shows:
- Name, source URI, target
- Status: new (no lockfile entry), unchanged (fingerprint + patterns match), stale (config drifted), or updated (upstream changed)
- For stale/updated: what changed (URI, target, include/exclude patterns, fingerprint)
- File count and total size after filtering

### --verbose (combinable with --dry-run)

Adds per-file detail:
- Lists every file that passes the filter with path and size
- For exclude-only or include+exclude: marks files that were excluded by which pattern set
- Useful for debugging filter patterns

### Implementation plan

1. Add `--dry-run` and `--verbose` flags to the import command
2. Extract an `ImportPlan` step from the builder that runs inventory + filter without ingress
3. CLI prints the plan summary; if --dry-run, exit
4. Otherwise proceed with the existing import pipeline
5. Tests for dry-run output

## Todo

- [x] Add ImportPlan method to WeightBuilder (inventory + filter only)
- [x] Add --dry-run and --verbose flags to import command
- [x] Print plan summary (what would change, file counts, sizes)
- [x] --verbose: per-file listing with filter status
- [x] Exit after plan when --dry-run is set
- [x] Tests for dry-run and verbose output


## Summary of Changes

Implemented --dry-run and --verbose flags on cog weights import.

**New files:**
- pkg/model/weight_import_plan.go — PlanImport method + WeightImportPlan type
- pkg/model/weight_import_plan_test.go — 7 unit tests

**Modified files:**
- pkg/cli/weights.go — Added --dry-run, --verbose flags, plan printing
- docs/cli.md — Regenerated
- docs/llms.txt — Regenerated
