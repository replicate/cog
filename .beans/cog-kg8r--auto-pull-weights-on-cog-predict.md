---
# cog-kg8r
title: Auto-pull weights on cog predict
status: todo
type: task
priority: low
created_at: 2026-04-22T20:24:44Z
updated_at: 2026-04-22T20:25:30Z
parent: cog-kgd7
blocked_by:
    - cog-40ed
    - cog-xhpw
---

When `cog predict` / `cog run` finds weights missing from the `WeightStore`, auto-pull them (TTY: prompt; non-TTY: error). Polish on the v1 behavior, which requires an explicit `cog weights pull`.

## Why

Smoother first-run UX. Right now (v1): `cog predict` errors with `"Weights not cached locally. Run 'cog weights pull' first."` This bean adds the convenience of auto-pulling when the user is interactive.

## Scope

- TTY: prompt `"Weights not cached (X GB, Y layers). Pull now? [Y/n]"` and run the equivalent of `cog weights pull` inline.
- Non-TTY: continue with the v1 error. Exit 1. Surface the pull command suggestion.
- Respect `--no-auto-pull` (or similar) flag for explicit opt-out even in TTY.
- Respect the existing `--from-registry` / `--from-source` flags from `cog weights pull` (pass through).

## Out of scope

- Changing v1 behavior for any code path other than `cog predict` / `cog run`.

## Dependencies

Blocked by:
- Wire cog predict to WeightStore.Mount
- Wire cog weights pull to WeightStore.Fetch

## Todo

- [ ] TTY detection + prompt
- [ ] Non-TTY: preserve current error path
- [ ] Flag plumbing for `--no-auto-pull` and pass-through of `--from-*`
- [ ] Tests with both TTY and non-TTY
