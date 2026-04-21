---
# cog.md-managed-weights-v0.0.2-m4e8
title: OCI index assembly with weight manifests
status: completed
type: task
priority: critical
created_at: 2026-04-17T19:27:10Z
updated_at: 2026-04-21T16:31:29Z
parent: cog.md-managed-weights-v0.0.2-9qcd
blocked_by:
    - cog.md-managed-weights-v0.0.2-2gv9
---

Update cog push to assemble an OCI index that includes weight manifests from the lockfile.

When cog.yaml has a weights stanza:
- cog push produces an OCI image index (not a single manifest)
- Index contains: model image manifest (linux/amd64 platform) + weight manifest descriptors (unknown/unknown platform)
- Weight descriptors carry duplicated annotations from the weight manifest (run.cog.weight.name, .set-digest per spec §2.6)
- Remove COG_OCI_INDEX=1 env var gate -- presence of weights stanza triggers bundle format
- Weight manifests are already in the registry (from cog weights import); cog push references them by digest from weights.lock

Existing code: pkg/model/pusher.go (BundlePusher), pkg/model/index_factory.go (IndexBuilder). These handle v0 single-file weights; update to reference multi-layer weight manifests.

Reference: specs/weights.md §2.4



## Partial progress (4fg4, 2026-04-17)

The assembly path is done. `BundlePusher.Push` (in `pkg/model/pusher.go`) produces an OCI index containing:

- Model image manifest (linux/amd64 platform, from `PushOptions.Platform` default)
- Weight manifest descriptor per weight (`unknown/unknown` platform) with annotations: `run.cog.weight.name`, `run.cog.weight.set-digest` (needs update to match spec §2.6 -- currently still emits old `run.cog.reference.*` annotations)
- Weight manifest digests computed by the push pipeline (not read from the lockfile — 4fg4 routes them through `WeightPushResult.Descriptor`)

`IndexBuilder` (in `pkg/model/index_factory.go`) writes only v1 `run.cog.*` annotations now (the v0 `vnd.cog.*` constants were deleted — no reader existed for them).

## What remains

- [x] **Remove the `COG_OCI_INDEX=1` env gate**. Currently `Resolver.Build` checks `opts.OCIIndex` before invoking the weight builder; callers set this from the env var. Replace with: trigger bundle format when `cfg.Weights` is non-empty. Touches `pkg/model/resolver.go`, `pkg/cli/push.go`, `pkg/model/model.go` (`Model.OCIIndex` field), and related tests (`pkg/model/pusher_test.go`, `resolver_test.go`).
- [x] **Integration test** — covered by existing `oci_bundle_push.txtar` and `oci_bundle_inspect.txtar` integration tests.

Once the gate is gone this bean is done and `hc35` checkpoint can be verified.

## Summary of Changes\n\nRemoved the `COG_OCI_INDEX=1` env var gate. Presence of `weights:` in `cog.yaml` now triggers OCI bundle format automatically.\n\n- Deleted `pkg/model/format.go` and `format_test.go` (env var reader)\n- Removed `OCIIndex bool` from `Model` and `BuildOptions` structs\n- `Resolver.Build()` gates on `len(cfg.Weights) > 0` alone\n- `Resolver.Push()` uses `m.IsBundle()` (artifact presence) instead of flag\n- Updated all unit tests, 3 integration tests, and examples
