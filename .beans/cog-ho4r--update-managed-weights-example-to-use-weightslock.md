---
# cog-ho4r
title: Update managed-weights example to use weights.lock
status: completed
type: task
priority: normal
created_at: 2026-04-23T21:13:31Z
updated_at: 2026-04-23T21:30:31Z
---

Replace the ad-hoc weights_manifest.json and generate_manifest.py with validation derived from the real weights.lock file. Error on missing files, warn on extra files.

## Summary of Changes

- **Deleted** `weights_manifest.json` — ad-hoc single-weight manifest replaced by `weights.lock`
- **Deleted** `generate_manifest.py` — no longer needed; `cog weights import` generates the lockfile
- **Rewrote** `predict.py` to read `weights.lock` directly:
  - Iterates all weight entries in the lockfile (not just parakeet)
  - Errors on missing files or digest/size mismatches (blocks setup)
  - Warns on extra files (logs WARNING but doesn't block setup)
  - predict() returns a per-weight summary instead of a flat file list
- **Updated** `README.md` — removed manifest generation docs, added `cog weights import` workflow
- **Updated** `cog.yaml` comment — removed references to old manifest approach
