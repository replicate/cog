# Cog Model Test Harness

Automated test harness for validating cog models against new SDK versions.
Designed to test any cog model from any repo.

## Quick Start

```bash
cd tools/test-harness

# Create a venv and install dependencies
python3 -m venv .venv
source .venv/bin/activate
pip install pyyaml

# List all models in the manifest
python -m harness list

# Run all non-GPU models
python -m harness run --no-gpu

# Run a specific model
python -m harness run --model hello-world

# Run GPU models only (requires NVIDIA GPU + nvidia-docker)
python -m harness run --gpu-only

# Output JSON report
python -m harness run --no-gpu --output json --output-file results/report.json

# Build images only (no predictions)
python -m harness build --no-gpu
```

## Prerequisites

- Python 3.10+
- Docker
- For GPU models: NVIDIA GPU + nvidia-docker runtime

### Version Resolution

By default the harness automatically resolves the **latest stable** versions
of both the cog CLI (from GitHub releases) and the Python SDK (from PyPI),
skipping any alpha/beta/rc tags. You can override either via the CLI or in
`manifest.yaml`:

```bash
# Use the latest stable CLI + SDK (default)
python -m harness run --no-gpu

# Pin a specific CLI version
python -m harness run --cog-version v0.16.12 --no-gpu

# Pin a specific SDK version
python -m harness run --sdk-version 0.16.12 --no-gpu

# Use a pre-release CLI
python -m harness run --cog-version v0.17.0-rc.2 --no-gpu

# Use a locally-built binary (overrides --cog-version)
python -m harness run --cog-binary ./dist/go/darwin-arm64/cog --no-gpu
```

You can also pin versions in `manifest.yaml` under `defaults`:

```yaml
defaults:
  sdk_version: "latest"    # or pin e.g. "0.16.12"
  cog_version: "latest"    # or pin e.g. "v0.16.12"
```

**Resolution priority** (for both CLI and SDK): CLI flag > manifest default > latest stable.

## Manifest Format

Models are defined in `manifest.yaml`. Each entry specifies a GitHub repo,
subdirectory, test inputs, and expected outputs:

```yaml
models:
  - name: hello-world
    repo: replicate/cog-examples
    path: hello-world
    gpu: false
    tests:
      - description: "basic predict"
        inputs:
          text: "world"
        expect:
          type: exact
          value: "hello world"
```

### Model Fields

| Field | Required | Description |
|-------|----------|-------------|
| `name` | yes | Unique identifier for the model |
| `repo` | yes | GitHub `owner/repo` to clone |
| `path` | no | Subdirectory within the repo (default: `.`) |
| `gpu` | no | Whether the model requires a GPU (default: `false`) |
| `sdk_version` | no | Override the SDK version (default: from `defaults.sdk_version`) |
| `timeout` | no | Per-prediction timeout in seconds (default: 300) |
| `requires_env` | no | List of env vars that must be set; model is skipped if missing |
| `env` | no | Extra env vars to pass; supports `${VAR}` expansion from host |
| `cog_yaml_overrides` | no | Dict deep-merged into the model's cog.yaml |
| `tests` | no | List of predict test cases |
| `train_tests` | no | List of train test cases |

### Input References

Prefix a value with `@` to reference a file in `fixtures/`:

```yaml
inputs:
  image: "@test_image.png"    # resolves to fixtures/test_image.png
```

### Validation Types

| Type | Fields | Description |
|------|--------|-------------|
| `exact` | `value` | Output must equal value exactly |
| `contains` | `value` | Output must contain the substring |
| `regex` | `pattern` | Output must match the regex |
| `file_exists` | `mime` (optional) | Output is a file path that must exist |
| `json_match` | `match` | Output parsed as JSON must contain the given subset |
| `json_keys` | `keys` (optional) | Output parsed as JSON dict must have entries |
| `not_empty` | — | Output must be non-empty |

## Adding a New Model

Add an entry to `manifest.yaml`:

```yaml
  - name: my-model
    repo: myorg/my-model-repo
    path: "."
    gpu: true
    # sdk_version: "0.16.12"  # optional per-model override
    env:
      HF_TOKEN: "${HF_TOKEN}"
    timeout: 600
    tests:
      - description: "smoke test"
        inputs:
          prompt: "hello"
        expect:
          type: contains
          value: "result"
```

No code changes required.

## CLI Reference

```
usage: cog-test {run,build,list} [options]

Commands:
  run     Build and test models (full pipeline)
  build   Build Docker images only (no predictions)
  list    List models defined in the manifest

Common options:
  --manifest PATH       Path to manifest.yaml
  --model NAME          Run only this model (repeatable)
  --no-gpu              Skip GPU models
  --gpu-only            Only run GPU models
  --sdk-version VER     SDK version (default: latest stable from PyPI)
  --cog-version TAG     CLI version to download (default: latest stable)
  --cog-binary PATH     Path to local cog binary (overrides --cog-version)
  --keep-images         Don't clean up Docker images after run

Run-specific options:
  --output {console,json}   Output format (default: console)
  --output-file PATH        Write report to file
```

## Architecture

```
tools/test-harness/
├── manifest.yaml           # Declarative test definitions
├── fixtures/               # Test input files (images, etc.)
├── harness/
│   ├── cli.py              # CLI entry point
│   ├── cog_resolver.py     # Resolves + downloads cog CLI and SDK versions
│   ├── runner.py           # Clone -> patch -> build -> predict -> validate
│   ├── patcher.py          # Patches cog.yaml with sdk_version + overrides
│   ├── validators.py       # Output validation strategies
│   └── report.py           # Console + JSON report generation
├── results/                # Output reports (gitignored)
└── pyproject.toml
```
