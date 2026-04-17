---
# cog.md-managed-weights-v0.0.2-am6m
title: Transfer concurrency and retry
status: todo
type: task
priority: low
created_at: 2026-04-17T19:28:32Z
updated_at: 2026-04-17T21:33:02Z
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



## Partial progress (4fg4, 2026-04-17)

Concurrency + retry infrastructure is wired through v1 already:

- **Upload concurrency**: `WeightPusher.Push` spins up `GetPushConcurrency()` goroutines per weight (`pkg/model/weight_pusher.go` `pushLayersConcurrently`). Override via `WeightPushOptions.Concurrency` or env `COG_PUSH_CONCURRENCY`.
- **Bundle-level concurrency**: `BundlePusher.pushWeights` also caps at `GetPushConcurrency()` — multiple weights push in parallel. (The reviewer of 4fg4 flagged that outer × inner concurrency can yield N² effective layer concurrency — intentional or revisit here.)
- **Retry**: `WeightPushOptions.RetryFn` threads through `registry.WriteLayer` to `pkg/registry/registry_client.go`, which already implements 5-attempt retry with 2s initial backoff.
- **Progress**: per-layer progress events (`WeightLayerProgress{WeightName, LayerDigest, Complete, Total}`) flow through to the CLI for `cog weights push` rendering.

## What remains

- [ ] **Align retry policy** with the plan (3 attempts, 1s/4s/16s backoff vs. current 5/2s/exponential). Decide whether to change the existing policy in `pkg/registry/registry_client.go` or document the divergence.
- [ ] **Classify transient vs. permanent errors** — confirm `429, 502, 503, 504, connection reset` retry; `4xx` (except 429) fail fast. May already match.
- [ ] **Download / pull side**: `cog weights pull` doesn't exist yet (kfvj) — re-home this bullet there once pull lands.
- [ ] Consider flattening bundle-level + layer-level errgroups into a single one for a true global `COG_PUSH_CONCURRENCY` cap.

Most of this is verification + small alignment, not new plumbing.
