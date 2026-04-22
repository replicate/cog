---
# cog-kkfz
title: 'Additional source schemes: s3:// and http(s)://'
status: todo
type: task
priority: low
created_at: 2026-04-22T20:25:16Z
updated_at: 2026-04-22T20:25:31Z
parent: cog-66gt
blocked_by:
    - cog-n2w1
    - cog-gr3t
---

Split off from the original `cog-9vfd` to keep scope focused. `cog-9vfd` now covers hf:// only (the primary use case); this bean tracks s3 and http.

## Why

s3:// and http:// are secondary. hf:// covers the common model-weight flows. These are important for completeness and for users with custom weight hosting, but they don't block v1.

## Scope

### s3://

- URI: `s3://<bucket>/<prefix>/` (directory-like) or `s3://<bucket>/<key>` (single file; usually only useful if the key is a tarball — not fully supported in v1).
- `Inventory`: `ListObjectsV2` with the prefix, collect `Key`, `Size`, `ETag`. For ETag: MD5 if single-part upload, opaque identifier otherwise. Fingerprint format: `etag:<etag>` (bucket-level opaque identity).
- `Open`: `GetObject` streaming.
- Auth: standard AWS credential chain (env, IMDSv2, profile, etc.).
- `NativeBlobRef`: return `(BlobRef{}, false)` in v1.

### http(s)://

- URI: `https://example.com/path` (usually a single file — .tar, .tar.gz, .zip).
- Single-file http sources don't fit the "directory of weight files" model. Either:
  - Stage to a temp dir, extract, walk, hash (defeats the disk-constrained goal).
  - Error out for archives; only support http pointing at a single weight file (rare).
- `Inventory`: HEAD request for `Content-Length` + `ETag` + `Last-Modified`. Fingerprint: `etag:<value>` or `timestamp:<rfc3339>`.
- `Open`: GET with streaming.
- Auth: bearer token via env or `~/.netrc`.

### Registration

- Add both to `For(uri)` scheme switch in `pkg/model/weightsource/source.go`.

## Out of scope

- Extraction of tar/zip archives as part of an http source. If it's needed, do it as a pre-processing step (`curl | tar -x` into a dir, then use `file://`) rather than building tar extraction into cog.
- xet-specific handling in s3 (if HF xet objects move to S3 with specific metadata schemes).

## Dependencies

Blocked by:
- Source interface redesign
- HuggingFace source (proves the Inventory+Open pattern works for remote sources)

## Todo

- [ ] `pkg/model/weightsource/s3.go` (new file)
- [ ] `pkg/model/weightsource/http.go` (new file)
- [ ] Register both schemes in `For`
- [ ] Unit tests with mocked S3 API and HTTP server
- [ ] Document auth for both

## Reference

- `plans/2026-04-22-managed-weights-import-and-local-run-design.md` §1 (Sources table)
- Original scope was combined in `cog-9vfd`; that bean retitled to hf:// only.
