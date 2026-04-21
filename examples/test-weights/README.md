# examples/test-weights

A minimal cog model used to exercise the v1 managed-weights OCI pipeline
end-to-end. It produces an OCI image index carrying a model image manifest
and per-weight manifests.

The predictor itself is a stub; the interesting part is `weights/`.

## Populating `weights/`

The weight directory is git-ignored because it's ~5 GB. Populate it by
cloning the HuggingFace repo and copying everything except `.git/`:

```bash
# One-time: clone the weights somewhere outside this repo
git clone https://huggingface.co/nvidia/parakeet-tdt-0.6b-v3 ~/hf/parakeet

# Copy everything except .git into examples/test-weights/weights/
mkdir -p examples/test-weights/weights
rsync -a --exclude=.git/ ~/hf/parakeet/ examples/test-weights/weights/
```

You can substitute any directory of model files; the pipeline is
content-agnostic.

## Running the pipeline

Start a local registry (or point at any registry you control):

```bash
docker run -d --rm -p 5000:5000 --name cog-test-registry registry:3
```

Build and push the full bundle. Presence of `weights:` in `cog.yaml`
triggers the OCI bundle format automatically.

```bash
cd examples/test-weights
cog push localhost:5000/test-weights
```

Or run the weight pipeline in isolation (no model image):

```bash
cd examples/test-weights
cog weights build
cog weights push localhost:5000/test-weights
```

## Inspecting the output

```bash
crane manifest localhost:5000/test-weights:latest | jq .
crane ls localhost:5000/test-weights
```

Weight manifests are pushed under tags of the shape
`weights-<name>-<12-hex-digest>` (see `pkg/model/weight_pusher.go`).
