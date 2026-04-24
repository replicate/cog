---
# cog-u17e
title: 'Parallel Pull: concurrent weight/layer downloads'
status: todo
type: task
priority: normal
created_at: 2026-04-24T00:59:45Z
updated_at: 2026-04-24T00:59:45Z
parent: cog-kgd7
---

v1 `Manager.Pull` is fully sequential — one weight at a time, one layer at a time within each weight. This is a correctness-first default: simple error flow, totally-ordered progress events, no interleaving in the CLI output.

## Why parallelize

Sequential pulls are IO-bound on a single HTTP stream per layer. A model with N weights or N layers pulls in N × single_stream_time. For a parakeet-sized weight (3 layers, ~4.5 GB) on a typical connection that's a real wall-clock cost.

## Scope

Add bounded parallelism on two axes:

1. **Across weights**: N weights pulled concurrently.
2. **Across layers within a weight**: N layers pulled concurrently.

Within a layer remains sequential (tar stream is inherently ordered).

## Design sketch

- New env var `COG_PULL_CONCURRENCY` (default 4 or 8; benchmark to pick), mirroring `COG_PUSH_CONCURRENCY` in `pkg/model/weight_pusher.go`.
- Use `golang.org/x/sync/errgroup` in `Manager.Pull` and `Manager.pullEntry`.
- `OnEvent` in `PullOptions` becomes hot-path: either serialize emissions behind a mutex inside the Manager, or document that the callback must be goroutine-safe. Prefer Manager-side serialization so the public contract stays "called synchronously in order" — CLI renderers don't have to care.
- CLI (`pkg/cli/weights_pull.go`) keeps its current printing code; ordering is the Manager's problem.
- `FileStore` is already safe for concurrent writers (atomic rename + idempotent PutFile across the same digest).

## Benchmarks

Add a benchmark or ad-hoc test comparing sequential vs concurrent pulls against a local registry fixture with a multi-layer weight. Worth capturing:

- Small (10 files, 10 MB) — overhead should be negligible either way.
- Medium (50 files, 500 MB) — concurrency starts to win.
- Large (100+ files, 5 GB) — expected headline improvement.

## Non-goals

- Within-layer parallelism (HTTP range requests). go-containerregistry doesn't expose the primitives cheaply and the per-layer bottleneck is usually bandwidth, not latency.
- Auto-detecting optimal concurrency. The `COG_PULL_CONCURRENCY` knob is explicit and matches the push-side pattern.

## Todo

- [ ] Add `GetPullConcurrency()` in `pkg/model/` (or similar) reading `COG_PULL_CONCURRENCY`
- [ ] Parallelize across weights in `Manager.Pull`
- [ ] Parallelize across layers in `Manager.pullEntry`
- [ ] Serialize `OnEvent` emissions behind a Manager-owned mutex
- [ ] Benchmark against a local registry fixture
- [ ] Document the env var in `docs/` (weight-management section) and regenerate `docs/llms.txt`

## Dependencies

- cog-xhpw (sequential Pull landed)

## Reference

- `pkg/model/weight_pusher.go` — `GetPushConcurrency()` pattern
- `pkg/cli/weights.go:190-192` — existing concurrent-push usage
