---
# cog-4lgn
title: Index descriptor missing run.cog.weight.size.uncompressed
status: completed
type: bug
priority: normal
created_at: 2026-04-21T23:40:31Z
updated_at: 2026-04-24T06:02:28Z
parent: cog-66gt
blocked_by:
    - cog-s5fy
---

artifactType now appears on index weight descriptors after the fix, but run.cog.weight.size.uncompressed is absent. Likely LayerResult.UncompressedSize is zero at push time so the annotation gets skipped (guard at index_factory.go:83). The new lockfile work should fix the data flow — this bean is a reminder to verify the annotation appears after that lands.
