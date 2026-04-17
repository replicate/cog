---
# cog.md-managed-weights-v0.0.2-m4e8
title: OCI index assembly with weight manifests
status: todo
type: task
priority: high
created_at: 2026-04-17T19:27:10Z
updated_at: 2026-04-17T19:27:10Z
parent: cog.md-managed-weights-v0.0.2-9qcd
blocked_by:
    - cog.md-managed-weights-v0.0.2-2gv9
---

Update cog push to assemble an OCI index that includes weight manifests from the lockfile.

When cog.yaml has a weights stanza:
- cog push produces an OCI image index (not a single manifest)
- Index contains: model image manifest (linux/amd64 platform) + weight manifest descriptors (unknown/unknown platform)
- Weight descriptors carry duplicated annotations from the weight manifest (run.cog.weight.name, .target, .reference.type, .reference.digest)
- Remove COG_OCI_INDEX=1 env var gate -- presence of weights stanza triggers bundle format
- Weight manifests are already in the registry (from cog weights import); cog push references them by digest from weights.lock

Existing code: pkg/model/pusher.go (BundlePusher), pkg/model/index_factory.go (IndexBuilder). These handle v0 single-file weights; update to reference multi-layer weight manifests.

Reference: specs/weights.md §2.4
