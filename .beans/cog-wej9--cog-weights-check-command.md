---
# cog-wej9
title: cog weights check command
status: todo
type: task
priority: normal
created_at: 2026-04-17T21:31:52Z
updated_at: 2026-04-21T17:01:35Z
parent: cog-66gt
blocked_by:
    - cog-4fg4
---

Implement `cog weights check`. Validates that every weight recorded in `weights.lock` is present in the registry with the expected manifest digest.

Behavior:
- For each weight: compute the expected tag with `model.WeightTag(name, lock.Digest)`, HEAD the registry manifest
- Compare the remote manifest digest with `lock.Digest`
- Exit 0 if all match, non-zero if any mismatch or missing
- Print a summary (name, tag, status) per weight
- `--json` for CI-friendly output

Split out from xv45 (cog weights check/list/inspect commands). The `inspect` slice was absorbed by 4fg4; `list` is its own bean. This is `check` alone.

`--source` mode (compare source fingerprint for upstream drift) is NOT in scope here — it depends on s5fy's source fingerprinting work. Add the flag only once s5fy lands.

Existing code to reuse:
- `resolveWeightsByTag` / `fetchRemoteWeight` pattern in `pkg/cli/weights_inspect.go` (HEAD vs GetImage — for `check` a HEAD is enough; use `reg.GetDescriptor(ctx, tagRef)` instead of `GetImage`)
- `model.WeightTag(name, digest)` for tag construction

Reference: plans/2026-04-16-managed-weights-v2-design.md §3 (User-facing commands)
