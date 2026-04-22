---
# cog-gr3t
title: HuggingFace source (hf://)
status: todo
type: task
priority: high
created_at: 2026-04-22T20:23:25Z
updated_at: 2026-04-22T20:43:45Z
parent: cog-66gt
blocked_by:
    - cog-n2w1
    - cog-3p4a
    - cog-gbse
---

Implement the `hf://` source scheme using the new Source interface (Inventory + Open + NativeBlobRef). Unblocks the disk-constrained import story for HuggingFace-hosted weights — the primary use case after file://.

## Why

HF is the most common weight source. The new Source interface was designed with HF's capabilities in mind: LFS/xet pointer files carry file sha256s, so `Inventory` is free (no payload downloads). `Open` streams from the LFS/CDN URL.

## Scope

### URI syntax

- `hf://<org>/<repo>` — follows main, fingerprint captures HEAD commit at import time.
- `hf://<org>/<repo>@<tag>` — follows tag, fingerprint captures resolved commit sha.
- `hf://<org>/<repo>@<sha>` — pinned to commit.

### Inventory

- Call HuggingFace Hub API to list files in the repo at the resolved ref.
- For each file: `Path`, `Size`, `Digest` from LFS pointer sha256 (if LFS/xet tracked) or compute from raw content (if small and inline in git).
- `Fingerprint` is `commit:<full-sha>` (resolved from the ref at inventory time).
- No payload downloads.

### Open

- Return an `io.ReadCloser` that streams the file from the HF CDN. Respect context cancellation. Reasonable retry on transient 5xx/network errors.

### NativeBlobRef

- Return `(BlobRef{}, false)` for v1. Future optimization: HF LFS objects live on S3 with predictable URLs — could be used for cross-source blob mount if target registry supports it. Not v1.

### Auth

- Honor `HF_TOKEN` / `HUGGING_FACE_HUB_TOKEN` env vars.
- Anonymous for public repos.

## Scope (code)

- `pkg/model/weightsource/huggingface.go` (new file).
- Register in `For(uri)` switch in `pkg/model/weightsource/source.go`.
- HF Hub API client (use an existing Go library if one exists, otherwise minimal HTTP client — the API surface we need is small).
- Tests: mock HF API, cover inventory with/without ref suffix, fingerprint stability, Open streaming, auth header handling.

## Out of scope

- xet-specific handling (if xet uses a different API path than LFS — research during implementation; may need a follow-up bean).
- Native blob ref optimization (future).
- s3://, http:// sources (separate bean).

## Dependencies

Blocked by:
- Source interface redesign (Inventory + Open + NativeBlobRef)

## Todo

- [ ] Create `pkg/model/weightsource/huggingface.go`
- [ ] Implement URI parsing (org/repo + optional @ref)
- [ ] Implement HF Hub API client (list files, resolve ref → commit sha)
- [ ] Implement `Inventory` (API call + LFS pointer digest extraction)
- [ ] Implement `Open` (streaming download with context + retry)
- [ ] Implement `NativeBlobRef` (stub for v1)
- [ ] Register scheme in `For`
- [ ] Auth via env vars
- [ ] Unit tests with mocked HF API
- [ ] Integration test with a small public HF repo (if feasible — may need manual verification)

## Reference

- `plans/2026-04-22-managed-weights-import-and-local-run-design.md` §1
- `specs/weights.md` fingerprint format (`commit:<sha>`)
