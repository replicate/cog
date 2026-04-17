---
# cog.md-managed-weights-v0.0.2-s5fy
title: weights.lock v2 format and source fingerprinting
status: todo
type: task
priority: normal
created_at: 2026-04-17T19:27:20Z
updated_at: 2026-04-17T19:33:14Z
parent: cog.md-managed-weights-v0.0.2-9qcd
blocked_by:
    - cog.md-managed-weights-v0.0.2-2gv9
---

Extend the lockfile with source fingerprint tracking and full provenance metadata.

Note: the base v2 lockfile format (version field, per-layer digests/sizes/mediaTypes) is established in the e2e import task (2gv9) which writes a minimal lockfile. This task adds the provenance layer on top.

weights.lock v2 adds:
- version field ("1.0")
- Per-weight: source URI, sourceFingerprint, importedAt timestamp
- Per-layer: digest, size, sizeUncompressed, mediaType, content type, file path

Source fingerprint is a prefixed string identifying the source version at import time:
- commit:<sha> for HuggingFace repos (git commit)
- etag:<value> for HTTP/HTTPS
- sha256:<hash> for content hash fallback
- md5:<hash> for S3 (ETag = MD5)
- timestamp:<rfc3339> for Last-Modified fallback

Enables cog weights check --source to detect upstream changes without re-downloading.

Existing lockfile: pkg/model/weights_lock.go (v0 format with single-file entries). Needs migration to layer-based format.

Reference: plans/2026-04-16-managed-weights-v2-design.md §2 (Lockfile section)
