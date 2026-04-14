# Cog Model Test Harness

Automated test harness for validating Cog models against new CLI/SDK versions.

This is the Go implementation and replaces the previous Python harness.

## Quick Start

```bash
cd tools/test-harness

# List all models in the manifest
go run . list

# Run all non-GPU models
go run . run --no-gpu

# Run a specific model
go run . run --model hello-world

# Run GPU models only (requires NVIDIA GPU + nvidia-docker)
go run . run --gpu-only

# Output JSON report
go run . run --no-gpu --output json --output-file results/report.json

# Build images only (no predictions)
go run . build --no-gpu

# Compare static vs runtime schema generation
go run . schema-compare --no-gpu

# Use a locally-built cog binary
go run . run --no-gpu --cog-binary ../../dist/go/darwin-arm64/cog

# Test from a git branch (builds CLI + SDK wheel automatically)
go run . run --no-gpu --cog-ref main
```

You can also run from repo root:

```bash
(cd tools/test-harness && go run . list)
(cd tools/test-harness && go run . run --no-gpu)
```

## Prerequisites

- Go (see repo toolchain)
- Docker
- For `--cog-ref`: Go + uv on PATH (`mise install` sets this up)
- For GPU models: NVIDIA GPU + nvidia-docker runtime

## Manifest and Fixtures

- Manifest: `tools/test-harness/manifest.yaml`
- Fixtures: `tools/test-harness/fixtures/`

Input values prefixed with `@` resolve to fixture paths. Example:

```yaml
inputs:
  image: "@test_image.png"
```

## CLI Reference

```text
usage: test-harness {run,build,list,schema-compare} [options]

Commands:
  run              Build and test models (full pipeline)
  build            Build Docker images only (no predictions)
  list             List models defined in the manifest
  schema-compare   Compare static vs runtime schema generation

Common options:
  --manifest PATH       Path to manifest.yaml
  --model NAME          Run only this model (repeatable)
  --no-gpu              Skip GPU models
  --gpu-only            Only run GPU models
  --sdk-version VER     SDK version override
  --cog-version TAG     CLI version to download
  --cog-binary PATH     Path to local cog binary
  --cog-ref REF         Git ref to build cog from source
  --sdk-wheel PATH      Local SDK wheel path
  --clean-images        Remove Docker images after run (default: keep them)
  --keep-outputs        Preserve prediction outputs and print the work directory path

Run/schema-compare options:
  --output {console,json}   Output format
  --output-file PATH        Write JSON report to file
```

## Project Layout

```text
tools/test-harness/
├── go.mod
├── main.go
├── README.md
├── manifest.yaml
├── fixtures/
├── cmd/
└── internal/
```
