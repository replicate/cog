# Managed Weights v2 Design

> **Update (2026-04-24):** The separate `model` field described below was never implemented. During implementation we collapsed it back into `image`, which now serves as the single registry namespace for bundles, weights, and the local Docker tag. Read `<model>` in this doc as `<image>`. The override hierarchy (`COG_MODEL` env var, CLI flag, etc.) was dropped with it.

Iteration on the v0 prototype based on real-world testing with z-image-turbo, kimi-k2.5, and other models. Addresses format limitations, broken local dev workflow, and UX friction from the first pass.

## Context

v0 learnings:
- 1 file = 1 named weight = 1 blob is unworkable. z-image-turbo has 19 entries for what's logically one HF model. kimi-k2.5 would need 65+.
- Can't run models with managed weights locally. No pull/cache/mount workflow exists.
- Pulling weights requires local disk space (32GB model on 512GB drive = pain). Remote dev environments make this worse.
- Registry upload speed is poor (WAF + worker buffering + R2 latency).
- MediaType bug: layers marked uncompressed but actually gzipped.
- `COG_OCI_INDEX=1` env var gate is clunky.
- No discovery or listing commands.

Reference material:
- [Original spec](https://wiki.cfdata.org/spaces/AI/pages/1337667307/Spec+Declarative+Weights+in+Cog)
- [OCI format spec](https://wiki.cfdata.org/spaces/AI/pages/1337679582/Cog+Model+OCI+Format+Specification)
- [v0 testing writeup](https://wiki.cfdata.org/spaces/~mdwan/pages/1387684467/Building+testing+a+cog+model+with+managed+weights)

---

## 1. Weight Format Specification

### Named weight = directory with layered contents

A named weight (e.g., `z-image-turbo`) maps to a target directory (e.g., `/src/weights/`). Its OCI manifest has multiple layers that, when extracted, produce the directory contents.

**Unlike Docker image layers, weight layers are order-independent.** Each layer contains a disjoint set of files -- no file path appears in more than one layer. Layers can be downloaded and extracted in any order, concurrently, without coordination. This is a hard requirement driven by the size of weight data: ordered extraction would force buffering of out-of-order layers or serialized download, both unacceptable at multi-GB scale. The packing algorithm enforces disjointness by assigning each source file to exactly one layer.

### Layer strategy

All layers are **tars**. Tar provides a uniform format with built-in file metadata (path, size, permissions) at negligible overhead (512 bytes per entry). Extraction is a streaming operation -- no buffering required even for multi-GB files.

Two thresholds control layer construction:

| Threshold | Default | Purpose |
|-----------|---------|---------|
| `bundle_file_max` | 64MB | Per-file cutoff. Files smaller than this are eligible for bundling. Files at or above this always get their own layer. |
| `bundle_size_max` | 256MB | Max cumulative size of a single bundle tar before rolling to the next one. |

These are internal implementation dials, not user-facing config (for now). They're separate values because they serve different purposes: `bundle_file_max` decides *what* gets bundled (truly small files vs. individually significant ones), while `bundle_size_max` controls *how big* a single bundle download unit gets.

**Small files (<`bundle_file_max`):** Collected, stable-sorted by relative path, then packed sequentially into `.tar.gz` layers (each up to `bundle_size_max`). Stable sorting ensures deterministic layer assignment -- a file always lands in the same position in the sequence. Adding or removing a file only invalidates the tar it falls into (and possibly the next one if it shifts the boundary), not all small-file layers. Bundles are always gzip-compressed since they're dominated by compressible text formats (JSON, YAML, txt) and the overhead of compressing any binary files mixed in is negligible at these sizes.

- MediaType: `application/vnd.oci.image.layer.v1.tar+gzip`

**Large files (>=`bundle_file_max`):** Each file is its own layer as a single-entry tar. Compression is determined by file extension using a **skip-compression set**: known dense binary formats (`.safetensors`, `.bin`, `.gguf`, `.onnx`, `.parquet`, `.pt`, `.pth`) are stored as `.tar` (uncompressed) since they don't benefit from compression and the CPU cost matters at multi-GB scale. All other large files are stored as `.tar+gzip`.

- MediaType: `application/vnd.oci.image.layer.v1.tar` (uncompressed, for dense binary formats)
- MediaType: `application/vnd.oci.image.layer.v1.tar+gzip` (compressed, for everything else)

Standard OCI layer media types are used for ecosystem compatibility (crane, skopeo, containerd). The manifest's `artifactType: application/vnd.cog.weight.v1` distinguishes weight artifacts from runnable images.

The compression decision is visible in each layer's mediaType in the manifest -- no ambiguity about what the infra needs to do when extracting.

All tar paths are relative to the weight's target directory. Layers are extracted in any order -- no file appears in more than one layer, so there is no conflict resolution.

### Rationale

Real-world observation: weight files from HuggingFace are already sharded into reasonable chunks (kimi-k2.5 = 64x 9.8GB safetensors). The source files are natural layer boundaries. Bundling small files reduces manifest bloat. The two-strategy approach optimizes for transfer efficiency (small files compressed and bundled) and parallel download/unpack speed (large files as independent layers).

### Applied to z-image-turbo (~32GB)

v0: 19 weight entries in cog.yaml, 19 manifests, 19 blobs in the index.

v2: 1 weight entry in cog.yaml, 1 manifest, 8 layers:

| Layer | Contents | Size | Format |
|-------|----------|------|--------|
| 1 | Small files bundle (configs, jsons, tokenizer, index files) | ~16MB | .tar.gz |
| 2 | text_encoder/model-00001-of-00003.safetensors | ~3.9GB | .tar |
| 3 | text_encoder/model-00002-of-00003.safetensors | ~3.9GB | .tar |
| 4 | text_encoder/model-00003-of-00003.safetensors | ~99MB | .tar |
| 5 | vae/diffusion_pytorch_model.safetensors | ~167MB | .tar |
| 6 | transformer/diffusion_pytorch_model-00001-of-00003.safetensors | ~9.9GB | .tar |
| 7 | transformer/diffusion_pytorch_model-00002-of-00003.safetensors | ~9.9GB | .tar |
| 8 | transformer/diffusion_pytorch_model-00003-of-00003.safetensors | ~4.6GB | .tar |

With `bundle_file_max=64MB`: the ~16MB of JSON/text/config files bundle together. The 99MB and 167MB safetensors each get their own layer (above 64MB cutoff, and in the skip-compression set so uncompressed). The large safetensors are their own layers as before.

### OCI manifest structure (per named weight)

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

### Annotations

Annotations live at **three levels**, each making that level self-describing and independently useful:

**Weight manifest annotations** (on the manifest itself):

| Annotation | Description |
|-----------|-------------|
| `run.cog.weight.name` | The weight name (e.g., `z-image-turbo`) |
| `run.cog.weight.target` | Mount path in the container (e.g., `/src/weights`) |
| `run.cog.reference.type` | `weights` |
| `run.cog.reference.digest` | Digest of the model image this weight belongs to |

Duplicated on the **index entry** for the same manifest, so the index is inspectable without fetching child manifests (e.g., platform placement decisions, `cog weights list --remote`).

**Layer annotations** (on each layer descriptor):

| Annotation | Description |
|-----------|-------------|
| `run.cog.weight.content` | `bundle` (binpacked small files) or `file` (single large file) |
| `run.cog.weight.file` | For `file` layers: relative path within the weight directory |
| `run.cog.weight.size.uncompressed` | Uncompressed size in bytes (string, per OCI spec) |

**Namespace convention:** Annotations use `run.cog.*` (reverse-domain of the project's TLD, cog.run). Layer media types use standard OCI types (`application/vnd.oci.image.layer.v1.*`) for ecosystem compatibility. The custom `application/vnd.cog.weight.v1` is used only as the manifest's `artifactType` discriminator.

### Media types (updated)

| Media Type | Description |
|-----------|-------------|
| `application/vnd.cog.weight.v1` | Weight manifest `artifactType` |
| `application/vnd.oci.image.layer.v1.tar` | Uncompressed tar layer (standard OCI) |
| `application/vnd.oci.image.layer.v1.tar+gzip` | Gzip-compressed tar layer (standard OCI) |
| `application/vnd.oci.image.layer.v1.tar+zstd` | Zstd-compressed tar layer (standard OCI, future) |

Layers use standard OCI media types for ecosystem compatibility. The `artifactType` on the manifest is the discriminator for weight artifacts, not the layer media types.

### Note on chunking

Training frameworks already shard weight files into manageable sizes (kimi-k2.5 = 64x 9.8GB, FLUX = 3x ~4-10GB). We don't expect to need further chunking. If an edge case arises where a single file is unmanageably large, splitting across layers with reassembly metadata is a possible escape hatch, but not something we're designing for.

---

## 2. cog.yaml Configuration

### New `model` field

A new top-level `model` field establishes the registry namespace for this model. Everything (images, weights, bundles) derives from it.

```yaml
model: registry.cloudflare.com/<account>/z-image-turbo
```

Derived paths:

| Thing | Registry path |
|-------|--------------|
| Bundle (OCI index) | `<model>:v1`, `<model>:latest` |
| Named weight | `<model>/weights/<name>` |
| Weight version | `<model>/weights/<name>:latest` |

Override hierarchy (highest wins):
1. CLI flag: `cog push <ref> --tag v1`
2. Environment: `COG_MODEL=registry.staging.../z-image-turbo`
3. Config file layering: `cog.yaml` + `cog.prod.yaml` (last-in-wins)
4. cog.yaml `model` field

This handles multi-environment (prod, staging, r8.im) without per-weight or per-registry config.

### Weights stanza

```yaml
weights:
  - name: z-image-turbo
    target: /src/weights
    source:
      uri: hf://stabilityai/z-image-turbo
      exclude: ["*.onnx", "*.bin", "*.msgpack"]

  - name: my-lora
    target: /src/weights/lora
    source:
      uri: ./fine-tuned-weights/
```

**Fields:**

- `name` (required): Unique identifier. Maps to `<model>/weights/<name>` in the registry.
- `target` (required): Absolute path where the weight directory is mounted in the container.
- `source` (optional): Import configuration -- provenance and re-import config, not a runtime dependency.
  - `uri`: Source location. Schemes: `hf://`, `s3://`, `http://`, `https://`, `oci://` (reference another weight in a registry), local filesystem paths.
  - `exclude`: Glob patterns for files to skip during import.
  - `include`: Glob patterns for files to include (allowlist mode).

At build/run/deploy time, only `name` + `target` matter. Weights are resolved from the registry using the digest pinned in `weights.lock`. The `source` block documents where they came from and how to re-import them.

**Open question: versioning in cog.yaml.** Should there be a `version:` or `tag:` field on a weight entry? The lockfile already pins the exact manifest digest, and git history provides rollback. The main case for an explicit version field would be shared weight repos across models (e.g., "use z-image-turbo:v3 from a common weights repo"). We don't have this use case yet, and each model has its own weight namespace. Leaving this out for now -- the lockfile is the version pin. Can revisit if shared/cross-model weights become a pattern.

### Lockfile (`weights.lock`)

Generated by `cog weights import`. Records exact layer digests, sizes, and import metadata. Committed to the repo as the reproducibility guarantee.

```json
{
  "version": "1.0",
  "weights": [
    {
      "name": "z-image-turbo",
      "target": "/src/weights",
      "source": "hf://stabilityai/z-image-turbo",
      "sourceFingerprint": "commit:a1b2c3d4e5f6",
      "importedAt": "2026-04-16T17:27:07Z",
      "digest": "sha256:abc123...",
      "size": 32600000000,
      "layers": [
        {
          "digest": "sha256:aaa...",
          "size": 15000000,
          "sizeUncompressed": 18500000,
          "mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
          "content": "bundle"
        },
        {
          "digest": "sha256:bbb...",
          "size": 3957900840,
          "sizeUncompressed": 3957900840,
          "mediaType": "application/vnd.oci.image.layer.v1.tar",
          "content": "file",
          "file": "text_encoder/model-00001-of-00003.safetensors"
        }
      ]
    }
  ]
}
```

**`sourceFingerprint`** records the source's version identity at import time, enabling `cog weights check --source` to detect upstream changes without re-downloading. The value is a prefixed string identifying the scheme:

| Prefix | Source type | Example |
|--------|-----------|---------|
| `commit:` | HuggingFace repo (git commit SHA) | `commit:a1b2c3d4e5f6` |
| `etag:` | HTTP/HTTPS (ETag header) | `etag:"abc123"` |
| `sha256:` | Content hash (fallback) | `sha256:def456...` |
| `md5:` | S3 object (ETag = MD5) | `md5:789abc...` |
| `timestamp:` | Last-Modified header | `timestamp:2026-04-16T17:27:07Z` |

The prefix makes fingerprints self-describing and avoids ambiguity when comparing across source types. `cog weights check --source` fetches the current fingerprint from the source and compares -- no content download needed for the common "nothing changed" case.

### Relationship between `model` and `image`

Today `image` serves two purposes: it names the Docker image that `cog build` produces locally, and it's the push destination for `cog push`. The new `model` field takes over the registry namespace role.

- **`model`**: The registry root. Used by `cog push`, `cog weights import/pull/check`, and all registry operations. Defines the namespace for bundles, images, and weights.
- **`image`**: The local Docker image name that `cog build` tags. Still useful for local dev (`cog build` -> `docker run <image>`). If omitted and `model` is present, cog can derive a local image name from the model field.

For models without managed weights, `image` alone continues to work exactly as before. `model` is only needed when using managed weights or the bundle/index push flow.

`COG_OCI_INDEX=1` env var is removed. If `weights` is in cog.yaml, `cog push` produces a bundle (OCI index).

**Worked examples:**

| cog.yaml has | `cog build` local tag | `cog push` pushes | Notes |
|---|---|---|---|
| `image` only | Tags as `<image>` | Single manifest to `<image>` | Today's behavior, unchanged |
| `model` only | Tags as `<model-basename>:latest` | Single manifest to `<model>` | Local tag derived from model basename |
| `model` + `image` | Tags as `<image>` | Single manifest to `<model>` | `image` controls local name, `model` controls registry |
| `model` + `weights` | Tags as `<model-basename>:latest` | OCI index to `<model>` (image manifest + weight manifests) | Bundle format, weights stored under `<model>/weights/<name>` |
| `model` + `image` + `weights` | Tags as `<image>` | OCI index to `<model>` (image manifest + weight manifests) | Full setup: local name from `image`, bundle push to `model` |
| `image` + `weights` | Tags as `<image>` | **Error**: `model` required when using managed weights | Weights need a registry namespace |

---

## 3. CLI Commands

### Weight manager commands

**`cog weights import <name>`**

Reads source config from cog.yaml, downloads source files, packages into tar layers, loads into Docker, pushes to registry, and updates the lockfile.

V1 pipeline (stage-to-disk):
1. Download source files to a temp directory on the local filesystem
2. Apply `include`/`exclude` filters
3. Classify files: small (<`bundle_file_max`) vs large (>=`bundle_file_max`)
4. Pack into tar layers with deterministic properties (stable sort by relative path, zero timestamps in tar headers, uid/gid 0, permissions 0644/0755)
5. Build OCI image via `go-containerregistry` (`mutate.AppendLayers` on `empty.Image`)
6. Load into Docker daemon via `daemon.Write()` (pipes through `ImageLoad`)
7. Push to registry via cog's existing multipart OCI push (`registry.WriteLayer()` + `registry.PushImage()`)
8. Update `weights.lock` with manifest digest, layer digests, sizes, source fingerprint

Streaming import (source → tar → registry without local staging) is a future optimization. The OCI distribution spec supports chunked upload, and large files could be streamed through a tar header wrapper and hashed on the fly. Deferred because staging is simpler and v1 assumes a local daemon with disk.

```bash
cog weights import z-image-turbo
# Streaming from hf://stabilityai/z-image-turbo...
# Excluding: *.onnx, *.bin, *.msgpack
# Packaging 19 files into 8 layers (32.6 GB)...
# Pushing to registry.cloudflare.com/acct/z-image-turbo/weights/z-image-turbo...
# [============================] 8/8 layers pushed
# Updated weights.lock
```

Import builds layers locally on the connected Docker daemon, pushes them to the registry, and **keeps them in Docker's content store** after upload. This means `cog weights pull` after an import is a no-op -- the layers are already present. This is the default behavior, not a flag. Byte-identical layers between local build and registry push is a hard requirement (deterministic tar packing, stable sort order, no timestamps in tar headers).

Flags:
- `--all`: Import all weights with source configs
- `--dry-run`: Show what would be imported without pushing
- `--force`: Re-import even if lockfile digests match
- `--purge-after`: Remove local layers from Docker after successful push. For CI environments where disk space matters and you won't be running `cog predict` on the same machine.

**`cog weights check [name]`**

Validates lockfile digests match the registry. For CI pipelines.

```bash
cog weights check
# z-image-turbo: OK (8 layers, 32.6 GB)
```

Flags:
- `--source`: Also check if source has changed since last import
- Exit code 0 = all good, non-zero = problems

### Developer commands

**`cog weights pull [name]`**

Downloads weight layers from registry into Docker. Required before `cog predict` for models with managed weights.

```bash
cog weights pull
# Pulling z-image-turbo (32.6 GB, 8 layers)...
# [============================] 8/8 layers complete
# Weights cached in Docker. Ready for cog predict.
```

Skips layers already cached (by digest). Works over Docker socket (local or remote).

**`cog weights purge [name]`**

Removes cached weight data from Docker. If `name` is omitted, purges all cached weights.

```bash
cog weights purge z-image-turbo
# Removed z-image-turbo (32.6 GB freed)

cog weights purge
# Removed all cached weights (64.2 GB freed)
```

### Discovery commands

**`cog weights list`**

Lists weights for this model. Reads from cog.yaml/lockfile by default, queries registry with `--remote`.

```bash
cog weights list
# NAME              SIZE      LAYERS  IMPORTED
# z-image-turbo     32.6 GB   8       2026-04-16

cog weights list --remote
# Shows what's in the registry, not just lockfile
```

**`cog weights inspect [name]`**

Detailed view of a weight: layers, sizes, digests, source provenance, local cache status.

Flags: `--json`, `--remote`

### Transfer concurrency and retries

Layer uploads (`import`) and downloads (`pull`) run **4 layers in parallel** by default. This balances throughput against memory pressure (each in-flight layer needs buffer space for hashing/compression). Override with `--concurrency=N`.

Failed transfers are retried up to 3 times with exponential backoff (1s, 4s, 16s). Transient HTTP errors (429, 502, 503, 504) and connection resets trigger retries; 4xx client errors (except 429) fail immediately. Progress output shows per-layer status so it's clear which layer is retrying.

### Integration with existing commands

**`cog predict` / `cog run`:** Before starting the container, checks weights are cached locally. If not cached and stdin is a TTY, prompts: `Weights not cached (32.6 GB). Pull now? [Y/n]`. On confirmation, runs the equivalent of `cog weights pull` inline. In non-TTY mode (CI, scripts), fails with a clear error and exit code 1: `Weights not cached. Run 'cog weights pull' first.`

**`cog push`:** Validates all weights are in the registry via lockfile digests. Assembles OCI index with model image + weight manifests. No env var gate -- if `weights` is in cog.yaml, it's a bundle.

---

## 4. Model Awareness & SDK API

Today, models have no concept of managed weights. The workaround is requiring all weights to be present on disk before the container starts, and hacking model code to skip download logic when files already exist. This needs to change.

### Build-time: embed weight metadata in the image

`cog build` writes `/.cog/weights.json` (derived from the lockfile) into the model image. This gives the running container knowledge of what weights it expects, their digests, mount paths, and sizes. Its presence is also the signal that managed weights are active (see below).

### Runtime: managed weights signal

The cog runtime (coglet) needs to know a model uses managed weights. Detection is based on the presence of `/.cog/weights.json` in the image -- no env var, no separate flag. If the file exists, managed-weight behavior is active: coglet enforces the readiness protocol below before calling `setup()`.

### Runtime state protocol

The contract between the weight **provider** (platform infra in prod, `cog weights pull` in dev) and the weight **consumer** (coglet, SDK). Communicated entirely through files in the mounted weight directory -- no out-of-band signaling, no coupling between provider and consumer beyond a shared on-disk format.

The provider writes state into each weight's target directory under a hidden `.cog/` subtree. Consumers only read:

```
<target>/.cog/
├── manifest.json              # written once, identifies what's being delivered
├── ready                      # aggregate: all layers complete
├── failed                     # aggregate: any layer failed (payload: short summary)
├── downloading                # aggregate: work started, not yet ready/failed
└── layers/
    ├── sha256-aaa.ready
    ├── sha256-bbb.downloading
    └── sha256-ccc.failed      # payload: full error detail
```

Digests in filenames replace `:` with `-` for filesystem portability.

**`manifest.json`** -- written atomically by the provider at the start of work, never mutated. Identifies the weight and its expected layer set so consumers can detect version skew and fail fast rather than waiting for layers that will never arrive.

```json
{
  "version": "1",
  "name": "z-image-turbo",
  "digest": "sha256:model123...",
  "layers": [
    "sha256:aaa...",
    "sha256:bbb...",
    "sha256:ccc..."
  ]
}
```

**Per-layer markers** -- three states, three atomic filenames:

| File | Meaning | Payload |
|------|---------|---------|
| `<digest>.downloading` | Provider is actively working on this layer | empty |
| `<digest>.ready` | Layer fully extracted and fsync'd | empty |
| `<digest>.failed` | Extraction failed | UTF-8 error detail |

No file = pending.

**Aggregate markers** -- same three filenames at `.cog/` root, representing the weight as a whole. Let consumers answer "is this weight ready?" with a single `stat()` rather than enumerating every layer file.

#### Writer rules (atomicity + ordering)

- All markers are created via write-to-temp + rename. Never mutate in place.
- A `.ready` marker (per-layer or aggregate) MUST NOT appear until the underlying data is fully written and fsync'd. A reader seeing `.ready` must be able to trust it.
- Per-layer markers land **before** the aggregate. Write order:
  1. Create `.cog/manifest.json` and `.cog/downloading` when work starts.
  2. For each layer: create `layers/<digest>.downloading`, extract, then create `layers/<digest>.ready`.
  3. Once every expected layer has `.ready`, create `.cog/ready` at root.
  4. If any layer fails, write `layers/<digest>.failed` with full detail, then `.cog/failed` at root with a short summary.
- Writers may delete `.downloading` markers when transitioning to a terminal state; the terminal markers are authoritative either way.

#### Reader algorithm

```
1. stat <target>/.cog/ready        → weight usable, proceed
2. stat <target>/.cog/failed       → read payload, surface error, fail
3. stat <target>/.cog/downloading  → in progress, poll (optionally drill into layers/)
4. <target>/.cog/manifest.json exists, no state file → writer crashed; treat as failure
5. nothing exists                  → provider hasn't started; wait with timeout
```

Readers never write to `.cog/`. Pure observation. One `stat()` covers the hot path; per-layer files are for progress UX and debugging (`cog weights inspect`).

#### Target directory constraints

- Each weight's `target` must be unique within a model. Two weights sharing a target is a validation error.
- Weight targets must be disjoint subtrees -- neither an ancestor nor a descendant of another weight's target. Nested targets would put one weight's data inside another's `.cog/` namespace.
- Both rules are enforced at cog.yaml load time with clear error messages.
- Model code must ignore the `.cog/` subdirectory in weight targets. Dot-prefixing keeps it invisible to most glob patterns; document the convention.

#### Dev/prod parity

This protocol is the same whether weights are delivered by platform infra (prod) or by `cog weights pull` locally. A Docker volume produced locally is bit-for-bit consumable by the same coglet code that runs in production. No code paths diverge.

One volume can be mounted read-only into many containers simultaneously, and each consumer independently observes readiness without the provider needing to know who's consuming. Supports future lazy-loading FS drivers and multi-tenant weight volumes with no protocol change.

### SDK API (future, needs design)

Currently all weights must be ready before `setup()` starts. For models with massive weights, an API that lets `setup()` await individual weights would enable:

```python
class Predictor(BasePredictor):
    def setup(self, weights: WeightManager):
        # Start loading the text encoder while transformer downloads
        text_enc = weights.get("z-image-turbo", "text_encoder/")
        self.text_encoder = load(text_enc.path)

        # Transformer might still be downloading -- this blocks until ready
        transformer = weights.get("z-image-turbo", "transformer/")
        self.transformer = load(transformer.path)
```

Implementation reads the per-layer `.ready` markers from the state protocol above and blocks (with timeout) until the layers covering a requested subpath are present. No new mechanism needed -- the protocol is already granular enough.

This is an optimization, not a blocker. For v2 the contract is: all weights are ready when `setup()` is called. The state protocol and embedded `/.cog/weights.json` give us the foundation to extend.

---

## 5. Local Dev Workflow

### Design constraints

**Hard constraint: no disk duplication.** 500GB of weight layers in Docker's content store cannot require another 500GB in a volume or anywhere else. Weight data exists once. This rules out any approach that copies layer contents into named Docker volumes.

**V1 assumes a local Docker daemon.** Remote `DOCKER_HOST` is an eventual goal (see §5.7), but v1 can use host filesystem paths from `docker inspect`. This unlocks zero-copy overlay mounting.

**V2 goal: remote `DOCKER_HOST` support.** Eventually, `cog predict` on a MacBook with `DOCKER_HOST=tcp://gpu-server:2375` should work -- weights download to the remote host, predict runs on the remote GPU, no weight bytes touch the laptop. The v1 architecture should not preclude this.

### First time setup

```bash
git clone <model-repo>
cd z-image-turbo

cog login registry.cloudflare.com    # if needed

cog weights pull                      # downloads to Docker, not local FS
# Pulling z-image-turbo (32.6 GB, 8 layers)...
# [============================] 8/8 layers complete

cog predict -i prompt="a cat wearing a hat"
```

### What happens on `cog predict`

1. Reads `weights` from cog.yaml + `weights.lock`
2. Checks weight images exist in Docker (by digest)
3. If missing: TTY prompt to pull, or error in non-TTY mode (see §3)
4. Builds/uses the model image (standard cog build)
5. Creates/reuses weight containers for overlay mount (see §5.5)
6. Bind-mounts each weight container's overlay directory at the target path, read-only
7. Runs model container; predictor's `setup()` sees `/src/weights/` with all files, as if baked in

### Weight manager workflow

```bash
# Edit cog.yaml to add/modify weight source
vim cog.yaml

# Import from source to registry (also loads into Docker)
cog weights import z-image-turbo

# Verify lockfile matches registry
cog weights check

# Pull locally to test (no-op if import already loaded it)
cog weights pull
cog predict -i prompt="test"

# Commit
git add cog.yaml weights.lock
git commit -m "Update z-image-turbo weights"
```

### Weight image representation

Weight layers are stored as a Docker image in the daemon's image store. This is the same content-addressable storage Docker uses for all images -- we get layer deduplication for free.

The weight image:
- Is assembled from `empty.Image` + weight tar layers via `go-containerregistry` (`mutate.AppendLayers`)
- Uses standard OCI layer media types, `run.cog.*` annotations on the manifest, `artifactType: application/vnd.cog.weight.v1`
- Has minimal/empty OCI config (no entrypoint, no cmd, no VOLUME declarations)
- Is tagged locally as `cog-weights/<name>:<short-digest>`
- Is content-addressable: if two models share a base weight set with identical layers, Docker stores those layers once

### Import pipeline (v1)

Import downloads source files, packs them into tar layers, loads the result into Docker, and pushes to the registry. V1 stages files to a temp directory on the local filesystem.

**Step-by-step:**

1. **Download** source files to a temp directory. Source-specific: `hf://` uses the HuggingFace Hub API (with LFS), `s3://` uses the AWS SDK, `http://` streams to disk, local paths are read directly. Apply `include`/`exclude` filters from cog.yaml.

2. **Classify** files by size against `bundle_file_max` (default 64MB).

3. **Pack tar layers:**
   - Small files: stable-sort by relative path, pack sequentially into `.tar.gz` bundles (each up to `bundle_size_max`). Compression is always gzip since small files are dominated by compressible text formats.
   - Large files: each file becomes a single-entry `.tar`. Dense binary formats (safetensors, bin, gguf, onnx, parquet, pt, pth) are uncompressed; everything else gets gzip.

4. **Deterministic tar properties** (required for byte-identical digests):
   - Stable sort by relative path within each bundle
   - Zero `mtime`, `atime`, `ctime` in tar headers
   - UID/GID 0/0 (root)
   - Permissions: 0644 for files, 0755 for directories
   - No extended attributes, no system-specific metadata
   - PAX tar format for paths > 100 chars

5. **Build OCI image:**
   ```go
   for _, tarPath := range layerPaths {
       layer, _ := tarball.LayerFromFile(tarPath,
           tarball.WithMediaType(types.MediaType(mediaType)))
       layers = append(layers, layer)
   }
   img, _ := mutate.AppendLayers(empty.Image, layers...)
   ```

6. **Load into Docker daemon** via `daemon.Write()` (internally pipes a `docker save`-format tar through `ImageLoad`).

7. **Push to registry** via cog's existing multipart OCI push infrastructure (`registry.WriteLayer()` for layer blobs, `registry.PushImage()` for the manifest). This reuses the push code that already handles WAF/worker buffering/R2 latency.

8. **Update `weights.lock`** with manifest digest, layer digests, sizes, media types, and source fingerprint.

The image stays in Docker's store after push (`--purge-after` removes it for CI). This means `cog weights pull` after an import is a no-op.

### Pull

Standard `docker pull` by digest reference:

```
docker pull <registry>/<model>/weights/<name>@sha256:<manifest-digest>
```

Docker handles auth (via credential helpers), resumption, and layer deduplication. If a layer already exists in the content store (from a previous pull, an import, or a shared base weight), Docker skips it.

After pull, tag locally for easy reference:
```
docker tag <registry>/...@sha256:<digest> cog-weights/<name>:<short-digest>
```

Multiple weights pull in parallel (4 default, `--concurrency=N` override). Each weight is an independent image with its own manifest.

### Mount via overlay2 (v1)

The key insight: Docker already solves the "merge multiple layers into a single directory" problem. When `docker create` instantiates a container from an image, Docker's overlay2 storage driver creates an overlay filesystem that combines all layers into a single unified view at `GraphDriver.Data.MergedDir`. This is a kernel-level overlay mount -- **zero data duplication**.

**Mount flow:**

1. **Verify storage driver.** Call `docker info` (Go SDK: `client.Info(ctx)`), check `Driver == "overlay2"`. If not overlay2, error with a clear message: `"Managed weights require the overlay2 storage driver. Current driver: <driver>. See https://docs.docker.com/storage/storagedriver/overlayfs-driver/"`.

2. **Create weight container** (if not already exists):
   ```
   docker create --read-only \
     --name cog-wt-<name>-<short-digest> \
     cog-weights/<name>:<short-digest>
   ```
   The container is never started. `docker create` allocates the overlay filesystem (merging all image layers) and a thin writable top layer. `--read-only` marks the root filesystem read-only at the OCI runtime level.

   If the container already exists (409 Conflict from a concurrent `cog predict`), reuse it.

3. **Get the overlay merge path.** Call `docker inspect` (Go SDK: `client.ContainerInspect(ctx, id)`), read `GraphDriver.Data["MergedDir"]`. This is a host filesystem path like `/var/lib/docker/overlay2/<id>/merged/` that contains the unified view of all weight layers.

4. **Bind-mount into model container:**
   ```
   docker run -v <MergedDir>:<target>:ro ... model-image
   ```

**Multiple weights** each get their own container and bind mount:
```
docker run \
  -v <MergedDir-base>:/src/weights:ro \
  -v <MergedDir-lora>:/src/lora:ro \
  model-image
```

**Container lifecycle:**
- Weight containers are created once and reused across predict runs. They persist until explicitly purged.
- Named deterministically: `cog-wt-<name>-<short-digest>` (predictable, idempotent).
- Never started -- just created. The overlay mount is set up by `docker create`, not `docker start`.
- `cog weights purge` removes weight containers, then weight images.

**Cache invalidation:** When weights change (new import/pull), the digest changes, so the container name changes. The old container + image become orphaned. `cog weights purge` removes them; a future enhancement could auto-prune stale weight containers on `cog weights pull`.

### Implementation notes

**Docker Desktop (Mac/Windows):** Docker runs inside a Linux VM. The `MergedDir` path is inside the VM, not on the host filesystem. However, bind mounts in `docker run -v` are resolved by the Docker daemon (inside the VM), so the path is accessible. **Verify during implementation** that this works end-to-end on Docker Desktop for Mac.

**Rootless Docker:** Overlay paths are within the user's namespace. `docker inspect` returns the correct path. Expected to work, but should be tested.

**Writable layer overhead:** `docker create --read-only` still creates a thin writable overlay layer (the container's init layer). This is small (~empty) and acceptable overhead. A future optimization could use containerd's `snapshotter.View()` for a true read-only snapshot with no writable layer, but this requires the containerd socket (not available through the Docker Engine API).

**Concurrent predict runs:** Multiple `cog predict` calls may race to create the same weight container. Handle the Docker API's 409 Conflict response by reusing the existing container rather than failing.

### Future: remote `DOCKER_HOST` and BuildKit (v2)

The v1 overlay2 approach requires host filesystem access (`MergedDir` is a host path), which doesn't work when the Docker daemon is on a remote machine. Two approaches for v2:

**`--volumes-from` with VOLUME declarations:** The weight image declares `VOLUME` at the target path in its OCI config. `docker create` the weight container, then the model container uses `--volumes-from cog-wt-<name>:ro` to mount the weight directory. This is zero-copy and works over the Docker API (no host paths). The tradeoff: the weight image config becomes coupled to the target path. Multiple weights work by chaining `--volumes-from`.

**BuildKit as the execution engine:** The entire import pipeline can be expressed as an LLB graph:
- `llb.HTTP(url)` downloads weight files on the daemon (remote host has the bandwidth)
- `llb.Diff(Scratch(), state)` controls layer boundaries (each Diff becomes a separate OCI layer)
- `llb.Merge(layers)` combines layers into a multi-layer result
- The "moby" exporter stores the result in Docker's image store (`store=true`) and can also push to a registry (`push=true`) in the same operation

This means `cog weights import` on a remote `DOCKER_HOST` would download and process weights entirely on the remote machine -- no bytes flow through the developer's laptop. The client sends a small LLB definition (protobuf) and monitors progress.

BuildKit's `moby` exporter (available in Docker Engine's embedded BuildKit) wraps the `image` exporter with Docker-specific callbacks. It supports `push=true` + `store=true` simultaneously, `oci-mediatypes=true`, and annotations. Cog already uses BuildKit via the Docker Engine's gRPC endpoint (see `pkg/docker/buildkit.go`).

The catch: cog's custom multipart push handles WAF/worker buffering issues that BuildKit's built-in containerd push may not. V2 would need to either fix the registry-side issues, use BuildKit for local store only and cog's push separately, or register a custom push implementation.

---

## 6. Registry Conventions

### Namespace structure

```
<model>                              # OCI index (bundle)
<model>:v1                           # Tagged bundle version
<model>/weights/<name>               # Named weight repository
<model>/weights/<name>:latest        # Latest weight version
<model>/weights/<name>@sha256:...    # Specific weight version
```

### Multi-environment support

The `model` field in cog.yaml is the default. Override for different environments:

```bash
# Production (default from cog.yaml)
cog push --tag v1

# Staging (env override)
COG_MODEL=registry.staging.cloudflare.com/acct/z-image-turbo cog push --tag v1

# Different registry (positional override)
cog push r8.im/username/z-image-turbo --tag v1
```

Config file layering (`cog.yaml` + `cog.prod.yaml`, last-in-wins) handles persistent per-environment config without env vars.

### Cross-repo weight sharing

Multiple models can share the same base weights without duplicating storage. Use `oci://` as the source URI to reference a weight from another model's registry:

```yaml
weights:
  - name: shared-base
    target: /src/weights/base
    source:
      uri: oci://registry.cloudflare.com/acct/base-model/weights/shared-base@sha256:abc123...
```

On `cog weights import`, if the source is `oci://` within the same registry, cog uses [cross-repository blob mounts](https://github.com/opencontainers/distribution-spec/blob/main/spec.md#mounting-a-blob-from-another-repository) (`POST /blobs/uploads/?mount=<digest>&from=<source-repo>`) to link existing blobs into the target repository without re-uploading. The registry deduplicates at the storage layer -- the 32GB of safetensors exist once regardless of how many models reference them.

For cross-registry `oci://` sources, layers are streamed through as a normal import (no blob mount available).

---

## 7. Changes from v0

No migration path needed -- v0 was a prototype used only for infra testing, no real models depend on it.

- `weights` stanza in cog.yaml changes shape (file-per-entry -> directory-per-entry)
- `weights.lock` format changes (new version field, layer-based)
- `cog weights build` and `cog weights push` replaced by `cog weights import`
- `COG_OCI_INDEX=1` env var removed
- Runtime weight readiness communicated via on-disk `.cog/` state protocol in each weight target dir
- v0 weight blobs in the registry can be deleted

---

## Open Questions

- **Config file layering**: The `cog.yaml` + `cog.prod.yaml` pattern needs design. Is it merge? Override? Last-in-wins per field? Not blocking for v2 -- env var and CLI flag overrides cover the multi-environment case for now.
- **Streaming import** (deferred post-v1): Can we stream source → tar → registry upload using OCI chunked upload without local staging? The OCI distribution spec supports `POST` + `PATCH` chunks + `PUT ?digest=` finalization, so it should be feasible for large files. Small-file bundles need to be assembled in memory/temp before upload (they're small, this is fine). V1 stages to disk (see §5.4); streaming is an optimization for when temp disk space is constrained.
- **`target` in OCI config blob**: Currently `target` is a manifest annotation (`run.cog.weight.target`). OCI-idiomatically, operational metadata like mount paths belongs in the config blob (analogous to how Docker puts CMD/ENV in the image config, not in annotations). Layers stay as pure content-addressable artifacts either way. Worth moving `target` (and possibly `name`) to the config blob, using a typed config mediaType (`application/vnd.cog.weight.config.v1+json`) instead of the empty config. Needs decision.
- **Docker Desktop overlay2 verification**: V1 mount depends on bind-mounting `GraphDriver.Data.MergedDir` from `docker inspect`. On Docker Desktop (Mac/Windows), this path is inside the Linux VM. Needs end-to-end verification that `docker run -v <MergedDir>:<target>:ro` works correctly in this environment.
- **`--mount type=image` (containerd/Podman)**: Containerd and Podman support `--mount type=image,source=<image>,target=<path>,readonly` which directly mounts an image's filesystem into a container read-only -- no intermediate container, no overlay path lookup. Docker Engine doesn't support this yet, but if/when it does, it's the cleanest mount primitive for our use case. The weight image spec is consistent regardless of mount mechanism; this is purely a runtime optimization.
