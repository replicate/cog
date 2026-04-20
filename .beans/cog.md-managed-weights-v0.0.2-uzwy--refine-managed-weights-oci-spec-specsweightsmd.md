---
# cog.md-managed-weights-v0.0.2-uzwy
title: Refine managed weights OCI spec (specs/weights.md)
status: in-progress
type: task
priority: high
created_at: 2026-04-20T17:07:58Z
updated_at: 2026-04-20T17:11:44Z
parent: cog.md-managed-weights-v0.0.2-9qcd
---

Update specs/weights.md based on review:
- [x] Section 1: Add explicit exclusions (symlinks, whiteouts, device nodes) and frame order-invariance as a guarantee against overlay semantics
- [x] Section 2.4: Add artifactType on weight descriptors in OCI index alongside unknown/unknown platform
- [x] Section 3: Add design rationale for file-based protocol (§3.0) and recovery semantics (§3.5)
- [ ] Section 2.5: Evaluate registry namespace (spec vs current tag-based impl)
