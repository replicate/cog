# Managed Weights: Import and Local Run Design

Refinement of `2026-04-16-managed-weights-v2-design.md` driven by two
blocking limitations that surfaced during v2 implementation:

1. Remote sources can't stage fully to local disk. Kimi K2.5 (~600 GB,
   ~66 layers) does not fit on a developer laptop. Today's pipeline —
   `Source.Fetch → localDir → walkSourceDir → Pack → push` — is
   structurally incapable of handling sources larger than free disk.
2. There is no local-run story that avoids doubling storage. The v2
   design leans on Docker overlay2 `MergedDir`, which works but ties
   us to a Docker-only, overlay2-only world and doesn't compose with
   the remote-`DOCKER_HOST` workflow that's the eventual unlock.

This plan refines the source provider interface, splits lockfile state
into canonical vs pending, introduces a local `WeightStore` abstraction,
and redefines `cog weights pull` as a synthesis operation rather than a
registry fetch.

Scope: producer-side (cog CLI). The on-registry format — `specs/weights.md`
— is unchanged. Where this plan adds fields to `weights.lock` or new
state files, those are cog's producer-side bookkeeping; a consumer can
derive everything from the OCI artifact.

---

## 1. `Source` as a thin provider

Today `Source.Fetch(ctx, uri, projectDir) → localDir` forces full
materialization before anything else can happen. Replace with a
capability-based interface that lets the weights subsystem drive the
pipeline one layer at a time.

```go
type Source interface {
    // Inventory returns everything needed to build a lockfile without
    // transferring payload bytes: per-file path, size, content digest,
    // and the source's version identity.
    //
    // For file://, this walks and hashes. For hf://, it reads the
    // HuggingFace Hub API and LFS/xet pointer sha256s. For oci://, it
    // reads the source manifest's config blob.
    Inventory(ctx context.Context, uri, projectDir string) (Inventory, error)

    // Open returns a byte reader for a single file in the source.
    // Called on demand during packing; there's no guarantee of
    // locality — hf:// streams from the CDN, file:// opens the local
    // file, s3:// starts a GetObject.
    Open(ctx context.Context, uri, path string) (io.ReadCloser, error)

    // NativeBlobRef returns a scheme-specific reference for a file
    // when one exists. Optional; the future optimization seam for
    // cross-repo blob mounts (oci://) and HF object URL pass-through.
    // v1 implementations return (BlobRef{}, false).
    NativeBlobRef(uri, path string) (BlobRef, bool)
}

type Inventory struct {
    Files       []InventoryFile
    Fingerprint weightsource.Fingerprint
}

type InventoryFile struct {
    Path   string // relative to the weight target
    Size   int64
    Digest string // "sha256:<hex>", supplied by the source
}
```

### Why thin

There are two concerns in every import: "what does this source look
like" (source-specific) and "turn this into lockfile/layers/registry
state" (common across sources). Thin providers expose the first;
the weights subsystem owns the second. One pack algorithm, one
lockfile writer, one registry push path — only the source
implementations vary.

A fat provider (source owns the whole pipeline) was considered and
rejected for v1. It pushes byte-identical-layers-across-sources from a
single well-tested code path to N source implementations, each of which
would have to get tar packing exactly right.

### Sources with cheap inventory

| Scheme | Inventory cost |
|--------|----------------|
| `file://` | Walk + hash every file (unavoidable; same cost as today's fingerprint). |
| `hf://` | HuggingFace Hub API + LFS pointer sha256. Free — no blob downloads. |
| `oci://` | Read source manifest's config blob. Free. |
| `s3://` | `ListObjectsV2` + ETag. ETag is MD5 for single-part uploads, opaque for multipart; treated as an opaque source identity marker. |
| `http://`, `https://` | Typically a single opaque object; needs stage-and-hash. Least efficient source, least common at scale. |

## 2. Three state files

**`cog.yaml`** — user intent. Unchanged.

**`weights.lock`** — canonical registry state. Committed. Written
only when an import fully succeeds for a weight. Schema unchanged
from v1: `WeightLockLayer` carries the tar blob digest, media type,
and sizes; `WeightLockFile` carries per-file path/size/digest and the
blob digest of the containing layer. Everything the local store and
import pipeline need is derivable from these fields.

(An earlier revision proposed adding a per-layer `ContentsDigest` so
the store could cache-check by layer in a single compare. That was
dropped once the store switched to file-granular storage: `Files[]`
filtered by layer gives us the same answer without a new field.)

**`.cog/weights-state/<name>.json`** — pending / in-flight state.
Not committed (added to `.gitignore` as part of this work). One file
per weight. Written atomically (tmp + rename) on every layer state
transition.

```jsonc
{
  "version": 1,
  "name": "z-image-turbo",
  "target": "/src/weights",
  "source": {
    "uri": "hf://Tongyi-MAI/Z-Image-Turbo",
    "fingerprint": "commit:a1b2c3d4...",
    "include": [],
    "exclude": []
  },
  "plannedSetDigest": "sha256:def...",
  "plannedFiles": [
    { "path": "config.json", "size": 1234, "digest": "sha256:f01...", "layer": 0 }
  ],
  "plannedLayers": [
    {
      "index": 0,
      "mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
      "state": "pushed",              // planned | packing | pushing | pushed | failed
      "blobDigest": "sha256:bbb...",  // populated after packing
      "size": 15000000,
      "sizeUncompressed": 18500000,
      "error": null                   // populated if state=failed
    }
  ],
  "updatedAt": "2026-04-22T17:30:00Z"
}
```

Deleted once the weight is promoted to `weights.lock`.

### Status is a projection

`cog weights status` becomes a pure function of the four inputs:
(`cog.yaml`, `weights.lock`, pending state, registry check).

| Lock | Pending | Source vs lock fingerprint | Registry | Status |
|------|---------|----------------------------|----------|--------|
| missing | missing | — | — | `pending` |
| missing | present | — | — | `importing` |
| present | missing | same | all layers present | `ready` |
| present | missing | same | any layer missing | `incomplete` |
| present | missing | different | — | `source-drifted` |
| present | present | — | — | `reimporting` |
| in lock, not in cog.yaml | — | — | — | `orphaned` (existing) |

`pkg/model/weights_status.go` already handles the registry-check half.
Adding `importing` / `reimporting` / `source-drifted` is reading the
pending state file and comparing fingerprints.

## 3. Import pipeline

`cog weights import [name...]` — single-phase externally, two-phase
internally.

### Plan phase (no registry, no payload downloads)

1. For each weight, resolve `Source` from the URI scheme.
2. Call `Source.Inventory(ctx, uri, projectDir)` → file list with
   digests + source fingerprint.
3. Apply `include` / `exclude` filters.
4. Classify files by `bundle_file_max`; assign to layers via the
   same algorithm as today.
5. Compute `setDigest`.
6. Write pending state file with all layers in `planned` state.

At the end of plan: `weights.lock` is untouched, pending state is
complete, zero payload bytes have moved (except for file:// which
had to walk and hash to get inventory).

### Execute phase (per layer, bounded concurrency)

The executor streams source bytes into the `WeightStore` as part of
packing — so import populates the local store as a side effect. After
a successful import, the store is warm and `cog weights pull` is a
no-op.

For each planned layer, in a bounded worker pool:

1. Transition to `packing`.
2. For each member file:
   - Open `Source.Open(uri, path)`.
   - Stream bytes into `WeightStore.PutFile(ctx, expectedDigest, size, reader)`.
     `PutFile` writes via tmp + rename into `files/sha256/<digest>`,
     hashing as it goes and rejecting any mismatch against the
     expected digest (source-drift detection at ingress).
   - If `PutFile` observes the file already exists at the expected
     digest (same weight re-imported, or two weights sharing a file),
     the reader is discarded — no redundant download.
3. Transition to `pushing`. Construct a streaming `v1.Layer` via
   `tarball.LayerFromOpener(openerFn)`, where `openerFn` re-packs the
   tar on demand from the WeightStore's `files/` paths (deterministic;
   `registry.WriteLayer` calls the opener per attempt, retries repack).
   The tar never materializes on disk — bytes flow from files/ →
   tar writer → HTTP upload body.
4. Upload via the existing `registry.WriteLayer` path. The layer's
   `Digest()` call (triggered by go-containerregistry during upload
   finalization) hashes the streamed bytes and produces the blob
   digest.
5. Transition to `pushed`. Update pending state with `blobDigest`,
   `size`, `sizeUncompressed`.

No `.cog/weights-cache/` directory is used for tars during execute.
The only persistent local state is the WeightStore's `files/` (the
extracted bytes, reused by pull/predict) and the pending state file
(the plan). Tars are ephemeral, produced on demand during upload.

A `--purge-after` flag on import removes the just-imported files
from the WeightStore after a successful push, for CI scenarios where
the machine won't run `cog predict`. Off by default.

### Manifest phase

1. Once all layers are `pushed`, assemble the OCI manifest (existing code).
2. Push the manifest.
3. Promote pending state into `weights.lock`: upsert the
   `WeightLockEntry`, save atomically.
4. Delete the pending state file.

### Resumption

On a second `cog weights import`, if a pending state file exists:

- Re-read inventory. If fingerprint and planned contents match the
  pending state, skip planning and resume from where layers left off.
- If inventory drifted, discard the pending state and re-plan. Log
  loudly; the pending file is explicitly non-authoritative.

Per-layer resumption is cheap because files are already in the
WeightStore from the interrupted run. A layer stuck in `packing` or
`pushing` just re-runs from its current state: `PutFile` is
idempotent on already-present files, and the streaming tar re-packs
from `files/` deterministically. Layers that reached `pushed` are
skipped entirely.

`.cog/weights-cache/` does not exist in this design — there is no
separate tar cache to reconcile with the pending state. The
WeightStore's `files/` and the pending state are the only two
on-disk artifacts of an in-progress import.

## 4. `WeightStore`: local-side counterpart to the registry

The local store is the machine-local equivalent of the registry for
the producer, consumer, and everything in between. It owns layer
blobs (conceptually — see v1 implementation), assembles weight
directories on demand, and handles fetching whatever it's missing via
whatever means the caller can give it.

```go
type WeightStore interface {
    // HasSet reports whether a fully assembled weight directory exists
    // for this set digest.
    HasSet(ctx context.Context, setDigest string) (bool, error)

    // HasFiles reports whether every file in the list is present in
    // the store. Answered by stat'ing each file's content-addressed
    // path — no per-layer hash required.
    HasFiles(ctx context.Context, files []LayerFileRef) (bool, error)

    // Fetch makes the store self-populate with whatever it's missing
    // for (setDigest, layers), using the cheapest means available.
    // Caller provides the means; the store decides whether to use
    // them or delegate (a remote DockerWeightStore may ignore source
    // and ask its remote daemon to docker pull).
    Fetch(ctx context.Context, setDigest string, layers []LayerRef, means FetchMeans) error

    // PutFile stores a file's bytes in the store under its
    // content-addressed digest. The reader is consumed and verified
    // against expectedDigest as it streams; a mismatch is an error.
    // Idempotent: if the file already exists at the digest, the
    // reader is discarded and no error is returned.
    //
    // Used by the import executor to populate the store during
    // packing — single-pass streaming from source to local storage.
    PutFile(ctx context.Context, expectedDigest string, size int64, r io.Reader) error

    // Mount returns a path bindable into a container at the weight's
    // target. Read-only from the caller's perspective. Caller
    // releases via the handle when the container shuts down.
    Mount(ctx context.Context, setDigest string, layers []LayerRef) (MountHandle, error)
}

type LayerRef struct {
    BlobDigest string          // registry-facing (lockfile.Layers[].Digest)
    MediaType  string
    Size       int64
    Files      []LayerFileRef  // members from lockfile.Files filtered by this layer
}

type FetchMeans struct {
    Source   weightsource.Source // nil if source unavailable
    URI      string
    Registry registry.Client     // nil if offline
    Repo     string
}

type MountHandle interface {
    Path() string       // bind-mount source (read-only)
    Release() error     // decrement ref count / cleanup
}
```

### Why the store, not Docker directly

- **Adaptable.** Today's most-likely-shippable backend (files + hardlinks)
  and the eventual remote-Docker-daemon backend are both valid
  implementations of the same interface. `cog predict` doesn't care.
- **Provider-driven transfer.** The caller hands the store access to
  source + registry; the store decides what to do with them. A
  remote `DockerWeightStore` can ignore `Source` and tell its
  daemon to `docker pull` — no bytes through the CLI machine. A
  `FileWeightStore` prefers source over registry when both are
  available.
- **Decoupled from Docker.** The model-container run path still uses
  Docker, but weight storage doesn't have to. That keeps the door
  open for bundled mode, systemd units, or anything else that runs
  containers without Docker.

### v1 backend: `FileWeightStore`

Layout:

```
~/.cache/cog/weights/
  files/sha256/<ab>/<abcdef...>    # content-addressed file blobs
  assembled/<set-digest>/          # hardlinks into files/, bind-mount target
```

Two directories. `files/` is the durable store; `assembled/` is a
view of a specific weight set composed via hardlinks.

Key decisions:

- **Cache stores extracted files, not tars.** Cheaper to repack a tar
  on demand than to store both tars and extractions. The packer is
  deterministic; layer tar bytes are a pure function of the file set
  + the packing rules, so the tar is reconstructable from files.
- **Hardlinked assembled directories.** Two weight sets referencing
  the same file share the same inode. Zero duplication across
  set-digests that share files (cross-model dedup is automatic).
- **No per-layer index.** The store works at file granularity. Layer
  membership lives in the lockfile (`Files[i].Layer`); the store
  doesn't need its own copy.

Operations:

- `HasFiles(files)`: for each file, stat `files/sha256/<digest>`;
  returns true iff all present.
- `HasSet(setDigest)`: stat `assembled/<setDigest>/.cog/ready` (the
  readiness marker from spec §3.2 — we produce it ourselves so the
  runtime state protocol works identically in dev and prod).
- `Fetch`, per layer in the request:
  1. For each file in `layer.Files`: if `files/sha256/<digest>`
     already exists, skip. Otherwise, if `means.Source != nil`,
     `Source.Open(uri, file.Path)` → `PutFile(ctx, file.Digest,
     file.Size, reader)`; `PutFile` streams, hashes, and verifies
     against the expected digest.
  2. If any file in the layer remains missing and `means.Registry
     != nil`, fall back to a registry fetch for the *layer*: stream
     the blob, decompress, extract, `PutFile` each tar entry.
     (Registry transport is layer-granular; source transport is
     file-granular — so file-by-file fetch is only possible when a
     `Source` is available.)
  3. If nothing satisfies the remaining files, error with a clear
     message.
- `PutFile(expectedDigest, size, r)`: write tmp + rename into
  `files/sha256/<digest>`, hash while writing, verify. Idempotent.
- `Mount`: if `HasSet`, return existing path. Otherwise build
  `assembled/<setDigest>/` by hardlinking each file from `files/`
  (looked up by the lockfile's per-file digest) into the weight
  target tree, write `.cog/ready` atomically last, return the path.

**Hardlink caveats:** Requires `files/` and `assembled/` on the same
filesystem. They are by default (both under `~/.cache/cog/weights/`).
Documented constraint; fall back to copying if ever detected across
filesystems (symlink is not acceptable — bind mounts don't follow
symlinks reliably inside containers).

**Garbage collection:** Reference-count `assembled/` dirs via a
manifest sidecar. `cog weights purge` removes assembled dirs and GCs
files with no remaining hardlinks. LRU eviction over a size budget is
a future enhancement.

### v2 backend: `DockerWeightStore`

Same interface. Layer blobs live in the Docker daemon's content store
(loaded via `daemon.Write` from imports or `docker pull` for
registry-seeded layers). `Mount` does the overlay2 `MergedDir` dance
(or `--volumes-from` + `VOLUME` for remote-`DOCKER_HOST` compatibility).

Unlocks the target workflow: laptop with `DOCKER_HOST=tcp://gpu-server:2375`,
weights fetched on the remote host, nothing through the laptop.

Not in v1 scope, but the `WeightStore` interface is shaped so dropping
it in doesn't require callers to change.

## 5. `cog weights pull` as synthesis

`cog weights pull [name...]` populates the `WeightStore` with
whatever's needed to run the model locally. It is **not** "download
from registry" — it's "make these weights runnable here."

Implementation is a one-liner into the store:

```go
store.Fetch(ctx, setDigest, layers, FetchMeans{
    Source:   sourceIfAvailable,
    URI:      lockEntry.Source.URI,
    Registry: registryClient,
    Repo:     repo,
})
```

The store's priority order (§4) handles the three cases:

1. **Cache warm** (common after `cog weights import`): no-op.
2. **Cache cold, source accessible** (common on `git clone` + `cog predict`
   for file:// weights): reconstruct from source, never touch registry.
3. **Cache cold, source unavailable** (common for hf:// weights after
   `git clone`, or prod-like envs with no HF access): registry pull.

Source-drift detection is free: reconstructed file digests get verified
against the lockfile. If they don't match, the store falls through to
the registry.

v1 requires an explicit `cog weights pull` before `cog predict`.
Auto-pull on predict is deferred to v2.

## 6. Impact on the spec and open items

`specs/weights.md` (the OCI format spec) is unchanged. Per-file content
digests and per-layer contents digests are derivable from the config
blob (§2.3); cog stores them in its producer-side lockfile as a
convenience, not as a format change. The spec stays format-only and
generic.

The v2 design doc (`2026-04-16-managed-weights-v2-design.md`) §3 and
§5 are partially superseded:

- §3 ("CLI commands") still describes the right surface, but `cog weights pull`
  semantics shift from "download from registry" to "synthesize
  runnable weights" (§5 of this doc).
- §5 ("Local dev workflow") changes backend: the v1 implementation
  uses `FileWeightStore` (hardlinked extracted files), not the
  overlay2 `MergedDir` path. Overlay2 / `MergedDir` / `--volumes-from`
  all come back as `DockerWeightStore` in v2.

Deferred to later (not v1):

- `DockerWeightStore` backend + remote `DOCKER_HOST` workflow.
- Auto-pull on `cog predict`.
- Single-file streaming import (source file bigger than local disk).
  Chunked-per-layer covers the common case; single-huge-file sources
  are rare.
- Fat-provider optimizations (hf:// LFS S3 URL pass-through, etc.).
- Cross-repo blob mounts for `oci://` sources.
- LRU / size-budget GC for `FileWeightStore`.

## 7. Migration and rollout

- `weights.lock` schema unchanged (v1 stays).
- `.gitignore` additions: `.cog/weights-state/`.
- `Source` interface change is internal; no external API.
- `WeightStore` is new; no backwards compatibility concerns.
- `cog weights pull` semantics change: document in CLI help and
  release notes.
- `.cog/weights-cache/` (existing per-import tar cache) is removed
  as part of the plan/execute split. There is no separate tar cache
  in the new design: the WeightStore's `files/` is the durable local
  copy, tars are streamed on demand during upload. Delete any
  `.cog/weights-cache/` tree on first run of the new import pipeline.

## 8. Worked example: Kimi K2.5 on a 200 GB laptop

Model: ~600 GB across ~66 safetensors files, sourced from HF.

**Import** (`cog weights import kimi-k2.5`):

1. Plan phase: HF API returns 66 files with LFS sha256 digests.
   Planning produces 66 planned layers (one per file, each ~9 GB)
   plus one bundle for configs. Pending state file is ~few KB.
   Zero bytes downloaded.
2. Execute phase: worker pool of 4 grabs layers. Each layer:
   stream 9 GB from HF through `Source.Open` → `WeightStore.PutFile`
   writes the file once into `~/.cache/cog/weights/files/` (verified
   against inventory digest as it streams) → streaming tar packer
   feeds `registry.WriteLayer` directly from `files/`, no on-disk
   tar. Peak **extra** disk usage during execute: near zero
   (WeightStore files are the durable copy). At end of import,
   `~/.cache/cog/weights/` holds 600 GB (unavoidable — those are the
   weights) and the registry has all layers.
3. Manifest phase: push manifest, promote pending to `weights.lock`,
   delete pending state.

**Pull + predict** (`cog weights pull && cog predict`):

- If the import just ran, the WeightStore is already warm — import
  streamed every file into `~/.cache/cog/weights/files/` via
  `PutFile` as part of packing. `cog weights pull` is a no-op.
  `cog predict` calls `WeightStore.Mount` which hardlinks files
  from `files/` into `assembled/<setDigest>/`. No network.
- If cold (new clone): reconstruct from HF (skip registry unless HF
  is down). Disk usage after pull: 600 GB in `files/`, hardlinks
  in `assembled/`. Still unavoidable — the weights have to exist
  somewhere to run.
- `cog predict` bind-mounts `assembled/<setDigest>/` → model runs.

Disk usage at steady state: 600 GB (one copy, no duplication, no
matter how many models/set-digests reference the same files).

## 9. Sequencing hints

The `WeightStore` sits on the critical path of import (it's populated
during packing via `PutFile`), not just of pull/predict. The store
interface and `FileWeightStore` must land before the plan/execute
split.

Linear order (each session picks up the next ready bean):

1. **Source interface redesign** (cog-n2w1): `Inventory` + `Open` + `NativeBlobRef`. Port `FileSource`.
2. **Pending state file** (cog-4rmi): format, atomic IO, `.gitignore`.
3. **WeightStore interface** (cog-p76s): types, contract, `HasSet` / `HasFiles` / `Fetch` / `PutFile` / `Mount`.
4. **FileWeightStore** (cog-gbse): `files/` + `assembled/` layout, hardlink assembly, per-file cache.
5. **Plan/execute split** (cog-3p4a): executor calls `PutFile` during packing, warming the store. Streams tar direct to registry via `tarball.LayerFromOpener`. Removes `.cog/weights-cache/`.
6. **HuggingFace source** (cog-gr3t): can start after step 1; lands after step 5 to plug into the executor.
7. **Wire `cog weights pull`** (cog-xhpw): delegate to `WeightStore.Fetch`.
8. **Wire `cog predict`** (cog-40ed): delegate to `WeightStore.Mount`.

(The earlier lockfile v2 bean was scrapped — `ContentsDigest` was
not pulling its weight once the store became file-granular.)

Deferred to v2:
- `DockerWeightStore` (cog-pqtq)
- Auto-pull on `cog predict` (cog-kg8r)
- s3/http sources (cog-kkfz)
- Streaming single-file import (cog-ntiv)

Steps 1–8 can be landed as straight replacements — there are no users
of the v2-prototype format yet, so no migration or feature-flag work
is needed on top.
