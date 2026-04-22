---
# cog-s5fy
title: weights.lock v2 format and source fingerprinting
status: todo
type: task
priority: critical
created_at: 2026-04-17T19:27:20Z
updated_at: 2026-04-22T01:57:34Z
parent: cog-66gt
blocked_by:
    - cog-2gv9
---

Redesign the lockfile from first principles and build the source-to-lockfile pipeline.

The lockfile is the center of gravity for managed weights. It is analogous to go.sum or pnpm-lock.yaml: the idempotent, repeatable result of scanning a source and pushing to the registry. Everything else is derived from it — OCI manifests, the runtime `/.cog/weights.json`, registry state validation, pull operations, build-time image assembly.

This bean redesigns the lockfile schema, introduces the `Source` interface for pluggable source types, and implements `file://` as the first (and currently only) source provider.

Reference: `specs/weights.md` for the OCI format spec. The lockfile is a Cog concern, not a spec concern — it carries everything needed to reproduce the spec-defined artifacts.

## Lockfile schema

```json
{
  "version": 1,
  "weights": [
    {
      "name": "z-image-turbo",
      "target": "/src/weights",
      "source": {
        "uri": "file://./weights",
        "fingerprint": "sha256:def456...",
        "include": [],
        "exclude": [],
        "importedAt": "2026-04-16T17:27:07Z"
      },
      "digest": "sha256:abc123...",
      "setDigest": "sha256:def456...",
      "size": 32600000000,
      "sizeCompressed": 32457803776,
      "files": [
        { "path": "config.json", "size": 1234, "digest": "sha256:f01...", "layer": "sha256:aaa..." },
        { "path": "text_encoder/model-00001.safetensors", "size": 3957900840, "digest": "sha256:f03...", "layer": "sha256:bbb..." }
      ],
      "layers": [
        { "digest": "sha256:aaa...", "mediaType": "application/vnd.oci.image.layer.v1.tar+gzip", "size": 15000000, "sizeUncompressed": 18500000 },
        { "digest": "sha256:bbb...", "mediaType": "application/vnd.oci.image.layer.v1.tar", "size": 3957900840, "sizeUncompressed": 3957900840 }
      ]
    }
  ]
}
```

### Design principles

- **Version** is an integer (`1`). Monotonic, no ambiguity (`"v1"` vs `"1.0"`).
- **No annotations anywhere.** Annotations are an OCI presentation detail. The lockfile stores the raw data; OCI annotations are derived at manifest-build time from the lockfile's typed fields.
- **File index at the entry level.** Every file with its path, size, content digest, and which layer contains it. Matches the config blob's files array (spec §2.3) — the lockfile is a superset of the config blob.
- **Layers carry only intrinsic properties**: digest, mediaType, compressed size, uncompressed size. No `content: "bundle"|"file"` — derivable from the file index (one file referencing the layer = single-file layer, many = bundle).
- **Provenance grouped in `source` block**: URI, fingerprint, include/exclude patterns, importedAt timestamp. The import is a function of (source at fingerprint, include/exclude). Recording all inputs makes the lockfile self-contained.
- **Both sizes on the root**: `size` = total uncompressed (sum of layer sizeUncompressed). `sizeCompressed` = total compressed (sum of layer size).
- **`digest`** is the OCI manifest digest (`<registry>/<repo>@sha256:<this>`). `setDigest` is the content-addressable file set identity (spec §2.4).
- **No top-level `created` timestamp.** Per-entry `importedAt` is sufficient.

### Serialization rules

- `files`: sorted lexicographically by `path`
- `layers`: sorted lexicographically by `digest`
- `include` / `exclude`: always serialized as `[]` when empty (never omitted)
- Regenerating the lockfile from the same source must produce byte-identical output
- The packer's layer emission order is not guaranteed stable (future concurrency). The serializer sorts to produce deterministic output independent of packer ordering

### Derivations from the lockfile

Everything downstream is a projection:

| Artifact | Derivation |
|----------|-----------|
| OCI manifest annotations | `name`, `target`, `setDigest` direct from entry fields |
| OCI config blob (spec §2.3) | `name`, `target`, `setDigest`, `files` |
| Layer descriptors in manifest | `digest`, `mediaType`, `size` per layer — no annotations (spec §2.5) |
| `/.cog/weights.json` (spec §3.3) | `[{ name, target, setDigest }]` per entry |
| OCI index descriptor annotations (spec §2.6) | `run.cog.weight.name`, `run.cog.weight.set-digest`, `run.cog.weight.size.uncompressed` from entry `name`, `setDigest`, `size` |

## Source interface

```go
// Source is the provider for a source scheme. Each source type (file://, hf://,
// s3://, etc.) implements this interface.
type Source interface {
    // Fetch materializes the source as a local directory ready for the packer.
    // For file:// this validates and returns the source path. For remote
    // sources (future), downloads to a temp directory.
    Fetch(ctx context.Context, uri string) (localDir string, err error)

    // Fingerprint returns the source's version identity. For file://, this is
    // sha256:<setDigest> computed over the file set. For remote sources,
    // a scheme-native identifier (commit:<sha>, etag:<value>, etc.).
    Fingerprint(ctx context.Context, uri string) (SourceFingerprint, error)
}
```

Source selection is a compile-time switch on URI scheme. Unknown schemes return a clear error. The interface establishes the contract that future source types (hf/s3/http in cog-9vfd) implement — no refactoring needed when the second source lands.

### SourceFingerprint

Scheme-prefixed string type: `sha256:<hex>`, `commit:<sha>`, `etag:<value>`, `md5:<hex>`, `timestamp:<rfc3339>`. The prefix identifies the algorithm/source type. Parsing helpers: `Scheme()`, `Value()`, `ParseSourceFingerprint()`.

### file:// implementation

URI forms: `file:///abs/path` (absolute), `file://./rel/path` (canonical relative), bare `./rel` or `/abs` (normalized to file:// at parse time).

- **Fetch**: parse URI, resolve relative paths against cog.yaml's directory, validate directory exists, return absolute path.
- **Fingerprint**: walk the directory, hash files, compute set digest, return `sha256:<setDigest>`. (For the import path, we skip the interface call and use the setDigest the packer already computed — acceptable temp hack since file:// fingerprint == content hash.)

URI normalization:
- Bare paths normalized to `file://` scheme
- Relative paths cleaned with `filepath.Clean`
- Canonical relative form: `file://./weights` (explicit `./`)
- The lockfile stores the normalized form, never the resolved absolute path (portable across machines)
- Resolution to an absolute on-disk path happens on demand in `Fetch`
- URI validation (path escape prevention, empty paths, platform-specific forms) is an implementation detail; the builder rejects malformed or unsafe URIs with clear errors

## Equality semantics

Split into two checks:

- **Content equality**: `digest`, `setDigest`, `size`, `sizeCompressed`, `files`, `layers`. If content is identical, no rewrite of content fields.
- **Source equality**: `source.uri`, `source.fingerprint`, `source.include`, `source.exclude`. Not `importedAt` — that is updated as a consequence when equality fails, not an input to the check.

Lockfile is rewritten if *either* check fails. This correctly handles: same source producing identical content (no rewrite), same source with new upstream fingerprint but identical filtered content (provenance updates), changed content (full rewrite).

## Builder changes

- `WeightBuilder.Build` uses `SourceFor(uri)` to pick the source, calls `source.Fetch()` (replaces the current direct `resolveSource()` call)
- After packing: assembles `WeightLockSource` with URI, fingerprint, include/exclude (empty for now — cog-6wm0 wires patterns), `importedAt = now`
- `NewWeightLockEntry` takes source metadata + packed files + layer results + computed digests
- Cache-hit path reads `files` directly from the lockfile entry instead of scanning tar archives

## Manifest builder changes

Layer descriptors carry no annotations per spec §2.5. Manifest-level annotations unchanged: `name`, `target`, `setDigest`.

Broader rewiring of manifest/config-blob/index generation to consume the lockfile exclusively is tracked in cog-861o.

## Dead code removal

- `listBundleFiles` in `weight_builder.go` (~35 lines) — file→layer mapping now in lockfile
- Bundle-scanning branch in `walkAndHashFiles` (~20 lines) — replaced by reading lockfile `files`
- `LayerResult.Annotations` — removed. Layers emit no annotations (spec §2.5), the lockfile does not persist them, the packer stops computing them. Annotation constants (`AnnotationV1WeightContent`, `AnnotationV1WeightFile`, `AnnotationV1WeightSizeUncomp`) deleted if no other consumer remains

## Tasks

- [ ] New types: `WeightsLock` (version as int), `WeightLockEntry`, `WeightLockSource`, `WeightLockFile`, `WeightLockLayer`
- [ ] `SourceFingerprint` type + parsing
- [ ] `Source` interface + `SourceFor` scheme switch + `FileSource` implementation
- [ ] `NewWeightLockEntry` redesigned to accept source metadata + file index + layers
- [ ] Lockfile serializer: sort files by path, layers by digest, stable output
- [ ] Split `lockEntriesEqual` into content + source equality
- [ ] Wire `Source.Fetch` into `WeightBuilder.Build`; compute and record fingerprint + source block
- [ ] Simplify cache-hit path: read `files` from lockfile, drop `listBundleFiles` + bundle tar scanning
- [ ] Update manifest builder: layer descriptors emit no annotations (spec §2.5); manifest annotations unchanged
- [ ] Update all call sites: `weights_inspect.go`, test helpers, `tools/weights-gen`
- [ ] Tests: type round-trips, fingerprint parsing, URI normalization, FileSource, builder with new lockfile, serializer ordering, cache-hit idempotency
- [ ] `mise run fmt:fix` → `mise run lint` → `mise run test:go` green

## Out of scope

- `/.cog/weights.json` generation at build time → cog-1pm2
- `cog weights check` command → cog-wej9
- Include/exclude pattern application at import time → cog-6wm0 (this bean records patterns in the lockfile; 6wm0 implements filtering)
- hf:// / s3:// / http:// Source implementations → cog-9vfd (implements the same `Source` interface established here)
