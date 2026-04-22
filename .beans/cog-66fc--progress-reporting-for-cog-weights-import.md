---
# cog-66fc
title: Progress reporting for cog weights import
status: todo
type: task
priority: high
created_at: 2026-04-22T15:21:10Z
updated_at: 2026-04-22T20:43:47Z
parent: cog-66gt
blocked_by:
    - cog-3p4a
    - cog-xhpw
---

cog weights import currently runs silently — no progress indicators during packing, hashing, or pushing. For large models (tens of GB, many files), users have no idea whether it's working or how long to wait.

Needs progress reporting at each stage:
- Scanning source directory (file count, total size)
- Hashing files for content digests (per-file or periodic progress)
- Packing layers (which layer, compressed size, compression ratio)
- Pushing layers to registry (per-layer progress with bytes transferred)
- Writing lockfile (summary of what changed)

Design constraints:
- Must work in both TTY (interactive progress bars) and non-TTY (periodic log lines)
- Should not add noise for small/fast imports — only show progress when it matters
- Layer push progress requires wiring into the existing registry.WriteLayer HTTP upload path

This is user-facing polish, not blocking the pipeline, but important for usability as soon as weights import is used with real model weights.



## Scope note (2026-04-22)

Updated by `plans/2026-04-22-managed-weights-import-and-local-run-design.md`. Progress reporting needs to cover the new plan/execute phases of import:

- Plan phase: inventory walk (for file://), API calls (for hf://), layer planning output
- Execute phase: per-layer pack → push progress (bytes transferred), resumption indication when pending state is picked up
- Manifest phase: manifest push + lockfile promotion

Also applies to `cog weights pull` (cog-xhpw) — progress for source reconstruction vs registry fallback, per-layer. The store's Fetch method should support a progress callback in FetchMeans.

Dependencies updated: this now aligns with cog-3p4a (plan/execute split) for import-side UX and cog-xhpw / cog-40ed for pull/predict UX.
