---
# cog-4rmi
title: Pending state file for in-flight weight imports
status: todo
type: task
priority: high
created_at: 2026-04-22T20:21:56Z
updated_at: 2026-04-22T20:38:38Z
parent: cog-66gt
---

Add a per-weight pending/in-flight state file that captures the plan, per-layer progress, and source fingerprint for an import in progress. This enables resumption after crash/SIGINT, progress UX, and status visibility for partially-imported weights.

## Why

`weights.lock` is canonical registry state — written only when an import fully succeeds. Everything before that (the plan, which layers are packing, which have been pushed) is in-flight state that must survive interruption without polluting the committed lockfile.

## Scope

### File format

Per-weight file at `.cog/weights-state/<name>.json`:

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
      "contentsDigest": "sha256:aaa...",
      "state": "pushed",
      "blobDigest": "sha256:bbb...",
      "size": 15000000,
      "sizeUncompressed": 18500000,
      "error": null
    }
  ],
  "updatedAt": "2026-04-22T17:30:00Z"
}
```

Layer states: `planned` → `packing` → `pushing` → `pushed` (terminal success) or `failed` (terminal failure, with `error` populated).

### Code

- New package or file (likely `pkg/model/weights_state.go`) with `WeightState` struct, load/save helpers, atomic write (tmp + rename), per-layer state transition helpers.
- Add `.cog/weights-state/` to any generated `.gitignore` templates.
- Crash-safety tests: partial writes don't corrupt the file (atomic replace), concurrent reader during writer sees a consistent snapshot.
- Load-or-empty helper mirroring `loadLockfileOrEmpty`.

## Out of scope

- Wiring into the import pipeline (separate bean — plan/execute split).
- Status command projection of pending state (separate bean or update to existing status bean).
- UI / progress reporting (covered by cog-66fc).

## Todo

- [ ] Define `WeightState`, `PlannedLayer`, `PlannedFile` types
- [ ] Implement atomic read/write (tmp + rename)
- [ ] Implement per-layer state transition helpers
- [ ] Add `.cog/weights-state/` to `.gitignore` templates (if cog generates them) — otherwise document
- [ ] Unit tests: round-trip, atomic replace, concurrent reader tolerance
- [ ] Unit test: partial/corrupt file recovery (reject and re-plan, don't crash)

## Reference

- `plans/2026-04-22-managed-weights-import-and-local-run-design.md` §2, §3



## Update 2026-04-22 (ContentsDigest dropped)

Following cog-vs3r being scrapped:

- `plannedLayers[].contentsDigest` field is removed from the pending state format.
- Layer identity within pending state is just the index + the member files (from plannedFiles[].layer).

Revised `plannedLayers` entry:

```jsonc
{
  "index": 0,
  "mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
  "state": "pushed",               // planned | packing | pushing | pushed | failed
  "blobDigest": "sha256:bbb...",   // populated after packing, matches future lockfile
  "size": 15000000,
  "sizeUncompressed": 18500000,
  "error": null
}
```

No contentsDigest. The plan is uniquely identified by the combination of plannedSetDigest + plannedFiles[] + plannedLayers[].index; that's sufficient for resumption comparisons.
