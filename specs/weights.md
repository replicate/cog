# Managed Weights: OCI Format Specification

Version: 1.0-draft
Status: Draft

## Overview

A managed weight is a named set of files (model weights, configs, tokenizers, etc.) stored as an OCI artifact in a container registry. Each weight maps to a target directory in the running container and is delivered independently from the model image.

This spec covers three things: how weight data is packed into OCI layers, how those layers are described in an OCI manifest, and how weight readiness is communicated between a provider and a consumer.

The spec is driven by the needs of [Cog](https://github.com/replicate/cog) but is intended to be general-purpose. Any system that needs to store, transfer, and deliver large file sets via OCI registries can implement it.

## 1. Layer Format

All layers are tar archives. Tar provides file metadata (path, size, permissions) at negligible overhead (512 bytes per entry) and supports streaming extraction without buffering.

### 1.1 Layer independence (order-invariant extraction)

**Layers MUST be extractable in any order and produce identical results regardless of extraction order.** Unlike Docker image layers which use overlay semantics (later layers shadow earlier ones), weight layers are independent units. Each layer contains a disjoint set of files. No file path appears in more than one layer within a manifest.

Specifically:
- No file path appears in more than one layer (disjoint file sets).
- Layers MUST NOT contain overlay/union filesystem artifacts: no whiteout files (`.wh.*`), no opaque whiteout markers (`.wh..wh..opq`), no delete markers of any kind.
- Extracting all layers to the same target directory in any order MUST produce a byte-identical result.

This constraint exists because weight layers are large (multi-GB) and must be downloaded and extracted in parallel without coordination or sequencing. Requiring ordered extraction would force either buffering of out-of-order layers or serialized download, both unacceptable at the scale of model weights.

The packing algorithm (§1.2) enforces this: each source file is assigned to exactly one layer. The manifest records the full set of layers; consumers extract all layers to the same target directory in whatever order they arrive.

### 1.2 Packing strategy

Two thresholds control layer construction:

| Threshold | Default | Purpose |
|-----------|---------|---------|
| `bundle_file_max` | 64 MB | Files below this are eligible for bundling. Files at or above this get their own layer. |
| `bundle_size_max` | 256 MB | Max cumulative size of a single bundle tar. |

These are internal implementation parameters, not user-facing configuration.

**Small files** (< `bundle_file_max`): Collected, stable-sorted by relative path, and packed sequentially into compressed tar layers (each up to `bundle_size_max`). Compression is always applied since small files are dominated by compressible text formats (JSON, YAML, tokenizer files). Stable sorting ensures deterministic layer assignment across re-imports.

**Large files** (>= `bundle_file_max`): Each file becomes its own layer as a single-entry tar. Whether a large file layer is compressed depends on its content:

- **Uncompressed**: Dense binary formats that don't benefit from compression: `.safetensors`, `.bin`, `.gguf`, `.onnx`, `.parquet`, `.pt`, `.pth`. These are high-entropy data (packed floating point arrays) where compression yields negligible savings (typically 2-5%) at substantial CPU cost on both import and extraction.
- **Compressed**: All other large files.

The specific compression algorithm and level are implementation choices, not part of this spec. The layer's media type (§2.1) tells consumers how to decompress; consumers MUST support all media types listed there. Parallelism comes from downloading and extracting multiple independent layers concurrently, not from intra-layer parallel decompression.

**Rationale and future directions:** The extension-based skip list is deliberately conservative -- it's cheaper to leave a compressible file uncompressed than to pay decompression cost on every pull for negligible savings. Future producer versions may introduce content-aware compression, where the producer samples file content to decide whether compression is worthwhile for files not on the skip list. This would not change the consumer contract: consumers already handle all defined media types, and the compression decision is purely producer-side policy. If adopted, seekable zstd (independently-decompressible frames with a seek table) could enable consumers to parallelize download and decompression within a single compressed layer -- a seekable zstd stream is a valid zstd stream, so no new media types would be required.

### 1.3 Excluded content

Layers MUST contain only regular files and directories. The following are explicitly excluded:

- **Symlinks** (symbolic and hard links) -- introduce ambiguity (relative vs absolute targets, dangling references, circular chains) and path traversal risk during extraction. Source directories containing symlinks MUST be resolved to regular files before import.
- **Device nodes, FIFOs, sockets** -- not meaningful for weight data.
- **Whiteout files** (`.wh.*`, `.wh..wh..opq`) -- overlay filesystem artifacts that imply ordered layer semantics, which this format forbids (§1.1).
- **Extended attributes, ACLs, security labels** -- platform-specific metadata that breaks deterministic packing.

Producers MUST reject (not silently skip) excluded content with a descriptive error.

### 1.4 Tar properties (deterministic packing)

All tar archives MUST be produced with these properties to ensure byte-identical digests across re-imports from the same source:

- Format: PAX (for paths exceeding 100 characters)
- `mtime`, `atime`, `ctime`: 0 (Unix epoch)
- UID/GID: 0/0
- Permissions: 0644 (files), 0755 (directories)
- No extended attributes, no system-specific metadata
- Paths relative to the weight's target directory (no leading `/` or `./`)

### 1.5 Note on chunking

Training frameworks already shard weight files into manageable sizes (e.g., 64x 9.8 GB safetensors for kimi-k2.5). We do not split individual files across layers. If this becomes necessary, it would require reassembly metadata and is deferred.

## 2. OCI Manifest

Each named weight is an OCI manifest with `artifactType` identifying it as a cog weight artifact.

### 2.1 Media types

| Media type | Usage |
|-----------|-------|
| `application/vnd.cog.weight.v1` | Manifest `artifactType` field |
| `application/vnd.oci.image.layer.v1.tar` | Uncompressed tar layer |
| `application/vnd.oci.image.layer.v1.tar+gzip` | Gzip-compressed tar layer |
| `application/vnd.oci.image.layer.v1.tar+zstd` | Zstd-compressed tar layer |

Layers use standard OCI layer media types for ecosystem compatibility (crane, skopeo, containerd). All three layer types are defined in the OCI image spec. Consumers MUST accept all three. The current packing strategy (§1.2) produces `tar+gzip` for compressed layers and `tar` for uncompressed layers; `tar+zstd` is reserved for future use by producers that adopt zstd compression. The manifest's `artifactType` distinguishes weight manifests from runnable image manifests.

### 2.2 Manifest structure

```json
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.manifest.v1+json",
  "artifactType": "application/vnd.cog.weight.v1",
  "config": {
    "mediaType": "application/vnd.oci.empty.v1+json",
    "digest": "sha256:44136fa355b3...",
    "size": 2
  },
  "layers": [
    {
      "mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
      "digest": "sha256:aaa...",
      "size": 15000000,
      "annotations": {
        "run.cog.weight.content": "bundle",
        "run.cog.weight.size.uncompressed": "18500000"
      }
    },
    {
      "mediaType": "application/vnd.oci.image.layer.v1.tar",
      "digest": "sha256:bbb...",
      "size": 3957900840,
      "annotations": {
        "run.cog.weight.content": "file",
        "run.cog.weight.file": "text_encoder/model-00001-of-00003.safetensors",
        "run.cog.weight.size.uncompressed": "3957900840"
      }
    }
  ],
  "annotations": {
    "run.cog.weight.name": "z-image-turbo",
    "run.cog.weight.target": "/src/weights",
    "run.cog.reference.type": "weights",
    "run.cog.reference.digest": "sha256:model123...",
    "org.opencontainers.image.created": "2026-04-16T17:27:07Z"
  }
}
```

Config is the OCI empty descriptor (`{}`, sha256 of `{}`). No typed config blob -- all metadata lives in annotations.

### 2.3 Annotations

Annotations use the `run.cog.*` namespace (reverse-domain of cog.run).

**Manifest-level annotations:**

| Key | Value | Description |
|-----|-------|-------------|
| `run.cog.weight.name` | string | Weight name (e.g., `z-image-turbo`) |
| `run.cog.weight.target` | string | Absolute mount path in the container (e.g., `/src/weights`) |
| `run.cog.reference.type` | `"weights"` | Artifact type discriminator |
| `run.cog.reference.digest` | digest string | Digest of the model image this weight belongs to |
| `org.opencontainers.image.created` | RFC 3339 timestamp | When the weight was imported |

Manifest-level annotations are duplicated on the corresponding descriptor in the OCI index, so the index is inspectable without fetching child manifests (enables `cog weights list --remote`, platform placement decisions).

**Layer-level annotations:**

| Key | Value | Description |
|-----|-------|-------------|
| `run.cog.weight.content` | `"bundle"` or `"file"` | Whether the layer is a binpacked bundle of small files or a single large file |
| `run.cog.weight.file` | relative path | For `file` layers only: path within the weight directory |
| `run.cog.weight.size.uncompressed` | numeric string | Uncompressed size in bytes |

### 2.4 OCI index (bundle)

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
        "run.cog.weight.target": "/src/weights",
        "run.cog.reference.type": "weights",
        "run.cog.reference.digest": "sha256:image..."
      }
    }
  ]
}
```

The model image gets a real platform descriptor. Weight descriptors carry both `artifactType` and `platform`:

- **`artifactType`**: Set to `application/vnd.cog.weight.v1`. This is the OCI-standard mechanism ([image-spec descriptor](https://github.com/opencontainers/image-spec/blob/main/descriptor.md)) for identifying non-image content in an index. It enables tooling to distinguish weight manifests from runnable images without inspecting annotations.
- **`platform`**: Set to `{"os": "unknown", "architecture": "unknown"}`. Weight data is not platform-specific, but the field is included for compatibility. This follows the precedent set by [Docker BuildKit attestations](https://docs.docker.com/build/metadata/attestations/attestation-storage/), which use the same convention to prevent container runtimes from accidentally pulling non-image entries. Omitting `platform` entirely is spec-valid (the field is OPTIONAL per the OCI image-spec) but risks being filtered out by containerd's platform matcher and other tools that assume its presence.

Weight annotations are duplicated from the manifest onto the index descriptor so the index is inspectable without fetching child manifests (enables remote listing and platform placement decisions).

### 2.5 Registry namespace

```
<model>                              # OCI index (bundle)
<model>:v1                           # Tagged bundle version
<model>/weights/<name>               # Named weight repository
<model>/weights/<name>:latest        # Latest weight version
<model>/weights/<name>@sha256:...    # Specific weight version
```

## 3. Runtime State Protocol

The contract between the weight **provider** (platform infra in prod, `cog weights pull` + local orchestration in dev) and the weight **consumer** (coglet). Communicated entirely through files in the mounted weight directory. No out-of-band signaling.

### 3.0 Why filesystem markers, not an HTTP API

The alternative is an API on the consumer (e.g., `POST /weights/deliver`) that the provider calls to report progress. This pushes substantial complexity to both sides:

- **Lifecycle coupling**: The provider must wait for the consumer to be up and accepting requests before starting delivery. With files, the provider writes immediately and the consumer reads when ready.
- **Crash recovery**: If the provider crashes mid-delivery, the consumer has no way to detect this without polling back. With files, the absence of a terminal marker (`.ready` or `.failed`) after `manifest.json` exists is the crash signal.
- **Retry/backoff/idempotence**: An HTTP API requires the caller to handle transient failures, duplicate delivery attempts, and connection management. With files, atomic rename makes writes idempotent and there is no connection to manage.
- **Two sources of truth**: An API means in-memory state on the consumer and files on disk, which can diverge. With files, the filesystem IS the state.
- **Observability**: `ls .cog/layers/` from a shell is a complete status check. No client, no auth, no formatting.

The file-based protocol decouples provider and consumer in time and in failure domains. Either side can crash and restart independently. State survives restarts because it lives on the filesystem, not in process memory.

### 3.1 State directory

The provider writes state into a `.cog/` subtree at the root of each weight's target directory:

```
<target>/.cog/
├── manifest.json              # written once at start, identifies the delivery
├── ready                      # aggregate: all layers complete
├── failed                     # aggregate: any layer failed (payload: summary)
├── downloading                # aggregate: work in progress
└── layers/
    ├── sha256-aaa.ready
    ├── sha256-bbb.downloading
    └── sha256-ccc.failed      # payload: error detail
```

Digest characters `:` are replaced with `-` in filenames for filesystem portability.

### 3.2 Manifest file

Written atomically at the start of delivery. Never mutated. Lets consumers detect version skew and fail fast.

```json
{
  "version": "1",
  "name": "z-image-turbo",
  "digest": "sha256:abc123...",
  "layers": [
    "sha256:aaa...",
    "sha256:bbb...",
    "sha256:ccc..."
  ]
}
```

### 3.3 State markers

**Per-layer markers** (in `layers/`):

| Filename | Meaning | Payload |
|----------|---------|---------|
| `<digest>.downloading` | Layer extraction in progress | empty |
| `<digest>.ready` | Layer fully extracted and fsync'd | empty |
| `<digest>.failed` | Extraction failed | UTF-8 error detail |

No file for a digest = pending (not yet started).

**Aggregate markers** (at `.cog/` root): `ready`, `failed`, `downloading`. Same semantics as per-layer, representing the weight as a whole. Enable single `stat()` readiness checks.

### 3.4 Writer rules

1. All markers are created via write-to-temp + rename (atomic).
2. A `.ready` marker MUST NOT appear until underlying data is fully written and fsync'd.
3. Per-layer markers land before the aggregate.
4. Write sequence:
   - Create `.cog/manifest.json` and `.cog/downloading` when work starts.
   - For each layer (in any order, concurrently): `layers/<digest>.downloading` → extract → `layers/<digest>.ready`.
   - Once all layers have `.ready`, create `.cog/ready`.
   - If any layer fails: write `layers/<digest>.failed` with detail, then `.cog/failed` with summary.
5. Writers may delete `.downloading` when transitioning to a terminal state; terminal markers are authoritative.

### 3.5 Reader algorithm

```
1. stat <target>/.cog/ready        → weight usable, proceed
2. stat <target>/.cog/failed       → read payload, surface error, fail
3. stat <target>/.cog/downloading  → in progress, poll (optionally inspect layers/)
4. manifest.json exists, no state  → writer crashed; treat as failure
5. nothing exists                  → provider hasn't started; wait with timeout
```

Readers never write to `.cog/`. Pure observation.

Recovery from states 2 and 4 is the provider's responsibility. The consumer surfaces the error and waits; it does not attempt to repair `.cog/` state or retry extraction. A provider recovering from a crash MUST clean up the `.cog/` directory and restart delivery from scratch (the simplest correct approach) or resume from per-layer markers (an optimization).

### 3.6 Model image metadata

`cog build` writes `/.cog/weights.json` into the model image. This file:

- Signals to coglet that managed weights are active (presence = on, absence = legacy mode).
- Tells coglet what weights the model expects before calling `setup()`.

Schema (subset of `weights.lock`):

```json
{
  "weights": [
    {
      "name": "z-image-turbo",
      "target": "/src/weights",
      "digest": "sha256:abc123...",
      "layers": [
        { "digest": "sha256:aaa...", "size": 15000000 },
        { "digest": "sha256:bbb...", "size": 3957900840 }
      ]
    }
  ]
}
```

Coglet reads this on startup and waits for the state protocol (§3.5) to report ready for each weight before invoking `setup()`.

### 3.7 Target directory constraints

- Each weight's `target` must be unique within a model.
- Weight targets must be disjoint subtrees (no nesting).
- Both rules enforced at config validation time.
- Model code should ignore `.cog/` subdirectories in weight targets.

## 4. Worked Example: z-image-turbo (~32 GB)

Source: HuggingFace repo with 19 files (configs, tokenizers, safetensors shards).

**v0:** 19 weight entries in cog.yaml, 19 separate manifests, 19 blobs in the OCI index.

**v1:** 1 weight entry, 1 manifest, 8 layers:

| Layer | Contents | Size | Format |
|-------|----------|------|--------|
| 1 | Small files bundle (configs, JSONs, tokenizer, index files) | ~16 MB | compressed |
| 2 | text_encoder/model-00001-of-00003.safetensors | ~3.9 GB | uncompressed |
| 3 | text_encoder/model-00002-of-00003.safetensors | ~3.9 GB | uncompressed |
| 4 | text_encoder/model-00003-of-00003.safetensors | ~99 MB | uncompressed |
| 5 | vae/diffusion_pytorch_model.safetensors | ~167 MB | uncompressed |
| 6 | transformer/diffusion_pytorch_model-00001-of-00003.safetensors | ~9.9 GB | uncompressed |
| 7 | transformer/diffusion_pytorch_model-00002-of-00003.safetensors | ~9.9 GB | uncompressed |
| 8 | transformer/diffusion_pytorch_model-00003-of-00003.safetensors | ~4.6 GB | uncompressed |

A current producer would emit layer 1 as `tar+gzip` and layers 2-8 as `tar` (safetensors are on the skip list). A future producer could use `tar+zstd` for layer 1 or compress non-skip-list large files -- consumers don't care, they read the media type.

All 8 layers are independent. An extractor can download and unpack them in any order. Layer 1 writes to paths like `config.json`, `tokenizer.json`. Layers 2-8 each write to a single path like `text_encoder/model-00001-of-00003.safetensors`. No path conflicts.
