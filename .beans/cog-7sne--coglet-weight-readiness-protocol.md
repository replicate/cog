---
# cog-7sne
title: 'Coglet: weight readiness protocol'
status: todo
type: task
priority: low
created_at: 2026-04-17T19:28:23Z
updated_at: 2026-04-23T22:23:25Z
parent: cog-kgd7
blocked_by:
    - cog-1pm2
---

Design and implement the runtime state protocol reader in coglet.

## Design questions (resolve before coding)

- Where in coglet's startup flow does weight readiness check hook in? Before worker subprocess spawn? After?
- Timeout strategy: fixed timeout per weight? Configurable? Based on total expected size?
- Health check interaction: does coglet report 'not ready' on the health endpoint while waiting for weights, or does it not start the HTTP server at all until weights are ready?
- How does coglet discover /.cog/weights.json? Hardcoded path check on startup, or config-driven?
- Error reporting: how does a weight readiness failure surface through the prediction API vs container logs?

On startup, if /.cog/weights.json exists:
1. For each weight entry, run the reader algorithm against <target>/.cog/:
   - stat ready -> proceed
   - stat failed -> read payload, surface error, fail startup
   - stat downloading -> poll with backoff, optionally report per-layer progress
   - manifest.json exists but no state file -> writer crash, fail
   - nothing exists -> wait with timeout
2. All weights ready -> call setup()
3. Any weight fails or times out -> fail with actionable error

This is Rust code in crates/coglet/. The protocol is specified in specs/weights.md §3.

Key: readers never write to .cog/. Pure observation. The hot path is a single stat() per weight.
