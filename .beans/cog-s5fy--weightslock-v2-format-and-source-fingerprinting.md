---
# cog-s5fy
title: weights.lock v2 format and source fingerprinting
status: todo
type: task
priority: normal
created_at: 2026-04-17T19:27:20Z
updated_at: 2026-04-21T17:01:35Z
parent: cog-66gt
blocked_by:
    - cog-2gv9
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



## Partial progress (4fg4 + simplification pass, 2026-04-17)

4fg4 established the v1 lockfile base format. What is now done:

- [x] `version` field (value `"v1"`)
- [x] Per-weight entry: `name`, `target`, `digest` (assembled manifest digest), `layers`
- [x] Per-layer entry: `digest`, `size`, `mediaType`, `annotations` (carries `run.cog.weight.content`, `run.cog.weight.file`, `run.cog.weight.size.uncompressed`)
- [x] Lockfile shape matches spec §3.6 verbatim — same struct can be serialized straight into `/.cog/weights.json` (this simplifies 1pm2)
- [x] `ParseWeightsLock` rejects unknown versions; no migration path for the pre-release v0 shape
- [x] `WeightsLock.Upsert` / `WeightsLock.FindWeight` helpers for caller use
- [x] Builder-side: Build is a no-op on disk when the cache-hit entry is unchanged (prevents `cog weights push` from churning the lockfile on every invocation)

What this bean still needs to deliver:

- [ ] Top-level per-weight provenance fields: `sourceURI`, `sourceFingerprint`, `importedAt`
- [ ] Source fingerprint scheme prefixes (`commit:<sha>`, `etag:<value>`, `sha256:<hash>`, `md5:<hash>`, `timestamp:<rfc3339>`)
- [ ] Per-layer `sizeUncompressed` as a top-level field (currently lives in `annotations["run.cog.weight.size.uncompressed"]` — spec requires promotion for ergonomics)
- [ ] Per-layer `content` + `file` as top-level fields (same reasoning)
- [ ] Enable `cog weights check --source` (requires fingerprints to be readable from the lockfile)

The easy half of this bean has been absorbed. Remaining work is provenance + field promotion + fingerprint resolvers per source scheme.
