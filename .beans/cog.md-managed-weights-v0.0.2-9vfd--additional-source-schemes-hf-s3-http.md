---
# cog.md-managed-weights-v0.0.2-9vfd
title: Additional source schemes (hf://, s3://, http://)
status: todo
type: task
priority: low
created_at: 2026-04-17T19:28:42Z
updated_at: 2026-04-17T19:28:42Z
parent: cog.md-managed-weights-v0.0.2-9qcd
blocked_by:
    - cog.md-managed-weights-v0.0.2-2gv9
---

Extend the weight import pipeline to support non-local sources.

Source schemes:
- hf://<org>/<repo> -- HuggingFace Hub API with LFS support. Source fingerprint: commit:<sha>.
- s3://<bucket>/<key> -- AWS SDK. Source fingerprint: md5:<etag>.
- http:// / https:// -- Stream to temp dir. Source fingerprint: etag:<value> or timestamp:<last-modified>.
- oci://<registry>/<repo>@sha256:... -- Reference another weight. Cross-repo blob mount if same registry.

Each scheme is a source resolver that produces a local directory (for v1 staged import). The tar packing engine operates on the resolved directory regardless of source.

Prioritize hf:// first (most common source for model weights).

Reference: plans/2026-04-16-managed-weights-v2-design.md §2 (source.uri), §6 (cross-repo)
