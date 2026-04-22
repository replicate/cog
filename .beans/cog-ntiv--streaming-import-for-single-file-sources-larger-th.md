---
# cog-ntiv
title: Streaming import for single-file sources larger than local disk
status: todo
type: task
priority: low
created_at: 2026-04-22T20:24:58Z
updated_at: 2026-04-22T20:25:31Z
parent: cog-66gt
blocked_by:
    - cog-3p4a
---

Support importing a single source file that's larger than the local disk has free space for. The plan/execute split already solves the multi-file case (pack one layer, push, delete); this bean handles the rare single-huge-file case where one file is bigger than available free space.

## Why

Current shape: pack a layer into `.cog/weights-cache/` → push → delete. If a single file within a layer is too big, we can't even fit the tar.

Rare at current model-weight sizes (framework-sharded weights are already in the 5-10 GB range), but becomes relevant if single safetensors files grow beyond 100+ GB or for unsharded archives.

## Scope

The OCI distribution spec supports chunked upload (`POST /blobs/uploads/` + `PATCH` chunks + `PUT ?digest=`). We'd stream source → tar writer → chunked upload, hashing both file content (SHA-256 per file) and tar blob (SHA-256 for the layer digest) on the fly. No on-disk tar; no on-disk file.

### Implementation sketch

- New `Pack` variant that takes an `io.Writer` instead of a `TempDir` and produces a streaming digest.
- New `registry.WriteLayerStreaming` that consumes an `io.Reader` in OCI-compliant chunks.
- Failure handling: OCI chunked uploads can be resumed, but state lives in the registry — our pending state file would need to track the upload URL + last-chunk digest.

## Out of scope (this bean)

- Not in v1. Track as a future enhancement.
- The 90% case is handled by the plan/execute split (one layer at a time, delete the tar after push).

## Dependencies

Blocked by:
- Plan/execute split (streaming would slot in as an alternative Executor strategy)

## Notes

Originally flagged as an open question in `plans/2026-04-16-managed-weights-v2-design.md` §4. Not urgent until we hit a concrete case where single-file-larger-than-disk breaks a real import.

## Reference

- `plans/2026-04-22-managed-weights-import-and-local-run-design.md` §6 (deferred)
