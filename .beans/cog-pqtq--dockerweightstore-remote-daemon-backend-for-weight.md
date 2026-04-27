---
# cog-pqtq
title: 'DockerWeightStore: remote-daemon backend for WeightStore'
status: todo
type: feature
priority: low
created_at: 2026-04-22T20:24:30Z
updated_at: 2026-04-27T19:02:21Z
parent: cog-9qcd
blocked_by:
    - cog-p76s
---

Second backend for `WeightStore`: use a Docker daemon's image store for layer storage and overlay2 `MergedDir` (or `--volumes-from` + `VOLUME`) for mounting. Unlocks the remote-`DOCKER_HOST` workflow.

## Why

The target workflow: laptop with `DOCKER_HOST=tcp://gpu-server:2375`, model training/inference on 8 H200s with 5 TB of local disk, nothing through the laptop. `FileWeightStore` on the laptop can't do this — the bytes would have to flow client → server.

`DockerWeightStore` delegates to the daemon: `Fetch` calls `docker pull` on the daemon, which pulls from the registry directly. The laptop only sends the manifest digest and polls for progress.

## Scope

### Backend operations

- `HasSet(setDigest)`: check for a named container / volume keyed on setDigest.
- `HasLayer(contentsDigest)`: not directly exposed by Docker — either track via image labels or skip (Docker handles layer dedup internally; `HasSet` is the only externally-visible check we need).
- `Fetch(means)`: delegate to `docker pull` on the weight image reference (`<registry>/<repo>/weights/<name>@sha256:<manifestDigest>`). Ignore `means.Source` — the daemon fetches from the registry directly.
- `Mount`: either
  - **overlay2 MergedDir**: `docker create --read-only` the weight image, `docker inspect` for `GraphDriver.Data.MergedDir`, return that path. Only works when Docker is local (not over `DOCKER_HOST`).
  - **`--volumes-from` + `VOLUME`**: weight image declares `VOLUME <target>`, model container uses `--volumes-from cog-wt-<name>:ro`. Works over `DOCKER_HOST`. Couples target path to weight image config.

Decision: support both; detect remote `DOCKER_HOST` and auto-select. `VOLUME` declarations are only needed when producing weight images — production of overlay2-only weight images stays the default for now.

### Integration

- `cog weights pull` with this backend: `docker pull` runs on the daemon; no CLI-side bytes.
- `cog predict` with this backend: same flow as `FileWeightStore`, different mount mechanism.

## Out of scope (v1)

- Everything. This is a deferred-to-v2 bean. Explicitly called out in the design doc as the drop-in for when the remote-daemon workflow becomes the priority.

## Dependencies

Blocked by:
- WeightStore interface
- Overlay2 spike (confirm MergedDir works on Docker Desktop)

## Notes

- The original cog-by3m had the overlay2 spike called out. That spike still has value, just for this bean instead of v1.
- `--volumes-from` requires weight images to declare `VOLUME` — a push-side change to weight image production. Track as a sub-concern if picked up.

## Reference

- `plans/2026-04-22-managed-weights-import-and-local-run-design.md` §4 (WeightStore interface)
- `plans/2026-04-16-managed-weights-v2-design.md` §5.5, §5.6, §5.7



## Update 2026-04-27: Not a Store-level swap — parallel Manager-mode

Design discussion (cog-i12u landing) clarified that DockerWeightStore cannot honestly be a drop-in `pkg/weights/store.Store` implementation. The current `Store` interface is fundamentally file-keyed:

- `Exists(fileDigest)` — Docker indexes layers, not individual file digests.
- `PutFile(fileDigest, r)` — Docker has no path to inject a single named file into the daemon's content store.
- `Path(fileDigest)` — Docker doesn't expose stable per-file paths; only layer-merged-directory paths via overlay2 `MergedDir`.

A `DockerWeightStore` that pretended to satisfy this interface would either fall over on `Path` or have to translate every call by walking the lockfile to map file digests back to layer digests. That's not really a store implementation, it's an impedance-mismatched adapter.

### Real shape of this work

DockerWeightStore is a **parallel Manager-mode**, not a Store swap. It lives at the `weights.Manager` level:

- A new Manager backend (or new Manager mode) that uses `docker pull <weight-image>` to populate the daemon's layer store.
- `Manager.Pull` becomes "tell daemon to pull"; the laptop only sends the manifest digest and polls progress.
- `Manager.Prepare` returns `Mounts{Specs []MountSpec}` — but the specs point at `--volumes-from <weight-container>:ro` references or overlay2 MergedDir paths instead of project-local hardlink trees.
- Predict / train / weights pull CLI commands don't change; they consume `Manager` and `MountSpec`.

### One small consumer-side change

`MountSpec` today is `{Source string, Target string}` and assumed to be a host bind path. Under DockerStore, the `Source` may be a `--volumes-from` reference. Two options when this lands:

1. Add a `MountSpec.Kind` discriminator (`bind` vs `volumes-from`) and update `pkg/predict/predictor.go:118-124` to handle both. Small surgical change.
2. Have DockerStore extract the image to a host path (overlay2 MergedDir) and return that path, preserving the bind-mount shape. Limits to local Docker only (no `DOCKER_HOST` workflow), but `MountSpec` stays unchanged.

Either way, the change to consumers outside `pkg/weights/` is bounded — predict/train CLI imports stay; only the volume-mount loop in the predictor needs to learn the new discriminator if we go route 1.

### What survives across the v2 swap

- `weights.Manager` interface (Pull, Prepare, MountSpec shape).
- The lockfile schema (still drives "what to fetch / what to mount").
- All callers in `pkg/cli/predict.go`, `pkg/cli/train.go`, `pkg/cli/weights_pull.go`, `pkg/predict/predictor.go`.

### What gets deleted/replaced

- `pkg/weights/store/` (FileStore implementation) — possibly deleted entirely, possibly retained as one of two backends.
- `pkg/weights/mount.go` hardlink-assembly path — replaced with the Docker-mount strategy.
- `pkg/weights/pull.go` — replaced with `docker pull` orchestration.
- The cache-warming flow added by cog-i12u — gone, since BuildKit handles build-side caching its own way and Docker handles cache-side.

### Sequencing

This is part of the v2 release, alongside BuildKit-based weight building. Both substitutions (build side + cache side) are independently scoped; they can land in either order or together. The important thing is: neither requires touching code outside `pkg/weights/` (and `pkg/cli/weights.go`'s import wiring), assuming we accept the small `MountSpec.Kind` discriminator if it becomes necessary.

### Updated bean status

Still parented under cog-9qcd as v2 work. Still blocked by cog-p76s (the Store interface). The blocking relationship is now nominal — what cog-pqtq actually needs is a stable Manager interface, which exists. Could be unblocked when v2 planning starts.
