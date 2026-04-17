---
# cog.md-managed-weights-v0.0.2-0iel
title: Tar layer packing engine
status: completed
type: task
priority: high
created_at: 2026-04-17T19:26:39Z
updated_at: 2026-04-17T20:00:42Z
parent: cog.md-managed-weights-v0.0.2-9qcd
---

Implement the two-tier tar packing algorithm from specs/weights.md §1.2.

## Design decisions (resolved)

- **Package location**: pkg/model/ (packer.go). Keeps new code next to the existing weight builder/pusher it replaces. No new sub-packages.
- **Function signature**: `Pack(ctx, sourceDir, opts) ([]LayerResult, error)` — LayerResult has TarPath, Digest, Size, UncompressedSize, MediaType, Annotations.
- **Relationship to v0 WeightBuilder**: Parallel for now. Caller picks which to use. v0 removed when v2 is wired through.
- **Streaming vs buffered**: Writes tars to temp dir (configurable via opts). Returns file paths. Interface supports future streaming without API change.

## Implementation tasks

- [x] Resolve design decisions
- [x] Implement file classification (small vs large by bundle_file_max threshold)
- [x] Implement deterministic tar writer (PAX, zero timestamps, 0644/0755, uid/gid 0)
- [x] Implement small-file bundling (stable sort, pack into .tar.gz up to bundle_size_max)
- [x] Implement large-file layer creation (single-entry tar, compression by extension)
- [x] Implement Pack() orchestrator that ties classification + tar writing together
- [x] Implement annotation construction per spec §2.2
- [x] Write comprehensive tests
- [x] Run linter

## Data flow context

The output of this engine feeds into tarball.LayerFromFile() which creates lazy v1.Layer objects that stream from disk on demand. The tar files on disk ARE the layer blobs -- they go straight to the registry via HTTP, never through Docker. So the output contract is: tar files on disk + metadata. Nothing more.

Given a source directory, produce tar layers:
- Classify files by size against bundle_file_max (64MB default)
- Small files: stable-sort by path, pack into .tar.gz bundles (up to bundle_size_max)
- Large files: single-entry .tar per file, compression based on extension skip-set
- Deterministic tar properties: zero timestamps, uid/gid 0, 0644/0755 perms, PAX format
- Each source file assigned to exactly one layer (order-independence guarantee)

Output: list of tar files on disk with their computed digests, sizes, media types, and annotations (content type, file path for single-file layers, uncompressed size).

This is the core of the format spec. Everything downstream consumes these layers.

Reference: specs/weights.md §1, existing code in pkg/model/weight_builder.go (v0, single-file-per-weight)

## Summary of Changes

Implemented the v1 tar layer packing engine in `pkg/model/packer.go` with comprehensive tests in `pkg/model/packer_test.go`.

**Core API**: `Pack(ctx, sourceDir, opts) ([]LayerResult, error)` — walks a directory, classifies files by size threshold, and produces deterministic tar layers on disk.

**What it does**:
- Classifies files as small (< 64 MB) or large (>= 64 MB) per spec §1.2
- Small files: stable-sorted by path, packed into `.tar.gz` bundles up to 256 MB
- Large files: single-entry tar per file, compression decided by extension (incompressible set: .safetensors, .bin, .gguf, .onnx, .parquet, .pt, .pth)
- Deterministic tar headers: PAX format, Unix epoch timestamps, uid/gid 0, 0644/0755 perms
- Returns LayerResult with tar path, digest, size, media type, and OCI annotations per spec §2.2/§2.3
- Skips .cog/ state directory

**Constants added**: OCI media types (`MediaTypeOCILayerTar`, `MediaTypeOCILayerTarGzip`), v1 annotation keys (`AnnotationV1WeightContent`, `AnnotationV1WeightFile`, `AnnotationV1WeightSizeUncomp`), content type values (`ContentBundle`, `ContentFile`).

**22 tests** covering: empty dir, single/mixed files, nested dirs, bundle splitting, incompressible extensions, threshold boundaries, .cog/ exclusion, deterministic properties, digest reproducibility, context cancellation, and the full worked example from spec §4.

## Post-review fixes\n\n- [x] Fix temp file leak on error (remove temp files, not just close)\n- [x] Clean up already-written results when Pack fails partway through\n- [x] Fix misleading readTarGzEntries doc comment\n- [x] Add comment about single-file-exceeding-BundleSizeMax behavior\n- [x] Add comment about symlink skipping
