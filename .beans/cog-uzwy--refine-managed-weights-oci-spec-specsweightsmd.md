---
# cog-uzwy
title: Refine managed weights OCI spec (specs/weights.md)
status: completed
type: task
priority: high
created_at: 2026-04-20T17:07:58Z
updated_at: 2026-04-20T17:29:36Z
parent: cog-9qcd
---

Update specs/weights.md based on review:
- [x] Section 1: Add explicit exclusions (symlinks, whiteouts, device nodes) and frame order-invariance as a guarantee against overlay semantics
- [x] Section 2.4: Add artifactType on weight descriptors in OCI index alongside unknown/unknown platform
- [x] Section 3: Add design rationale for file-based protocol (§3.0) and recovery semantics (§3.5)
- [ ] Section 2.5: Evaluate registry namespace (spec vs current tag-based impl)

## Summary of Changes\n\nRefined specs/weights.md based on review against Friday's implementation. Added explicit content exclusions (§1.3), zstd support (§1.2, §2.1), artifactType on OCI index descriptors (§2.4), design rationale for file-based runtime protocol (§3.0), and recovery semantics (§3.5). Registry namespace (§2.5) left as open item for next iteration.
