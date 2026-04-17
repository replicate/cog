---
# cog.md-managed-weights-v0.0.2-2gv9
title: Wire cog weights import end-to-end (file:// source)
status: todo
type: task
priority: high
created_at: 2026-04-17T19:27:01Z
updated_at: 2026-04-17T19:38:23Z
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
