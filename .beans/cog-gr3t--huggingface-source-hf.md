---
# cog-gr3t
title: HuggingFace source (hf://)
status: completed
type: task
priority: high
created_at: 2026-04-22T20:23:25Z
updated_at: 2026-04-24T15:35:48Z
parent: cog-66gt
blocked_by:
    - cog-n2w1
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

- [x] Create `pkg/model/weightsource/huggingface.go`
- [x] Implement URI parsing (org/repo + optional @ref)
- [x] Implement HF Hub API client (list files, resolve ref → commit sha)
- [x] Implement `Inventory` (API call + LFS pointer digest extraction)
- [x] Implement `Open` (streaming download with context + retry)
- [x] Implement `NativeBlobRef` (stub for v1) — dropped, not in interface
- [x] Register scheme in `For`
- [x] Auth via env vars
- [x] Unit tests with mocked HF API
- [x] Integration test with a small public HF repo — skipped; mock coverage is thorough (all code paths + response shapes verified once against nvidia/parakeet-tdt-0.6b-v3). Real-world validation will happen organically on first use; add a targeted regression test if anything breaks.

## Reference

- `plans/2026-04-22-managed-weights-import-and-local-run-design.md` §1
- `specs/weights.md` fingerprint format (`commit:<sha>`)

## Research notes (2026-04-23)

Session goal was to scope cog-gr3t. Key findings and decisions below.

### Blockers are not real

Listed as blocked_by: cog-n2w1 (completed), cog-3p4a (plan/execute split, todo), cog-gbse (FileWeightStore, todo).

- **cog-n2w1** is done. Source interface with Inventory + Open landed. cog-qma1 also landed (manifest digest canonicalization), so the OCI side is stable.
- **cog-3p4a** is not a true prerequisite. cog-3p4a is about resumable, per-layer, disk-constrained imports (Kimi K2.5 use case). For a straightforward HF import where the user has enough free disk for the cache, the existing pipeline works: Source.Inventory + Source.Open already stream per-file. The plan/execute split is additive.
- **cog-gbse** is about `cog predict` materialization, not import. Unrelated to the Source side.

**Recommendation:** unblock cog-gr3t. The design pressure of building a remote source now will also inform cog-3p4a and cog-gbse designs before they lock in.

### Hub tree API response shape (verified)

`GET /api/models/{repo}/tree/{ref}?recursive=true` returns one object per entry:

```json
{"type":"file","oid":"2d98cb...","size":2508311120,
 "lfs":{"oid":"3a2026...","size":2508311120,"pointerSize":135},
 "xetHash":"bb353e...","path":"model.safetensors"}
```

- `oid` = git blob sha1 (useless for content integrity)
- `lfs.oid` = sha256 of the full file. **Present on both LFS and xet-tracked files.** This is the critical discovery: xet files still expose sha256 via the `lfs` field, so for inventory purposes xet is transparent.
- `xetHash` = xet content address (ignore for v1)
- Inline files (tracked in git, not LFS/xet) have **no content digest field at all**

Verified against `nvidia/parakeet-tdt-0.6b-v3` — the `lfs.oid` values exactly match the fixture digests for `model.safetensors`, the .nemo, and `plots/asr.png`.

### Inline file digest resolution

Tried HEAD on the resolve endpoint for inline files: `x-linked-etag` returns git sha1, not sha256. So we can't avoid fetching inline files to compute sha256.

Fine in practice — inline files are configs, tokenizer JSON, etc. Small. Fetch once during Inventory, hash while reading, record. Bounded concurrent pool.

### Download: xet vs LFS is transparent

`GET /{repo}/resolve/{ref}/{path}` issues a 302 to the appropriate backend (cas-bridge.xethub.hf.co for xet, cdn-lfs.huggingface.co for LFS, or serves inline directly). **We don't need a separate xet code path.** The HTTP redirect handles everything.

### Auth

`HF_TOKEN` env var → `Authorization: Bearer`. `HUGGING_FACE_HUB_TOKEN` as fallback. No token file lookup for v1.

### Scope audit: what FileSource consumers rely on

`rg 'weightsource\.' pkg/` turned up:

- `For(uri, projectDir)` is the only construction path in production code
- `Inventory(ctx)` + `Open(ctx, path)` + `.Fingerprint` are the only interface methods called
- `*FileSource` is type-asserted **only in `source_test.go`** (one test, checks that `For("file://...")` returns the right concrete type)
- Nothing in production code assumes a local path. `FileSource.Dir()` is not called outside the `weightsource` package.

Interface is already opaque. hf:// should plug in cleanly.

### Design

**File:** `pkg/model/weightsource/huggingface.go` (alongside `file.go`).

**URI forms:**
- `hf://{org}/{repo}` — follows main
- `hf://{org}/{repo}@{ref}` — ref is any branch, tag, or 40-char sha
- Canonical normalized form stored in lockfile: `hf://{org}/{repo}@{sha}` — always pin to resolved commit

**Hub client:** minimal HTTP client in-tree. Three endpoints:
- `GET /api/models/{repo}/revision/{ref}` — resolve ref → commit sha
- `GET /api/models/{repo}/tree/{ref}?recursive=true` — file list
- `GET /{repo}/resolve/{sha}/{path}` — stream one file (handles xet/LFS/inline via redirect)

No new Go dependencies. Auth via env vars.

**Source methods:**

- `Inventory(ctx)`:
  1. Resolve ref → full commit sha
  2. Fetch tree recursively
  3. For each entry with `lfs.oid`: use it as the content digest (free)
  4. For each inline entry: fetch via resolve URL, hash while streaming, record sha256 (bounded concurrency, e.g. 4)
  5. Fingerprint = `commit:<sha>`
  6. Return Inventory sorted by path, same as FileSource

- `Open(ctx, path)`: stream from resolve URL, respect context cancellation, retry on 5xx/network errors with backoff.

**Scheme dispatch:** add `case "hf":` in `For()` in `source.go`.

**Testing:**
- `httptest.Server` fixture mocking the three endpoints
- Cover: ref resolution, LFS digest path, inline digest path (fetch + hash), auth header passthrough, context cancellation, 5xx retry, malformed tree response
- Consider one opt-in integration test against a small real public HF repo, gated by `HF_INTEGRATION=1` or similar

### Interface stress points to watch

Building hf:// will exercise parts of the design that file:// didn't:

1. **Inventory does real I/O.** For file:// it walks a local dir (fast but not trivial). For hf:// it's a tree API call plus N GETs. Context propagation and error reporting from Inventory need to be solid. Will shake out any call sites that assume Inventory is cheap.
2. **Open is high-latency, streaming, retryable.** Will exercise whether the packer's tar-writing loop handles slow readers and mid-stream failures gracefully.
3. **No local path.** Confirmed above that no production code reaches for one.

If any of these surface real issues, capture as follow-up beans rather than expanding cog-gr3t's scope.

### Out of scope for v1 (unchanged from original bean)

- xet-specific optimization (we rely on resolve endpoint's redirect)
- `NativeBlobRef` — return `(BlobRef{}, false)`
- HF auth beyond env vars (no `~/.cache/huggingface/token`)
- s3://, http:// sources

## Summary of Changes

Implemented `hf://` source scheme for HuggingFace Hub weight imports.

### New files
- `pkg/model/weightsource/huggingface.go` — `HFSource` implementing the `Source` interface (Inventory + Open)
- `pkg/model/weightsource/huggingface_test.go` — comprehensive tests with httptest mock of Hub API

### Modified files
- `pkg/model/weightsource/source.go` — added `case HFScheme:` to `For()` dispatch, updated error message and comments
- `pkg/model/weightsource/source_test.go` — updated test table for hf:// now being a supported scheme
- `pkg/model/weightsource/fingerprint.go` — updated stale package doc
- `go.mod` / `go.sum` — added `hashicorp/go-retryablehttp`

### Design decisions
- **Retry via go-retryablehttp**: custom `CheckRetry` retries 5xx and 429, treats other 4xx as permanent with specific error messages (401/403/404)
- **Concurrency via errgroup**: inline file hashing uses `errgroup.WithContext` + `SetLimit(4)`, consistent with the rest of the codebase
- **Resolved ref pinning**: Inventory resolves the ref to a commit SHA and stores it on the source; Open uses the resolved SHA to prevent content drift between Inventory and Open
- **No new Go dependencies besides go-retryablehttp**: minimal HTTP client for the three Hub API endpoints needed (revision, tree, resolve)
- **NativeBlobRef dropped**: not in the interface, deferred per bean scope
