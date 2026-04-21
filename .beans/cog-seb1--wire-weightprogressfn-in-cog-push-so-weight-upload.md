---
# cog-seb1
title: Wire WeightProgressFn in cog push so weight uploads show progress
status: completed
type: bug
priority: high
created_at: 2026-04-17T23:21:19Z
updated_at: 2026-04-17T23:31:51Z
parent: cog-9qcd
---

When `cog push` runs the v1 managed-weights flow, `BundlePusher.Push` calls `pushWeights` → `WeightPusher.Push` for every weight artifact after the image push completes. The weight upload can be multi-GB per artifact and take minutes, but there is **zero progress output** during this phase — the CLI appears hung.

## Repro

With a multi-GB weights directory and a remote registry:

```bash
cd examples/test-weights
COG_OCI_INDEX=1 cog push registry.cloudflare.com/<scope>/test-weights
```

Output ends at:
```
⚙  1 weight artifact(s)
⚙  Pushing image registry.cloudflare.com/<scope>/test-weights-v1...
… docker push progress …
latest: digest: sha256:… size: 3064
```

Then the CLI hangs silently for 5–30 minutes depending on upload bandwidth while 5 GB of tar layers upload to the registry.

## Root cause

`pkg/cli/push.go:126` passes `ImageProgressFn` and `OnFallback` to `model.PushOptions` but leaves `WeightProgressFn` unset. `BundlePusher.pushWeights` (`pkg/model/pusher.go:122`) passes the nil callback through to `WeightPusher.Push`, so every `v1.Update` from `registry.WriteLayer` goes nowhere.

## Proposed fix

Wire `WeightProgressFn` in `pkg/cli/push.go` analogously to `ImageProgressFn`:

1. Before the call to `resolver.Push`, print `console.Infof("\nPushing weights...")` (or defer it to the first progress tick, to avoid printing if there are no weights).
2. Set `WeightProgressFn` to a callback that feeds into the same `docker.NewProgressWriter` `pw` instance used for image layers, with per-layer keys like `"<weight-name>/<short-digest>"` so progress bars don't collide with image-layer bars.
3. Make sure the progress writer's output is coherent when image push has just finished: currently `pw.Close()` is called when `OnFallback` fires, but not between the image phase and the weight phase. Decide whether to re-use the same `ProgressWriter` or create a fresh one for weights.

## Acceptance

- Running `cog push` with managed weights prints per-layer progress for each weight layer upload.
- Total weight push duration is unchanged (purely cosmetic fix).
- `cog weights push` standalone already prints a brief line per artifact; verify the bundle path is at least as informative.

## Out of scope

- A better progress display (e.g. aggregated per-artifact bar instead of per-layer). Per-layer bars are what image push uses today; matching that is sufficient.



## Summary of Changes

Wired `WeightProgressFn` in `pkg/cli/push.go`:

- Added `sync` import and a `sync.Once` so `"Pushing weights..."` is printed at most once, the first time a weight progress event arrives. This avoids printing the header when there are no weights or when the push errors out before the weight phase.
- Added a `WeightProgressFn` callback that feeds per-layer progress into the same `docker.NewProgressWriter` used for image layers. Layer id is `"<weight-name>/<short-digest>"` so progress bars for multiple weight artifacts uploading concurrently don't collide.
- Truncate digest to 12 hex chars for display, matching the image-layer convention.

Verified end-to-end in an interactive terminal: per-layer progress bars now render during the weight push phase. In non-TTY contexts (piped output), `jsonmessage.DisplayJSONMessagesStream` suppresses partial updates by design — that's consistent with how image-layer progress already behaves and is out of scope.

Tests: `go test ./pkg/cli/... ./pkg/model/...` green. `mise run lint:go` clean for the touched file.
