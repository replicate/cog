---
# cog.md-managed-weights-v0.0.2-2gv9
title: Wire cog weights import end-to-end (file:// source)
status: completed
type: task
priority: high
created_at: 2026-04-17T19:27:01Z
updated_at: 2026-04-17T21:32:29Z
parent: cog.md-managed-weights-v0.0.2-9qcd
blocked_by:
    - cog.md-managed-weights-v0.0.2-b2mv
---

Connect the tar packing engine and manifest push into a working cog weights import command for local directory sources.

Flow: read cog.yaml weights stanza -> resolve source directory (file:// or bare path) -> pack into tar files on disk -> build v1.Layer objects from tars -> push layers to registry via HTTP -> push manifest -> write weights.lock.

No Docker daemon involvement. Weights go from disk to registry directly via cog's custom HTTP push (WriteLayer + PushImage).

Minimal viable path:
- Parse the v2 weights stanza from cog.yaml (name, target, source.uri for local paths)
- Call tar packing engine on the resolved directory
- Call manifest push with the packed layers
- Write weights.lock with manifest digest, layer digests, sizes, media types
- Skip: Docker daemon loading, HF/S3/HTTP sources, include/exclude filters

This is the milestone where we can point at a real weight artifact in a registry and say 'infra, go'. The unhidden cog weights import command replaces the v0 cog weights build + cog weights push.

Reference: plans/2026-04-16-managed-weights-v2-design.md §3, existing code in pkg/cli/weights.go (hidden v0 commands)



## Summary of Changes (4fg4, 2026-04-17)

End-to-end wiring for v1 weight import is done. The full flow now works:

1. Parse the v1 weights stanza from `cog.yaml` — no change required, existing `config.WeightSource` is a directory source.
2. `WeightBuilder.Build()` resolves the directory, calls `Pack()` (v1 multi-layer packer from 0iel), writes tars to `<projectDir>/.cog/weights-cache/<name>/` with content-addressed filenames.
3. `WeightPusher.Push()` uploads layers concurrently via `registry.WriteLayer` (existing multipart+retry path), then pushes the v1 manifest via `registry.PushImage`.
4. `weights.lock` captures per-weight manifest digest + per-layer digest/size/mediaType/annotations (spec §3.6 shape).
5. `cog weights push` is a no-op on the lockfile when entries are unchanged — safe to run repeatedly.

No Docker daemon involvement. Weights go directly from disk to registry via cog's custom HTTP push.

What this bean did NOT do (tracked in separate beans):
- Unified `cog weights import` command (currently `build` + `push` as separate hidden subcommands) — new bean `cog.md-managed-weights-v0.0.2-5lg2`
- Source fingerprinting — s5fy
- Non-file:// sources — 9vfd
- Include/exclude filters — 6wm0

The handoff checkpoint (0wma) can now be verified using `cog weights build` + `cog weights push` in place of the eventual `cog weights import`.
