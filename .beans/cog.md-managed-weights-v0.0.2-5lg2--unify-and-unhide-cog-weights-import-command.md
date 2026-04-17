---
# cog.md-managed-weights-v0.0.2-5lg2
title: Unify and unhide cog weights import command
status: todo
type: task
priority: high
created_at: 2026-04-17T21:32:13Z
updated_at: 2026-04-17T21:32:13Z
parent: cog.md-managed-weights-v0.0.2-9qcd
blocked_by:
    - cog.md-managed-weights-v0.0.2-4fg4
---

The plumbing for `cog weights import` (directory source → packed layers → registry) is done as of 4fg4. Two separate hidden commands currently exist:

- `cog weights build` — packs the source directory and writes `weights.lock`
- `cog weights push` — pushes the packed layers + manifest to the registry

Bean 2gv9 calls for unifying these into a single `cog weights import` that performs both steps atomically and unhides the command.

Scope:
- [ ] Add `cog weights import [name...]` — runs build then push for the listed weights (or all if none specified)
- [ ] Unhide the `weights` command group (remove `Hidden: true`)
- [ ] Keep `build` and `push` as visible subcommands for advanced use (build without pushing, push after manual build)
- [ ] Update `docs/cli.md` via `mise run docs:cli`
- [ ] Update the 2026-04-16 design doc if the flag names drift

Out of scope (separate beans):
- Source fingerprinting (s5fy)
- Non-file:// source schemes (9vfd)
- Include/exclude filters (6wm0)

Split out from 2gv9. 2gv9's "end-to-end wiring" work is done; this is the user-facing surface polish.
