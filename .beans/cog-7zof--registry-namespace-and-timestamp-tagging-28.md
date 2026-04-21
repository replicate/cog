---
# cog-7zof
title: Registry namespace and timestamp tagging (§2.8)
status: todo
type: task
priority: normal
created_at: 2026-04-21T00:01:06Z
updated_at: 2026-04-21T17:01:35Z
parent: cog-66gt
---

Current code pushes weight tags to the model repo as `<model>:weights-<name>-<12hex>`. The spec defines a different layout: dedicated per-weight repositories with timestamp tags.

## Todo

- [ ] Push weight manifests to `<model>/weights/<name>` repository instead of model repo
- [ ] Tag with ISO 8601 compact timestamp (`YYYYMMDDTHHMMSSZ`, e.g. `20260416T172707Z`)
- [ ] Update `WeightPusher` tag generation logic
- [ ] Update `BundlePusher` / `IndexBuilder` to reference the new repository in index descriptors
- [ ] Update lockfile to record the new tag format
- [ ] Update `cog weights inspect` to resolve tags from new namespace
- [ ] Do NOT create `:latest` tags (spec: lockfile records digest, timestamps are for human listing)
- [ ] Verify existing `cog weights push` and `cog push` flows work with the new layout

## Context

Spec §2.8: `<model>/weights/<name>:<ts>` layout. Timestamp answers "when was this version imported?" Source-specific identifiers tracked in cog.yaml and lockfile, not in the artifact tag. This is a breaking change from the current format but nothing is in production yet.
