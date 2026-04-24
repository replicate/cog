---
# cog-40ed
title: Wire cog predict to WeightManager.Prepare for managed weights
status: completed
type: task
priority: critical
created_at: 2026-04-22T20:24:11Z
updated_at: 2026-04-24T15:28:23Z
parent: cog-kgd7
blocked_by:
    - cog-xhpw
---

Add `Manager.Prepare` (assembles per-invocation mount dirs from the local store) and wire it into the `Predictor` so `cog predict` / `cog run` mount managed weights into the container at runtime.

## Why

Closes the local-run loop: after `cog weights pull`, a user can `cog predict` and the model container sees the weight files at the configured target path, with zero byte-duplication on disk (hardlinks from store to per-invocation mount dir).

## Design

### Per-invocation mount dir

```
<projectDir>/.cog/mounts/<invocation-id>/<weight-name>/<file tree from lockfile>
```

- `<invocation-id>` — 8-char hex generated per Predictor run.
- For each weight: `os.Link(store.Path(digest), <mount-dir>/<entry.Files[i].Path>)`.
- Container bind-mount source is `<mount-dir-root>/<weight-name>/`; target is `entry.Target`. Read-only.
- On cleanup: `rm -rf <mount-dir>/<invocation-id>/`. No refcount — each invocation gets its own dir.

Hardlinks mean no byte copies and no refcount needed on the store side: files in `store.files/sha256/<digest>` remain until explicit GC.

### Same-filesystem check

If `$XDG_CACHE_HOME` is on a different filesystem from `<projectDir>`, hardlinks fail with `EXDEV`. On first `Link`, detect this and return a clear error pointing the user at `COG_CACHE_DIR` to relocate the store. No silent fallback to copy (would defeat the no-duplication property) or symlink (unreliable across bind-mount boundaries).

### Manager.Prepare

```go
type Mounts struct {
    Specs   []MountSpec
    cleanup func() error
}

type MountSpec struct {
    Source string  // host path — <projectDir>/.cog/mounts/<id>/<weight-name>/
    Target string  // container path — entry.Target from lockfile
}

func (m Mounts) Release() error { return m.cleanup() }

func (m *Manager) Prepare(ctx context.Context) (Mounts, error)
```

Read-only is implicit — managed weights are never writable. The Predictor applies `:ro` when translating `MountSpec` to `command.Volume`.

Flow:

1. Generate invocation ID (8-char hex).
2. For each weight in `m.lock.Weights`:
    - For every file in `entry.Files`, check `store.Exists(digest)`.
    - If any missing → return error: `"weights not fully cached locally; run 'cog weights pull' first"`. No auto-pull in v1.
3. For each weight (after full precondition):
    - Create `<projectDir>/.cog/mounts/<id>/<name>/` with parent dirs for each file path.
    - `os.Link(store.Path(file.Digest), <mount>/<file.Path>)` per file. First link attempt detects same-filesystem constraint.
    - Append `MountSpec{Source: <mount-root>, Target: entry.Target}`.
4. Return `Mounts{Specs, cleanup}` where `cleanup` is `os.RemoveAll(<projectDir>/.cog/mounts/<id>)`.

### Predictor integration

`pkg/predict/predictor.go`:

```go
// Existing signature replaced:
func NewPredictor(ctx context.Context, opts PredictorOptions) (*Predictor, error)

type PredictorOptions struct {
    RunOptions    command.RunOptions   // user-provided: image, user volumes, env, gpus, ports
    WeightManager *weights.Manager     // optional; nil = no managed weights
    IsTrain       bool
    Docker        command.Command
}
```

- `WeightManager: nil` preserves current behavior for callers that don't know about weights.
- User volumes and system mounts are merged into a single `runOptions.Volumes` list at `Start()` time. No separate tracking. Docker engine errors on container path collision — no upfront validation in v1.

`Predictor.Start(ctx, logsWriter, timeout)`:

1. If `p.wm != nil`: call `p.wm.Prepare(ctx)`. Store the `Mounts` handle on the Predictor for later cleanup.
2. For each `MountSpec`: append `command.Volume{Source, Destination, ReadOnly: true}` to `p.runOptions.Volumes`. (If `command.Volume` lacks a `ReadOnly` field, add it — small prerequisite change in `pkg/docker/command/`.)
3. Existing container start flow.

`Predictor.Stop(ctx)`:

1. Existing container stop.
2. If weight mounts were prepared: call `Release()`. Log warning on cleanup failure, don't fail the stop. Bind mounts don't prevent source-side `rm -rf` on Linux; cleanup after container stop is safe.

### CLI integration

`pkg/cli/predict.go` (and `train.go`) constructs the Manager only when the model has weights:

```go
var wm *weights.Manager
if len(cfg.Weights) > 0 {
    lock, err := model.LoadWeightsLock(filepath.Join(projectDir, model.WeightsLockFilename))
    if err != nil {
        return fmt.Errorf("load weights.lock: %w", err)
    }
    fileStore, err := store.NewFileStore(paths.WeightsStoreDir())
    if err != nil { return err }
    wm, err = weights.NewManager(weights.ManagerOptions{
        Store: fileStore, Registry: regClient,
        Repo: cfg.Image, Lock: lock, ProjectDir: projectDir,
    })
    if err != nil { return err }
}

predictor, err := predict.NewPredictor(ctx, predict.PredictorOptions{
    RunOptions:    userRunOpts,  // user volumes, gpus, env — NOT weight mounts
    WeightManager: wm,
    IsTrain:       false,
    Docker:        dockerClient,
})
```

CLI has zero knowledge of mount assembly. It constructs the Manager and hands it off.

## Scope (code)

- `pkg/weights/mount.go`: `Mounts` type, `MountSpec` type, `Manager.Prepare` implementation
- `pkg/predict/predictor.go`: `PredictorOptions`, modified `NewPredictor`, updated `Start` / `Stop`
- `pkg/cli/predict.go`, `pkg/cli/train.go`: construct Manager conditionally, pass via options
- `pkg/docker/command/`: add `ReadOnly` to `Volume` if missing
- Tests:
    - `Prepare` happy path with populated store, verifies hardlinks created
    - `Prepare` with cold store errors pointing at `cog weights pull`
    - `Prepare` same-filesystem check (inject EXDEV)
    - `Mounts.Release` removes invocation dir
    - `NewPredictor` with `WeightManager: nil` preserves current behavior
    - `Predictor.Start` calls `Prepare`, merges into Volumes
    - `Predictor.Stop` calls `Release`

## Out of scope

- Auto-pull on missing weights (deferred)
- Model image `/.cog/weights.json` signal (cog-1pm2)
- Coglet readiness protocol (cog-7sne / cog-iy3e)
- Collision validation between user volumes and weight targets
- Unhiding `cog weights` group (cog-gxqs)

## Dependencies

- cog-xhpw (Manager + Pull)

## Todo

- [x] `Mounts`, `MountSpec` types in `pkg/weights/`
- [x] `Manager.Prepare` implementation with same-filesystem check
- [x] `PredictorOptions` type; update `NewPredictor`
- [x] Wire weight mount merge into `Predictor.Start`
- [x] Wire `Release` into `Predictor.Stop`
- [x] `command.Volume.ReadOnly` (add if missing)
- [x] Update `pkg/cli/predict.go` and `train.go`
- [x] Tests covering Prepare, Release, Predictor integration

## Reference

- cog-p76s (interface)
- cog-gbse (FileWeightStore)
- cog-xhpw (Manager + Pull)
- Session design discussion: 2026-04-23 (chat log)

## Summary of Changes

- `pkg/docker/command/command.go` + `pkg/docker/docker.go` — added `Volume.ReadOnly`, emitted as `:ro` on the Docker bind spec.
- `pkg/weights/mount.go` — `MountSpec` (source+target; read-only is implicit), `Mounts` (owns per-invocation scratch dir, `Release` idempotent + nil-safe), `Manager.Prepare` (precondition check for every file, MkdirAll + `os.Link` per file, EXDEV detection with clear COG_CACHE_DIR-pointing error, automatic cleanup on partial-failure).
- `pkg/predict/predictor.go` — `NewPredictor(ctx, PredictorOptions)` replaces the old three-positional-arg form. `PredictorOptions` carries `RunOptions`, `IsTrain`, `Docker`, and optional `WeightManager`. `Start` calls `Prepare` (when `WeightManager != nil`) and merges the resulting mounts into `runOptions.Volumes` with `ReadOnly: true` — user volumes and weight mounts live in one list, no separate tracking. `Stop` calls `Release` best-effort, logging on failure.
- `pkg/cli/weights_manager.go` — new `buildWeightManagerIfNeeded` helper shared by predict and train. Returns nil when cog.yaml has no weights; returns a configured Manager otherwise. Errors when weights are declared but no image is set.
- `pkg/cli/predict.go`, `pkg/cli/train.go` — both CLI commands now construct a Manager (build-from-source path only) and pass it into `PredictorOptions.WeightManager`. The predict-with-prebuilt-image path does not wire managed weights; there's no cog.yaml in scope and no in-image weight metadata yet (future: cog-1pm2). The GPU-retry fallback in predict.go also threads `wm` through.
- `pkg/weights/mount_test.go` — happy path (specs, paths, byte content), hardlinks share inodes with the store, missing-file error mentions `cog weights pull`, partial-failure cleanup removes the invocation dir, Release is idempotent and nil-safe, no-project-dir rejection, EXDEV detection including wrap through `os.LinkError`.

`mise run lint:go` → 0 issues. `go test ./...` → all green. `cog weights pull --help` + `cog predict --help` render correctly.

Notes worth flagging at review time:

1. **Predict-with-prebuilt-image does not mount managed weights.** The bean doesn't require it; the design is that the image itself signals weight targets (cog-1pm2 is the follow-up). I left a comment in the code to make that explicit.
2. **No explicit check for weight-target collisions with user volumes.** The bean spec says Docker's engine can reject this at container start. I did not add upfront validation.
3. **`.cog/mounts` lives under the project dir** which is also the `/src` bind source. This is fine (bind mount sources don't conflict with other bind mount sources), and Release cleans them up on Stop. Users running `ls .cog/mounts/` during a prediction will see one invocation-id subdirectory; that's expected and documented on the `Mounts` type.
