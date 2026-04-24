---
# cog-xhpw
title: WeightManager + cog weights pull command
status: completed
type: task
priority: critical
created_at: 2026-04-22T20:23:52Z
updated_at: 2026-04-24T00:30:43Z
parent: cog-kgd7
blocked_by:
    - cog-gbse
---

Introduce the `WeightManager` orchestrator and wire it to a new `cog weights pull` subcommand that populates the local `WeightStore` from the registry.

## Why

After `cog weights import`, a user on a fresh machine needs a way to populate the local store from the registry so `cog predict` can run. The importer doesn't warm the store yet (cog-3p4a, future); v1 requires an explicit `cog weights pull` step.

The Manager is where weight-level orchestration lives: knowing which files a weight needs, checking the store, fetching from the registry, assembling mounts. `cog predict` (cog-40ed) and `cog weights pull` (this bean) both go through it. The store stays narrow (cog-p76s); the Manager holds the refs to registry, lockfile, store, and project dir.

## WeightManager

New package: `pkg/weights/`

```go
type Manager struct {
    store      store.WeightStore
    registry   registry.Client
    repo       string              // from cog.yaml Image field
    lock       *model.WeightsLock
    projectDir string               // mounts live under <projectDir>/.cog/mounts
}

type ManagerOptions struct {
    Store      store.WeightStore
    Registry   registry.Client
    Repo       string
    Lock       *model.WeightsLock
    ProjectDir string
}

func NewManager(opts ManagerOptions) (*Manager, error)
```

Three public operations for v1:

- **`Pull(ctx, names []string) error`** — this bean.
- **`Prepare(ctx) (Mounts, error)`** — cog-40ed.
- **`Status(ctx) (*model.WeightsStatus, error)`** — thin wrapper over existing `model.ComputeWeightsStatus`; enhancement to consider local store as a "ready" source is a follow-up.

## Pull implementation

For each named weight (all weights if names is empty):

1. Find the lockfile entry by name.
2. Collect needed digests from `entry.Files`.
3. For each file, check `store.Exists(digest)`. Skip if present.
4. Group remaining-needed files by layer (via `entry.Files[i].Layer`).
5. For each layer with outstanding files:
    - Fetch layer blob from registry: `<repo>@<entry.Digest>` for the manifest, then the individual layer blob digest.
    - Stream blob → gunzip → tar reader.
    - For each tar entry: look up expected file in the lockfile by path. If it's one of our outstanding files, `store.PutFile(expectedDigest, entry.Size, tarEntryReader)`. Otherwise skip the entry body.
    - Error if a tar entry's path isn't in the lockfile (the registry content should be fully described by the lockfile — the lockfile is authoritative).
6. Error if, after processing the layer, any expected file is still missing.

Registry is **always** the source. We don't fall back to source URI reconstruction in v1 — the registry is authoritative once import has run, per the design.

## CLI: `cog weights pull`

`pkg/cli/weights.go` gets a new subcommand:

```
cog weights pull [NAME...]
```

- No NAMEs → pull all weights.
- NAMEs → validate each against cog.yaml, error on unknown names.
- No flags in v1.
- Exit codes:
    - 0 all pulled (including already-cached)
    - 1 any weight failed
    - 2 config error

CLI is thin: parse args, load config + lockfile, build store + registry client, construct Manager, call `mgr.Pull(ctx, names)`. No logic.

Idempotency: per-file `Exists` check via `store.Exists`. Running pull twice in a row is cheap.

## Output

v1 keeps output minimal:
```
Pulling parakeet... done (4.5 GB, 3 layers)
Pulling minilm... cached
```

Progress bars and per-layer reporting deferred.

## Unhiding

The `cog weights` command group is currently `Hidden: true` (cog-gxqs tracks unhiding). This bean does **not** unhide. Users who know about the command can use it; general rollout happens when cog-gxqs lands.

## Scope (code)

- `pkg/weights/manager.go`: `Manager`, `ManagerOptions`, `NewManager`.
- `pkg/weights/pull.go`: `Manager.Pull` implementation (layer fetch + tar extract + PutFile loop).
- `pkg/cli/weights.go`: new `pullCmd` subcommand, registered under the existing `weights` group.
- Tests:
    - `Pull` happy path with mock registry client (layer-by-layer, streams to store)
    - `Pull` with cached files (per-file Exists skips already-present digests)
    - `Pull` idempotent (second call is no-op)
    - `Pull` digest mismatch in tar entry (corrupt bytes → error, store unchanged)
    - `Pull` unexpected file path in tar (not in lockfile → error)
    - `Pull` with NAME filter (only named weights pulled, unknown name errors)

## Out of scope

- `Prepare` / mount assembly (cog-40ed)
- `Status` local-store awareness (follow-up)
- Source-reconstruction fallback (future; registry is authoritative for v1)
- Progress UI
- Auto-pull on `cog predict` (deferred to v2)
- Unhiding `cog weights` group (cog-gxqs)

## Dependencies

- cog-gbse (FileWeightStore)

## Todo

- [x] Create `pkg/weights/` package
- [x] `Manager`, `ManagerOptions`, `NewManager`
- [x] `Manager.Pull(ctx, names)`: per-weight → per-layer → tar-extract → PutFile
- [x] `Manager.Status` thin wrapper over existing `ComputeWeightsStatus`
- [x] `pkg/cli/weights.go` gains `pullCmd` subcommand
- [x] CLI constructs store + registry + Manager, delegates
- [x] Error messages for missing config (no image, no weights)
- [x] Tests for all paths

## Reference

- cog-p76s (interface)
- cog-gbse (FileWeightStore)
- Session design discussion: 2026-04-23 (chat log)

## Summary of Changes

- `pkg/weights/manager.go` — `Manager` type + `ManagerOptions` + `NewManager`. Carries the store, registry client, repo, lockfile, and project dir. `Status` is a thin wrapper over existing `model.ComputeWeightsStatus`. `selectEntries` resolves name filters against the lockfile and reports every unknown name at once.
- `pkg/weights/pull.go` — `Manager.Pull`. Per-weight: compute missing-file set (skips fully-cached weights with zero registry I/O), group remaining files by layer, fetch the weight manifest by digest via `registry.GetImage`, stream each needed layer's uncompressed tar, `PutFile` every regular-file entry. Unexpected tar paths error out (lockfile is authoritative). Post-pull existence check catches the case where a layer silently lacks an expected file. Returns `[]PullResult` so callers can render progress.
- `pkg/cli/weights_pull.go` — new `cog weights pull [NAME...]` subcommand wired in `weights.go`. Thin: loads config + lockfile, resolves repo (cog.yaml `image` or `--image`), constructs `FileStore`/`registry.Client`/`Manager`, delegates to `mgr.Pull`.
- `pkg/weights/pull_test.go` — happy path (multi-layer), all-cached (no registry I/O), idempotent re-pull, digest mismatch in tar (store unchanged), unexpected file in tar, NAME filter, unknown name, `NewManager` required-opts validation. Tests use an in-memory `rawTarLayer` + `empty.Image` + `mutate.Append` + a tiny `stubRegistry` implementing only `GetImage` — the other `registry.Client` methods return errors so misuse is loud.

No new methods added to `registry.Client`: Pull uses the existing `GetImage` → `v1.Image.LayerByDigest` → `layer.Uncompressed` path.

`mise run lint:go` → 0 issues; `go test ./...` → all green. `cog weights pull --help` renders.
