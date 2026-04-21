# examples/managed-weights

A minimal cog model used to exercise the v1 managed-weights OCI pipeline
end-to-end. It produces an OCI image index carrying a model image manifest
and per-weight manifests.

The predictor validates weight files on disk against a baked-in manifest
(`weights_manifest.json`), errors on any mismatch, and returns the
actual inventory as structured JSON from predict().

## Populating `weights/`

The weight directory is git-ignored because it's ~5 GB. Populate it by
cloning the HuggingFace repo and copying everything except `.git/`:

```bash
# One-time: clone the weights somewhere outside this repo
git clone https://huggingface.co/nvidia/parakeet-tdt-0.6b-v3 ~/hf/parakeet

# Copy everything except .git into examples/managed-weights/weights/
mkdir -p examples/managed-weights/weights
rsync -a --exclude=.git/ ~/hf/parakeet/ examples/managed-weights/weights/
```

You can substitute any directory of model files; the pipeline is
content-agnostic.

## Regenerating the manifest

After changing the contents of `weights/`, regenerate the manifest that
gets baked into the image:

```bash
cd examples/managed-weights
python generate_manifest.py
```

This writes `weights_manifest.json`. The predictor's `setup()` compares
files on disk against this manifest and raises RuntimeError on any
missing, extra, or mismatched files.

This is a temporary hack — it will be replaced by `/.cog/weights.json`
once that's embedded in the model image.

## Running the pipeline

Start a local registry (or point at any registry you control):

```bash
docker run -d --rm -p 5000:5000 --name cog-test-registry registry:3
```

Build and push the full bundle. Presence of `weights:` in `cog.yaml`
triggers the OCI bundle format automatically.

```bash
cd examples/managed-weights
cog push localhost:5000/managed-weights
```

Or run the weight pipeline in isolation (no model image):

```bash
cd examples/managed-weights
cog weights build
cog weights push localhost:5000/managed-weights
```

## Testing locally

Build the image and run it with weights bind-mounted:

```bash
cd examples/managed-weights
cog build -t managed-weights-local
docker run --rm -p 5050:5000 \
  -v $(pwd)/weights:/src/weights:ro \
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

Weight manifests are pushed under tags of the shape
`weights-<name>-<12-hex-digest>` (see `pkg/model/weight_pusher.go`).
