---
# cog-9vfd
title: Additional source schemes (hf://, s3://, http://)
status: todo
type: task
priority: normal
created_at: 2026-04-17T19:28:42Z
updated_at: 2026-04-22T01:27:10Z
parent: cog-66gt
blocked_by:
    - cog-2gv9
    - cog-s5fy
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



## Dependency on s5fy (2026-04-21)

s5fy establishes the `Source` interface (`Fetch` + `Fingerprint` methods), the `SourceFor(uri)` scheme switch, and `FileSource` as the reference implementation. This bean adds hf/s3/http implementations of the same interface — no interface redesign needed.

Each new source type:
- Implements `Source.Fetch` to materialize a local directory (download → temp dir)
- Implements `Source.Fingerprint` with a scheme-native identifier (`commit:<sha>` for hf, `md5:<etag>` for s3, `etag:<value>` or `timestamp:<rfc3339>` for http)
- Registers in the `SourceFor` switch statement

The URI scheme supports `@<ref>` suffixes for pinning:
- `hf://org/repo` → follows main, fingerprint captures HEAD commit at import time
- `hf://org/repo@v1.2.0` → follows tag, fingerprint captures the resolved commit SHA
- `hf://org/repo@abc123` → pinned to commit

The packer and lockfile are unchanged — the `Source` abstraction means adding a new scheme only touches the source implementation and the scheme switch.
