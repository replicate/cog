---
# cog.md-managed-weights-v0.0.2-am6m
title: Transfer concurrency and retry
status: todo
type: task
priority: low
created_at: 2026-04-17T19:28:32Z
updated_at: 2026-04-17T19:28:32Z
parent: cog.md-managed-weights-v0.0.2-9qcd
blocked_by:
    - cog.md-managed-weights-v0.0.2-2gv9
---

Add configurable concurrency and retry semantics to weight layer transfers.

Upload (import) and download (pull):
- 4 layers in parallel by default
- --concurrency=N override
- Retry up to 3 times with exponential backoff (1s, 4s, 16s)
- Transient errors (429, 502, 503, 504, connection reset) trigger retry
- 4xx client errors (except 429) fail immediately
- Per-layer progress output showing which layer is retrying

Existing retry infrastructure in pkg/registry/registry_client.go (5 attempts, 2s initial). Align or extend.

Reference: plans/2026-04-16-managed-weights-v2-design.md §3 (Transfer concurrency)
