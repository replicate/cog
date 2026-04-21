---
# cog-6b5a
title: cog weights list command
status: todo
type: task
priority: normal
created_at: 2026-04-17T21:31:40Z
updated_at: 2026-04-21T17:01:35Z
parent: cog-66gt
blocked_by:
    - cog-4fg4
---

Implement `cog weights list`. Reads `cog.yaml` + `weights.lock` and prints a table of configured weights with name, target, size (sum of layer sizes), layer count, and manifest digest.

Flags:
- `--remote` — query registry for what's actually stored (run the same lookup as `inspect`, list entries without local state, mark as remote-only)
- `--json` — structured output

Split out from xv45 (cog weights check/list/inspect commands). The `inspect` slice of xv45 was absorbed by 4fg4; `check` is tracked separately. This is the `list` slice on its own.

Existing v1 types to reuse:
- `model.WeightLockEntry` / `model.WeightLockLayer` for local state
- `fetchRemoteWeight` helper in `pkg/cli/weights_inspect.go` for remote lookups
- `formatSize` helper in `pkg/cli/weights.go`

Reference: plans/2026-04-16-managed-weights-v2-design.md §3 (User-facing commands)
