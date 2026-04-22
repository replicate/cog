---
# cog-pqtq
title: 'DockerWeightStore: remote-daemon backend for WeightStore'
status: todo
type: feature
priority: low
created_at: 2026-04-22T20:24:30Z
updated_at: 2026-04-22T20:25:30Z
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
