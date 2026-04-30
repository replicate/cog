# Managed Weights: OCI Format and Runtime State Specification

- Version: 1.0-draft
- Status: Draft

## Overview

A managed weight is a named set of files (model weights, configs, tokenizers, etc.) stored as an OCI artifact in a container registry. Each weight maps to a target directory in the running container and is delivered independently from the model image.

This spec covers three things: how weight data is packed into OCI layers, how those layers are described in an OCI manifest, and how weight readiness is communicated between a provider and a consumer.

The spec is driven by the needs of [Cog](https://github.com/replicate/cog) but is a general-purpose format for storing and delivering AI model weights via OCI registries.

## 1. Layer Format

All layers are tar archives. Tar provides file metadata (path, size, permissions) at negligible overhead (512 bytes per entry) and supports streaming extraction without buffering.

**Layers are immutable.** Once a layer is produced and pushed, its content never changes -- the digest is its identity. A weight set is a fixed collection of immutable layers. If all layers are present, the complete file set is present. This property is fundamental to the caching and delivery model: layers can be cached indefinitely, shared across weights and models, and assembled into weight sets without re-verification.

### 1.1 Layer independence (order-invariant extraction)

**Layers MUST be extractable in any order and produce identical results.** Unlike Docker image layers which use overlay semantics (later layers shadow earlier ones), weight layers are independent units. Each layer contains a disjoint set of files. No file path appears in more than one layer within a manifest.

Specifically:

- No file path appears in more than one layer (disjoint file sets).
- Layers MUST NOT contain overlay/union filesystem artifacts: no whiteout files (`.wh.*`), no opaque whiteout markers (`.wh..wh..opq`), no delete markers of any kind.
- Extracting all layers to the same target directory in any order MUST produce a byte-identical result.

This constraint exists because weight layers are large (multi-GB) and must be downloaded and extracted in parallel without coordination or sequencing. Requiring ordered extraction would force either buffering of out-of-order layers or serialized download, both unacceptable at the scale of model weights.

The packing algorithm (§1.2) enforces this: each source file is assigned to exactly one layer. The manifest records the full set of layers; consumers extract all layers to the same target directory in whatever order they arrive.

### 1.2 Packing strategy

Producers assign each source file to exactly one layer, maintaining the disjoint file set invariant (§1.1). Two categories of layers exist:

- **Bundle layers** contain multiple small files packed into a single tar archive. Files within a bundle MUST be stable-sorted by relative path so that identical source files produce byte-identical tar archives (and therefore identical layer digests) across reimports.
- **Standalone layers** contain a single file as a single-entry tar.

Whether to bundle or not, whether to compress or not, and all other packing parameters are producer implementation choices. Consumers MUST process each layer according to its media type (§2.1) regardless of the producer's choices. For example, a producer might bundle all files under 64 MB into compressed tar layers (up to 256 MB each) and give every file at or above 64 MB its own uncompressed layer. A different producer could skip bundling entirely and emit one layer per file.

As a general principle, producers SHOULD compress bundle layers (dominated by compressible text formats like JSON and YAML) and SHOULD NOT compress standalone layers (often high-entropy binary data where compression yields negligible savings at substantial CPU cost). If a producer does compress large standalone layers, it SHOULD first probe the content to verify compression yields meaningful savings -- many weight formats are high-entropy and compress poorly. It SHOULD also use a format that supports parallel decompression (e.g., seekable zstd) to avoid serializing extraction of multi-GB layers.

### 1.3 Allowed content

Layers MUST contain only regular files and directories. The following are not permitted:

- **Symlinks** (symbolic and hard links) -- introduce ambiguity (relative vs absolute targets, dangling references, circular chains) and path traversal risk during extraction. Source directories containing symlinks MUST be resolved to regular files before import.
- **Device nodes, FIFOs, sockets** -- not meaningful for weight data.
- **Whiteout files** (`.wh.*`, `.wh..wh..opq`) -- overlay filesystem artifacts that imply ordered layer semantics, which this format forbids (§1.1).
- **Extended attributes, ACLs, security labels** -- platform-specific metadata that breaks deterministic packing.

Producers MUST reject (not silently skip) excluded content with a descriptive error.

Producers MUST also reject source directories containing a `.cog/` (or equivalent state directory, see §3) directory, which is reserved for the runtime state protocol.

### 1.4 Tar properties (deterministic packing)

All tar archives MUST be produced with these properties to ensure byte-identical digests across re-imports from the same source:

- Format: PAX (for paths exceeding 100 characters)
- `mtime`, `atime`, `ctime`: 0 (Unix epoch)
- UID/GID: 0/0
- Permissions: 0644 (files), 0755 (directories)
- No extended attributes, no system-specific metadata
- Paths relative to the weight's target directory (no leading `/` or `./`)
- Paths are case-sensitive with no case folding.
- Paths MUST be valid UTF-8.
- Path components MUST NOT contain: NUL (`\0`), forward slash (`/` -- used only as the path separator), backslash (`\`), or control characters (bytes `0x01`-`0x1F` and `0x7F`).

### 1.5 No file splitting

Each file is packed into exactly one layer, whole. Files are never split across multiple layers. This keeps the format simple -- no reassembly metadata, no ordering dependencies between layers, and each extracted file is immediately usable.

This works because training frameworks already shard large models into multiple files (e.g., 64x 9.8 GB safetensors for kimi-k2.5). The sharding provides natural parallelism at the layer level. If a use case arises where individual files are too large for practical single-layer transport, file splitting would require a reassembly protocol and is deferred to a future spec version.

## 2. OCI Manifest

Each named weight is an OCI manifest with `artifactType` identifying it as a cog weight artifact.

### 2.1 Media types

| Media type                                    | Usage                         |
| --------------------------------------------- | ----------------------------- |
| `application/vnd.cog.weight.v1`               | Manifest `artifactType` field |
| `application/vnd.cog.weight.config.v1+json`   | Config blob media type        |
| `application/vnd.oci.image.layer.v1.tar`      | Uncompressed tar layer        |
| `application/vnd.oci.image.layer.v1.tar+gzip` | Gzip-compressed tar layer     |
| `application/vnd.oci.image.layer.v1.tar+zstd` | Zstd-compressed tar layer     |

The layer media types (`tar`, `tar+gzip`, `tar+zstd`) are standard OCI types defined in the [OCI image spec](https://github.com/opencontainers/image-spec/blob/main/layer.md), reused here for ecosystem compatibility. Consumers MUST accept all three. Producers choose which to use for each layer (§1.2); the media type communicates that choice. Because the manifest uses standard OCI media types throughout, existing tools (crane, skopeo, containerd, `docker pull`) work with weight artifacts without modification.

The `artifactType` distinguishes weight manifests from runnable image manifests. The config blob carries a file-level index for the weight (§2.3).

### 2.2 Manifest structure

```json
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.manifest.v1+json",
  "artifactType": "application/vnd.cog.weight.v1",
  "config": {
    "mediaType": "application/vnd.cog.weight.config.v1+json",
    "digest": "sha256:config123...",
    "size": 512
  },
  "layers": [
    {
      "mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
      "digest": "sha256:aaa...",
      "size": 15000000
    },
    {
      "mediaType": "application/vnd.oci.image.layer.v1.tar",
      "digest": "sha256:bbb...",
      "size": 3957900840
    }
  ],
  "annotations": {
    "run.cog.weight.name": "z-image-turbo",
    "run.cog.weight.target": "/src/weights",
    "run.cog.weight.set-digest": "sha256:def456..."
  }
}
```

The manifest contains no timestamps, source URIs, or producer version metadata. This makes the manifest a pure function of the weight content (files), the packing strategy (layers), and the cog.yaml config (name, target). Identical inputs always produce an identical manifest digest, and the registry handles dedup at the storage level (§2.7).

### 2.3 Config blob (file index)

The config blob is a JSON document with media type `application/vnd.cog.weight.config.v1+json`. It describes the weight artifact and provides a file-level index: every file, which layer it belongs to, its size, and its content digest.

```json
{
  "name": "z-image-turbo",
  "target": "/src/weights",
  "setDigest": "sha256:def456...",
  "files": [
    {
      "path": "config.json",
      "layer": "sha256:aaa...",
      "size": 1234,
      "digest": "sha256:f01..."
    },
    {
      "path": "tokenizer.json",
      "layer": "sha256:aaa...",
      "size": 5678,
      "digest": "sha256:f02..."
    },
    {
      "path": "text_encoder/model-00001-of-00003.safetensors",
      "layer": "sha256:bbb...",
      "size": 3957900840,
      "digest": "sha256:f03..."
    }
  ]
}
```

**Top-level fields:**

| Field       | Type   | Description                                                                                   |
| ----------- | ------ | --------------------------------------------------------------------------------------------- |
| `name`      | string | Weight name (e.g., `z-image-turbo`). Same as the manifest annotation.                         |
| `target`    | string | Absolute mount path in the container (e.g., `/src/weights`). Same as the manifest annotation. |
| `setDigest` | string | Weight set digest (§2.4). Same as the manifest annotation.                                    |

**File entry fields:**

| Field    | Type    | Description                                                                    |
| -------- | ------- | ------------------------------------------------------------------------------ |
| `path`   | string  | File path relative to the weight target directory. Same as the tar entry path. |
| `layer`  | string  | Digest of the layer containing this file.                                      |
| `size`   | integer | File size in bytes (uncompressed).                                             |
| `digest` | string  | SHA-256 content digest of the individual file.                                 |

The `files` array MUST be sorted by `path` lexicographically. This ensures the config blob is deterministic for a given packing: the same source files packed with the same parameters always produce an identical config blob. Note that the config blob may differ across packing changes (different `layer` values), but the weight set digest (§2.4) remains stable because it is computed from file content only.

The config blob provides a complete file-level index of the weight. Infra uses it to assemble the final weight directory from extracted layers without walking the filesystem -- for each file, it knows exactly which layer to source from. The per-file `digest` additionally enables infra to identify identical files across different layers or weights. The `name`, `target`, and `setDigest` fields duplicate the manifest annotations so the config blob is self-describing -- a consumer with only the config blob has enough context to understand what it is and where it goes.

### 2.4 Weight set digest

The **weight set digest** is the content identity of a weight's file set, independent of how those files are packed into layers, and independent of manifest metadata (annotations, timestamps, producer version). It is the canonical content-addressable identifier for a set of weight files: two weight manifests with identical weight set digests produce byte-identical extracted results.

Producers MUST compute the weight set digest as:

```
sha256(join(sort(entries), "\n"))
```

Where each entry is `<hex-sha256>  <path>` (hex-encoded SHA-256 hash of the file content, two spaces, file path) from the config blob's `files` array, sorted lexicographically by `path`. The result is encoded as a standard OCI digest string (e.g., `sha256:def456...`).

This entry format matches the output of `sha256sum`, so producers and operators can verify a weight set digest from a shell:

```bash
sha256sum $(find <target> -type f | sort) | sha256sum
```

Because the weight set digest is computed from file content (not layer structure), it is **packing-independent**: changing bundle thresholds, compression settings, or any other packing parameter does not change the weight set digest as long as the source files are identical. Different producer versions producing different layer layouts from the same source files will produce the same weight set digest.

Producers MUST include this value as the `run.cog.weight.set-digest` manifest annotation. The computation is specified so that any party (producers, infra, operators) can independently verify or recompute it.

The weight set digest enables several behaviors:

- **Caching**: Infra uses it as the key for assembled weights. If the assembled result already exists for this digest, skip extraction entirely.
- **Cross-model reuse**: Two models using identical weight files produce identical file digests and therefore identical weight set digests, enabling shared caching even when the models have separate weight repositories and different layer layouts.

### 2.5 Annotations

Annotations use the `run.cog.*` namespace (reverse-domain of cog.run).

**Manifest-level annotations:**

| Key                         | Value         | Description                                                            |
| --------------------------- | ------------- | ---------------------------------------------------------------------- |
| `run.cog.weight.name`       | string        | Weight name (e.g., `z-image-turbo`). REQUIRED.                         |
| `run.cog.weight.target`     | string        | Absolute mount path in the container (e.g., `/src/weights`). REQUIRED. |
| `run.cog.weight.set-digest` | digest string | Weight set digest (§2.4). REQUIRED.                                    |

All manifest-level annotations are deterministic from the weight content and cog.yaml config. No timestamps, source URIs, or producer metadata are included -- identical inputs always produce an identical manifest digest.

**Layer descriptor annotations:**

| Key                                | Value          | Description                                                                                                      |
| ---------------------------------- | -------------- | ---------------------------------------------------------------------------------------------------------------- |
| `run.cog.weight.size.uncompressed` | integer string | Uncompressed size of the layer's contents in bytes (sum of regular-file bytes, excluding tar headers). REQUIRED. |

This is the only annotation layer descriptors carry. All file-level metadata (paths, per-file sizes, layer mappings) lives in the config blob (§2.3); consumers that need it MUST read the config blob.

The uncompressed size is present at the descriptor level so consumers can make per-layer decisions (disk allocation, parallel extraction progress, partial pulls) without fetching the config blob. For compressed layers (`tar+gzip`) the descriptor's `size` is the compressed byte count; this annotation carries the uncompressed count. For uncompressed layers (`tar`) the two are approximately equal (modulo tar headers).

### 2.6 OCI index (bundle)

When a model uses managed weights, the push operation produces an OCI image index containing the model image manifest and all weight manifests:

```json
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.index.v1+json",
  "manifests": [
    {
      "mediaType": "application/vnd.oci.image.manifest.v1+json",
      "digest": "sha256:image...",
      "size": 1234,
      "platform": { "os": "linux", "architecture": "amd64" }
    },
    {
      "mediaType": "application/vnd.oci.image.manifest.v1+json",
      "digest": "sha256:weight...",
      "size": 5678,
      "artifactType": "application/vnd.cog.weight.v1",
      "platform": { "os": "unknown", "architecture": "unknown" },
      "annotations": {
        "run.cog.weight.name": "z-image-turbo",
        "run.cog.weight.set-digest": "sha256:def456...",
        "run.cog.weight.size.uncompressed": "32457803776"
      }
    }
  ]
}
```

The model image gets a real platform descriptor. Weight descriptors carry both `artifactType` and `platform`:

- **`artifactType`**: Set to `application/vnd.cog.weight.v1`. This is the OCI-standard mechanism ([image-spec descriptor](https://github.com/opencontainers/image-spec/blob/main/descriptor.md)) for identifying non-image content in an index. It enables tooling to distinguish weight manifests from runnable images without inspecting annotations.
- **`platform`**: Set to `{"os": "unknown", "architecture": "unknown"}`. Weight data is not platform-specific, but the field is included for compatibility. This follows the precedent set by [Docker BuildKit attestations](https://docs.docker.com/build/metadata/attestations/attestation-storage/), which use the same convention to prevent container runtimes from accidentally pulling non-image entries. Omitting `platform` entirely is spec-valid (the field is OPTIONAL per the OCI image-spec) but risks being filtered out by containerd's platform matcher and other tools that assume its presence.

**Index descriptor annotations:**

| Key                                | Value          | Description                                               |
| ---------------------------------- | -------------- | --------------------------------------------------------- |
| `run.cog.weight.name`              | string         | Weight name. REQUIRED.                                    |
| `run.cog.weight.set-digest`        | digest string  | Weight set digest (§2.4). REQUIRED.                       |
| `run.cog.weight.size.uncompressed` | integer string | Total uncompressed size of all layers in bytes. REQUIRED. |

These annotations exist so the index is scannable without fetching child manifests. `name` and `set-digest` identify what the weight is and enable cache lookups. `size.uncompressed` enables scheduling decisions (e.g., whether a node has enough disk space) without downloading any weight data.

Note that `run.cog.weight.target` is intentionally omitted from the index descriptor. The target path is operational detail available in the weight manifest annotations and config blob (§2.3); it is not needed for scanning or scheduling at the index level.

The `size` field on the descriptor itself is the size of the weight manifest JSON (per the OCI spec, descriptor `size` is always the byte count of the referenced blob). It is NOT the size of the weight data. Use `run.cog.weight.size.uncompressed` for the actual weight size.

The binding between a model image and its weights is structural: both appear as siblings in the same OCI index. No back-reference annotation from weight to model is needed.

### 2.7 Import behavior

`cog weights import` hashes source files, packs layers, builds the config blob, and pushes the manifest. The lockfile is updated when the weight content changes; if nothing changed, the lockfile is unchanged.

Because the manifest contains no volatile metadata (§2.2), identical inputs always produce an identical manifest digest. The registry handles blob and manifest dedup at the storage level -- pushing an already-existing blob or manifest is a no-op. No special client-side dedup logic is required.

Note that the manifest digest and weight set digest (§2.4) operate at different levels. The manifest digest changes when packing changes (different layers, config, and annotations); the weight set digest does not (same files). Infra uses the weight set digest to reuse assembled weights even when the manifest differs.

### 2.8 Registry namespace and tagging (Cog convention)

This section describes how Cog organizes weight artifacts in a registry. The namespace layout and tagging scheme are Cog conventions, not normative requirements of the format. Other producers may organize weight artifacts differently.

```
<model>                        # OCI index (bundle)
<model>/weights/<name>         # Named weight repository
<model>/weights/<name>:<ts>    # Timestamp tag (e.g., 20260416T172707Z)
<model>/weights/<name>@sha256: # Immutable digest reference
```

**Tagging scheme:** Weight imports are tagged with the import timestamp in ISO 8601 compact format (`YYYYMMDDTHHMMSSZ`). This applies uniformly regardless of source type (HuggingFace, filesystem, HTTP, registry). The timestamp answers the question "when was this version imported?" Source-specific identifiers (HF commit SHA, S3 path, etc.) are dev-time concerns tracked in `cog.yaml` and the lockfile, not in the weight artifact.

Cog does not automatically create `:latest` tags. The lockfile records the manifest digest for reproducibility; timestamp tags exist for human-readable listing via `cog weights list`.

## 3. Runtime State Protocol

> **Status: Work in progress.** The design direction is settled (filesystem markers, provider writes, consumer reads). The exact file layout and semantics are being refined as the implementation evolves. The state directory name (`.cog/` vs `.weight/` or similar) is TBD.

### 3.1 Design

The **provider** (platform infra in prod, `cog weights pull` + local orchestration in dev) assembles a weight directory and communicates readiness via marker files. The **consumer** (coglet) reads these markers to gate `setup()` -- blocking until all weights are ready without requiring any user code to handle the wait. In the future, per-weight markers could enable an async API where `setup()` begins processing weights (such as loading to the GPU) as they become available while others are still downloading. The consumer never writes state -- it is a pure observer.

Filesystem markers are used instead of an HTTP API because they decouple provider and consumer in time and failure domains. The provider can write state before the container boots. The consumer can read state without the provider being alive. Either side can crash and restart independently. No orchestration, no lifecycle coupling, no retry logic. Multiple containers can share the same weight directory without complexity scaling proportionally -- they all observe the same markers. And the consumer interface is identical regardless of how the weight directory was assembled: attaching a ready-to-run cached volume, downloading all layers from scratch, fetching a diff of changed layers, or rebuilding from new layers all look the same to coglet.

### 3.2 State markers

The provider writes state into a `.cog/` subtree within each weight's target directory:

```
<target>/.cog/ready        # weight is usable
<target>/.cog/failed       # delivery failed (contents: error message)
<target>/.cog/downloading  # delivery in progress
```

Coglet checks these with a single `stat()` call:

```
1. .cog/ready exists       → weight usable, proceed
2. .cog/failed exists      → read error, surface it, fail
3. .cog/downloading exists → in progress, poll
4. .cog/ missing           → provider hasn't started, wait with timeout
```

Markers are created atomically (write-to-temp + rename). A `ready` marker MUST NOT appear until all weight data is fully written and flushed to disk.

If the weight directory is already fully assembled when mounted (e.g., reused from cache), the provider writes `ready` immediately.

**Correctness is the provider's responsibility.** When `ready` is set, the weight directory MUST contain the exact files matching the configured weight set digest. Serving stale or mismatched weights is a catastrophic infra failure. Consumers MUST NOT verify weight content -- no checksumming, no manifest cross-checking, no redundant validation. The `ready` marker is the contract.

### 3.3 Model image metadata

`cog build` writes `/.cog/weights.json` into the model image. This file:

- Signals to coglet that managed weights are active (presence = managed weights, absence = no managed weights).
- Tells coglet what weights the model expects before calling `setup()`.

```json
{
  "weights": [
    {
      "name": "z-image-turbo",
      "target": "/src/weights",
      "setDigest": "sha256:def456..."
    }
  ]
}
```

The `setDigest` is the weight set digest (§2.4). Coglet reads this file to know which weights to expect and where, then waits for each weight's state markers (§3.2) to report ready before invoking `setup()`. If the weight directory reports a different set digest than expected, coglet will refuse to start.

### 3.4 Target directory constraints

- Each weight's `target` must be unique within a model.
- Weight targets must be disjoint subtrees (no nesting).
- Both rules enforced at config validation time.
- Model code should ignore `.cog/` subdirectories in weight targets.

## 4. Real Example: z-image-turbo (~32 GB)

Source: [HuggingFace repo](https://huggingface.co/Tongyi-MAI/Z-Image-Turbo) with 19 files (configs, tokenizers, safetensors shards).

**v0:** 19 weight entries in cog.yaml, 19 separate manifests, 19 blobs in the OCI index.

**v1:** 1 weight entry, 1 manifest, 8 layers. Using a 64 MB bundle threshold: the 12 small files (configs, JSONs, tokenizer, index files -- all under 64 MB, ~16 MB total) are bundled into a single compressed layer. The 7 large files (all safetensors shards, each above 64 MB) each get their own uncompressed standalone layer:

| Layer | Contents                                                        | Size    | Format       |
| ----- | --------------------------------------------------------------- | ------- | ------------ |
| 1     | Bundle: 12 small files (configs, JSONs, tokenizer, index files) | ~16 MB  | compressed   |
| 2     | text_encoder/model-00001-of-00003.safetensors                   | ~3.9 GB | uncompressed |
| 3     | text_encoder/model-00002-of-00003.safetensors                   | ~3.9 GB | uncompressed |
| 4     | text_encoder/model-00003-of-00003.safetensors                   | ~99 MB  | uncompressed |
| 5     | vae/diffusion_pytorch_model.safetensors                         | ~167 MB | uncompressed |
| 6     | transformer/diffusion_pytorch_model-00001-of-00003.safetensors  | ~9.9 GB | uncompressed |
| 7     | transformer/diffusion_pytorch_model-00002-of-00003.safetensors  | ~9.9 GB | uncompressed |
| 8     | transformer/diffusion_pytorch_model-00003-of-00003.safetensors  | ~4.6 GB | uncompressed |

Layer 1 is `tar+gzip` (small compressible text files). Layers 2-8 are `tar` (large binary safetensors where compression yields negligible savings). Consumers process each layer according to its media type regardless of the producer's threshold or compression choices.

All 8 layers are independent. An extractor can download and unpack them in any order. Layer 1 writes to paths like `config.json`, `tokenizer.json`. Layers 2-8 each write to a single path like `text_encoder/model-00001-of-00003.safetensors`. No path conflicts.
