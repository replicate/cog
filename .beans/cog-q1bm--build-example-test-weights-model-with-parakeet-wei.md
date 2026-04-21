---
# cog-q1bm
title: Build example test-weights model with parakeet weights
status: completed
type: task
priority: high
created_at: 2026-04-17T22:32:43Z
updated_at: 2026-04-17T23:03:28Z
parent: cog-0wma
---

Create examples/test-weights/ and use it to produce a real v1 managed-weights OCI index for infra handoff.

Weights: /Users/mdwan/src/huggingface/nvidia/parakeet-tdt-0.6b-v3/ (~5GB, excludes .git/)
Registry: local docker registry:3 on :5000
Parent: cog-0wma (checkpoint: v2 weight artifact in registry)

## Tasks
- [x] Build local cog CLI (user ran `mise build:all` → v0.18.1-dev+g1e5bd766)
- [x] Scaffold examples/test-weights/ (cog.yaml, predict.py, requirements.txt, README.md)
- [x] Update root .gitignore: replace /test-weights/ with examples/test-weights/weights/ and .cog/
- [x] Copy weight files from ~/src/huggingface/nvidia/parakeet-tdt-0.6b-v3/ via rsync --exclude=.git/ (4.7GB)
- [x] Start local docker registry on :5000 (registry:3 container 'cog-test-registry')
- [x] Install crane for manifest verification (brew, v0.21.5)
- [x] Run cog weights build + push; verify outputs
- [x] Run cog build + COG_OCI_INDEX=1 cog push
- [x] Verify index structure with crane manifest
- [x] Document ref for infra handoff

## Summary of Changes

Produced a real v1 managed-weights OCI image index in a local registry, suitable for infra handoff.

**Scaffolded example model:**
- `examples/test-weights/cog.yaml` — weights: [{ name: parakeet, source: weights, target: /src/weights }]
- `examples/test-weights/predict.py` — minimal stub predictor (stats a file under /src/weights)
- `examples/test-weights/requirements.txt` — empty
- `examples/test-weights/README.md` — documents how to populate weights/ and run the pipeline
- Root `.gitignore` updated: removed stale `/test-weights/`, added `examples/test-weights/weights/` and `examples/test-weights/.cog/`

**Artifact in registry** (`localhost:5000/test-weights`):

Tags:
- `latest` → OCI index (digest sha256:a21ef64c99df0a45ae02d63d790d54222a3bb95373a475e91bd6094cbbbcc40d)
- `weights-parakeet-fc9ec231be26` → bundled weight manifest (has reference.digest → model image)
- `weights-parakeet-0abab1ba683e` → standalone weight manifest from the `cog weights push` dry run (no reference.digest)

Index structure (spec §2.4 ✓):
```
application/vnd.oci.image.index.v1+json
├── application/vnd.docker.distribution.manifest.v2+json  (model image, linux/amd64)
│   sha256:fc9ec231be26ad9ce6384135e346f19dbc3cbfc74072bcbbcc105562b35be085
│   17 layers (base + SDK wheel + coglet wheel + predict.py)
└── application/vnd.oci.image.manifest.v1+json  (weight, unknown/unknown)
    sha256:0f2045685b00b8bd40fa02c7ac2d02f6d35b3f3b7e3690431779e677f78256da
    annotations:
      run.cog.reference.type: weights
      run.cog.reference.digest: sha256:fc9e...be085  (cross-ref to image)
      run.cog.weight.name: parakeet
      run.cog.weight.target: /src/weights
    layers:
      - bundle (1.3MB → 383KB gzip): config.json, tokenizer.json, README, etc.
        annotations: run.cog.weight.content=bundle, run.cog.weight.size.uncompressed
      - file (2.5GB raw tar): model.safetensors (uncompressed — .safetensors is in incompressibleExts)
        annotations: run.cog.weight.content=file, run.cog.weight.file=model.safetensors, run.cog.weight.size.uncompressed
      - file (2.5GB → 2.3GB gzip): parakeet-tdt-0.6b-v3.nemo
        annotations: run.cog.weight.content=file, run.cog.weight.file=parakeet-tdt-0.6b-v3.nemo, run.cog.weight.size.uncompressed
```

**Timings:**
- `cog weights build`: 47s (walk + tar 4.5GB)
- `cog weights push` (standalone): 4.5s (to localhost)
- `COG_OCI_INDEX=1 cog push` (full): 4m48s (docker build + weight push + image push + index assembly)

**Inspection:**
```bash
crane ls localhost:5000/test-weights
crane manifest localhost:5000/test-weights:latest | jq .
crane manifest localhost:5000/test-weights@sha256:0f2045685b00b8bd40fa02c7ac2d02f6d35b3f3b7e3690431779e677f78256da | jq .
```

**Note on registry lifetime:** the local `cog-test-registry` container was started with `--rm` (no persistent volume), so the artifact is lost on docker restart. Re-running `cog push` reproduces it exactly — everything is deterministic given the same source dir.
