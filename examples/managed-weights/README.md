# examples/managed-weights

Test fixture for the v1 managed-weights OCI pipeline. This isn't a model you'd
deploy -- it's an end-to-end exercise of weight import, packing, pushing, and
runtime validation.

If you're looking for a starting point for a real model, see
[`examples/resnet`](../resnet/) instead.

## What this does

The predictor doesn't do inference. Instead, it reads `weights.lock` at setup,
validates that every expected file exists on disk with the correct size and
digest, and returns a per-weight status summary from `predict()`. It's a
smoke test for the weight pipeline.

The `cog.yaml` declares two weight sources to exercise both code paths:

- **`parakeet`** -- local directory (`uri: weights`), filtered with `include`
  globs. You populate this manually by cloning from HuggingFace.
- **`minilm`** -- HuggingFace repo (`uri: hf://sentence-transformers/all-MiniLM-L6-v2`),
  filtered with `exclude` globs. Downloaded automatically by `cog weights import`.

## Setup

### Populate the local weights

The `weights/` directory is git-ignored (~5 GB). Clone the HuggingFace repo
and copy everything except `.git/`:

```bash
git clone https://huggingface.co/nvidia/parakeet-tdt-0.6b-v3 ~/hf/parakeet

mkdir -p examples/managed-weights/weights
rsync -a --exclude=.git/ ~/hf/parakeet/ examples/managed-weights/weights/
```

You can substitute any directory of model files -- the pipeline is
content-agnostic.

### Import weights and generate the lockfile

```bash
cd examples/managed-weights
cog weights import
```

This fetches the HuggingFace weights (minilm), ingresses everything (both
local and remote files) into the content-addressed store at
`~/.cache/cog/weights/`, and writes `weights.lock`.

## Running the pipeline

### Option A: Full build + push

Start a local registry (or point at any registry you control):

```bash
docker run -d --rm -p 5000:5000 --name cog-test-registry registry:3
```

Build and push. The `weights:` block in `cog.yaml` triggers the OCI bundle
format automatically:

```bash
cd examples/managed-weights
cog push
```

### Option B: Weight pipeline only (no model image)

```bash
cd examples/managed-weights
cog weights build
cog weights push
```

### Testing locally

Build the image and run with weights bind-mounted:

```bash
cd examples/managed-weights
cog build -t managed-weights-local
docker run --rm -p 5050:5000 \
  -v $(pwd)/weights:/src/weights/parakeet:ro \
  managed-weights-local
```

Then hit predict:

```bash
curl -s -X POST http://localhost:5050/predictions \
  -H 'Content-Type: application/json' \
  -d '{"input":{}}' | jq '.output | fromjson'
```

## Inspecting the output

```bash
crane manifest localhost:5000/managed-weights:latest | jq .
crane ls localhost:5000/managed-weights
```

Weight manifests are pushed under tags like
`weights-<name>-<12-hex-digest>` (see `pkg/model/weight_pusher.go`).
