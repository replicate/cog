# Cog Architecture

This document provides a deep technical overview of Cog's architecture, covering both the CLI tooling and the two runtime implementations (Cog and Coglet).

## Table of Contents

1. [Overview](#overview)
2. [High-Level Architecture](#high-level-architecture)
3. [Go CLI (`cmd/cog/`, `pkg/`)](#go-cli)
4. [Docker Image Building](#docker-image-building)
5. [Python SDK (`python/cog/`)](#python-sdk)
6. [Cog Runtime (Python Server)](#cog-runtime-python-server)
7. [Coglet Runtime (Go Server + Python Runner)](#coglet-runtime)
8. [Prediction Flow](#prediction-flow)
9. [Key Design Decisions](#key-design-decisions)

---

## Overview

Cog is a tool for packaging machine learning models into production-ready Docker containers. It consists of:

1. **Go CLI** - Command-line tool for building, running, and pushing model containers
2. **Python SDK** - Library for defining model predictors with type-safe inputs/outputs
3. **Two Runtimes**:
   - **Cog** - Original Python-based HTTP server (FastAPI/uvicorn)
   - **Coglet** - Next-generation Go HTTP server with Python subprocess runner

---

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              User's Model                                    │
│                                                                              │
│   predict.py                         cog.yaml                                │
│   ┌──────────────────────┐          ┌─────────────────────────────┐         │
│   │ from cog import ...  │          │ build:                      │         │
│   │                      │          │   python_version: "3.11"    │         │
│   │ class Predictor:     │          │   python_requirements: ...  │         │
│   │   def setup(self):   │          │   gpu: true                 │         │
│   │     ...              │          │ predict: "predict.py:..."   │         │
│   │   def predict(self): │          └─────────────────────────────┘         │
│   │     ...              │                                                   │
│   └──────────────────────┘                                                   │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      │ cog build / cog push
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Go CLI                                          │
│                                                                              │
│   cmd/cog/         pkg/cli/           pkg/dockerfile/      pkg/image/       │
│   ┌─────────┐     ┌──────────────┐   ┌──────────────────┐ ┌─────────────┐  │
│   │ main()  │────▶│ build.go     │──▶│ StandardGenerator│─▶│ Build()    │  │
│   └─────────┘     │ predict.go   │   │ FastGenerator    │  │ Push()     │  │
│                   │ push.go      │   └──────────────────┘ └─────────────┘  │
│                   │ run.go       │                              │           │
│                   └──────────────┘                              ▼           │
│                                                          Docker BuildKit    │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      │ Docker Image
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Docker Container                                     │
│                                                                              │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                    Runtime (Cog OR Coglet)                           │   │
│   │                                                                      │   │
│   │   ┌─────────────────────┐    OR    ┌────────────────────────────┐   │   │
│   │   │   Cog Runtime       │          │    Coglet Runtime          │   │   │
│   │   │ (Python FastAPI)    │          │ (Go HTTP + Python runner)  │   │   │
│   │   └─────────────────────┘          └────────────────────────────┘   │   │
│   │              │                                  │                    │   │
│   │              ▼                                  ▼                    │   │
│   │   ┌─────────────────────────────────────────────────────────────┐   │   │
│   │   │                   User's Predictor                           │   │   │
│   │   │                   (predict.py)                               │   │   │
│   │   └─────────────────────────────────────────────────────────────┘   │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
│   HTTP API: POST /predictions, GET /health-check, etc.                      │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Go CLI

### Entry Point

```
cmd/cog/cog.go
    └── cli.NewRootCommand()
        └── Execute()
```

The CLI uses [Cobra](https://github.com/spf13/cobra) for command structure.

### Command Structure (`pkg/cli/`)

| Command | File | Description |
|---------|------|-------------|
| `build` | `build.go` | Build Docker image from `cog.yaml` |
| `predict` | `predict.go` | Run a single prediction |
| `push` | `push.go` | Build and push image to registry |
| `run` | `run.go` | Run arbitrary command in container |
| `serve` | `serve.go` | Start HTTP prediction server locally |
| `train` | `train.go` | Run model training (hidden) |
| `init` | `init.go` | Initialize new Cog project |
| `login` | `login.go` | Authenticate with registry |

### Key Packages

| Package | Responsibility |
|---------|---------------|
| `pkg/config/` | Parse `cog.yaml`, validate configuration |
| `pkg/docker/` | Docker client, container operations, push logic |
| `pkg/dockerfile/` | Dockerfile generation (Standard vs Fast) |
| `pkg/image/` | High-level build orchestration, schema validation |
| `pkg/predict/` | HTTP client for prediction server |
| `pkg/wheels/` | Embedded Python wheels (cog, coglet) |

### Command Flow: `cog build`

```
cog build -t my-model:latest
    │
    ▼
pkg/cli/build.go:buildCommand()
    │
    ├── docker.NewClient()              # Create Docker SDK client
    ├── config.GetConfig("cog.yaml")    # Parse configuration
    │
    └── image.Build()
        │
        ├── dockerfile.NewGenerator()   # Select Standard or Fast
        │   │
        │   ├── StandardGenerator       # Traditional Dockerfile
        │   └── FastGenerator           # Monobase-based fast builds
        │
        ├── generator.GenerateDockerfileWithoutSeparateWeights()
        │
        ├── dockerClient.ImageBuild()   # BuildKit build
        │
        ├── GenerateOpenAPISchema()     # Run container to introspect
        │
        └── BuildAddLabelsAndSchemaToImage()  # Add metadata
```

---

## Docker Image Building

### Two Build Modes

#### Standard Build (`pkg/dockerfile/standard_generator.go`)

Traditional Dockerfile generation:

```dockerfile
# syntax=docker/dockerfile:1.4
FROM python:3.11-slim  # or nvidia/cuda:... or r8.im/cog-base:...

# System packages
RUN apt-get update && apt-get install -y ...

# Python dependencies
COPY requirements.txt /tmp/
RUN pip install -r /tmp/requirements.txt

# Cog wheel
COPY cog-*.whl /tmp/
RUN pip install /tmp/cog-*.whl

# User code
COPY . /src
WORKDIR /src

EXPOSE 5000
CMD ["python", "-m", "cog.server.http"]
```

**Characteristics:**
- Supports custom `run` commands in cog.yaml
- Supports separate weights layers
- Full flexibility, slower builds

#### Fast Build (`pkg/dockerfile/fast_generator.go`)

Uses monobase images with pre-built Python/CUDA environments:

```dockerfile
# syntax=docker/dockerfile:1-labs
FROM r8.im/monobase:latest

ENV R8_PYTHON_VERSION=3.11
ENV R8_TORCH_VERSION=2.1.0
ENV R8_CUDA_VERSION=12.1
ENV R8_COG_VERSION=coglet

# Activate pre-built venv
RUN --mount=type=cache,... monobase.build --mini

# Install user dependencies via UV
RUN --mount=type=cache,... monobase.user --requirements=requirements.txt

COPY . /src
WORKDIR /src

ENTRYPOINT ["/usr/bin/tini", "--", "/opt/r8/monobase/exec.sh"]
CMD ["python", "-m", "cog.server.http"]
```

**Characteristics:**
- Pre-built Python/Torch/CUDA combinations
- UV package manager for fast installs
- Does NOT support `run` commands
- Significantly faster builds

### Base Image Selection

1. **Cog Base Images** (`r8.im/cog-base:cuda11.8-python3.10-torch2.0.1`)
   - Pre-built images with common CUDA/Python/Torch combinations
   - Fastest option when available

2. **NVIDIA CUDA Images** (`nvidia/cuda:11.8.0-cudnn8-devel-ubuntu22.04`)
   - Fallback when cog-base not available
   - Requires full Python installation

3. **Python Slim** (`python:3.11-slim`)
   - CPU-only models

4. **Monobase** (`r8.im/monobase:latest`)
   - Universal base for fast builds
   - Contains all Python/CUDA combinations

### Embedded Wheels (`pkg/wheels/`)

Cog and Coglet wheels are embedded in the Go binary:

```go
//go:embed cog-*.whl coglet-*.whl
var wheelFS embed.FS
```

Selection controlled by:
1. `COG_WHEEL` environment variable
2. `cog_runtime: true` in cog.yaml (uses coglet-alpha)
3. Default: embedded cog wheel

---

## Python SDK

### Public API (`python/cog/__init__.py`)

```python
from cog import (
    BasePredictor,  # Base class for models
    Input,          # Define input parameters
    Path,           # File path type (auto-downloads URLs)
    Secret,         # Sensitive string type
    ConcatenateIterator,  # Streaming text output
    BaseModel,      # Pydantic model for complex outputs
    current_scope,  # Access prediction context
)
```

### Predictor Interface (`python/cog/base_predictor.py`)

```python
class BasePredictor:
    def setup(self, weights: Optional[Path] = None) -> None:
        """Called once at startup to load model"""
        pass
    
    def predict(self, **kwargs) -> Any:
        """Called for each prediction request"""
        raise NotImplementedError
```

### Input Types (`python/cog/types.py`)

| Type | Description |
|------|-------------|
| `str`, `int`, `float`, `bool` | Basic types |
| `Path` | File input (auto-downloads URLs) |
| `Secret` | Sensitive string (masked in logs) |
| `List[T]` | Array of allowed types |
| `Literal["a", "b"]` | Fixed choices |

### Input Validation

```python
def predict(
    self,
    prompt: str = Input(description="Text prompt"),
    temperature: float = Input(default=0.7, ge=0, le=2),
    image: Path = Input(description="Input image"),
) -> str:
    ...
```

Constraints: `ge`, `le`, `min_length`, `max_length`, `regex`, `choices`

---

## Cog Runtime (Python Server)

The original runtime is a pure Python HTTP server.

### Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    Cog Python Server                             │
│                                                                  │
│   python -m cog.server.http                                     │
│                                                                  │
│   ┌─────────────────┐                                           │
│   │    uvicorn      │  ASGI Server                              │
│   │    (async)      │                                           │
│   └────────┬────────┘                                           │
│            │                                                     │
│   ┌────────▼────────┐                                           │
│   │    FastAPI      │  HTTP Framework                           │
│   │   Application   │                                           │
│   └────────┬────────┘                                           │
│            │                                                     │
│   ┌────────▼────────────────────────────────────────────────┐   │
│   │              PredictionRunner                            │   │
│   │  - Manages prediction lifecycle                          │   │
│   │  - Handles webhooks                                      │   │
│   │  - Uploads output files                                  │   │
│   └────────┬────────────────────────────────────────────────┘   │
│            │                                                     │
│   ┌────────▼────────────────────────────────────────────────┐   │
│   │                Worker (subprocess)                       │   │
│   │  - Loads predictor in isolated process                   │   │
│   │  - Runs setup() once                                     │   │
│   │  - Runs predict() for each request                       │   │
│   │  - Captures stdout/stderr                                │   │
│   └─────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

### HTTP Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health-check` | Health status |
| `POST` | `/predictions` | Run prediction |
| `PUT` | `/predictions/{id}` | Idempotent prediction |
| `POST` | `/predictions/{id}/cancel` | Cancel prediction |
| `POST` | `/shutdown` | Graceful shutdown |

### Worker Communication

Parent and child processes communicate via `multiprocessing.Pipe`:

```
Parent (HTTP handlers)          Child (Predictor)
        │                              │
        │──── PredictionInput ────────▶│
        │                              │
        │◀─── PredictionOutputType ────│
        │◀─── PredictionOutput ────────│  (multiple for generators)
        │◀─── Log ─────────────────────│  (stdout/stderr capture)
        │◀─── Done ────────────────────│
        │                              │
        │──── Cancel ─────────────────▶│  (if requested)
        │                              │
```

### Key Files

| File | Purpose |
|------|---------|
| `python/cog/server/http.py` | FastAPI app, endpoints |
| `python/cog/server/runner.py` | PredictionRunner |
| `python/cog/server/worker.py` | Worker subprocess management |
| `python/cog/server/eventtypes.py` | IPC event definitions |

---

## Coglet Runtime

Coglet is a next-generation runtime with a Go HTTP server and Python subprocess.

### Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Coglet Runtime                                     │
│                                                                              │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                    Go HTTP Server (coglet-server)                    │   │
│   │                                                                      │   │
│   │   ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐    │   │
│   │   │   Service   │  │   Handler   │  │    Runner Manager       │    │   │
│   │   │ (lifecycle) │──│  (HTTP API) │──│  (process management)   │    │   │
│   │   └─────────────┘  └─────────────┘  └───────────┬─────────────┘    │   │
│   └─────────────────────────────────────────────────┼───────────────────┘   │
│                                                     │                        │
│                          IPC (files + HTTP)         │  spawn                 │
│                                                     ▼                        │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                    Python Runner (coglet)                            │   │
│   │                                                                      │   │
│   │   ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐    │   │
│   │   │ FileRunner  │  │   Runner    │  │      Inspector          │    │   │
│   │   │ (IPC loop)  │──│ (predictor) │──│ (schema generation)     │    │   │
│   │   └─────────────┘  └─────────────┘  └─────────────────────────┘    │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────────┘
```

### IPC Mechanism

Communication uses files in a working directory:

```
working_dir/
├── config.json           # Go → Python: predictor configuration
├── openapi.json          # Python → Go: generated schema
├── setup_result.json     # Python → Go: setup status
├── ready                 # Python → Go: runner ready signal
├── request-{pid}.json    # Go → Python: prediction request
├── response-{pid}.json   # Python → Go: prediction response
├── cancel-{pid}          # Go → Python: cancellation signal
└── stop                  # Go → Python: shutdown signal
```

Plus HTTP notifications to `/_ipc` endpoint for immediate response handling.

### Cog Compatibility Shim

Coglet includes a compatibility layer (`coglet/python/cog/`) that provides the same API:

```python
# coglet/python/cog/__init__.py
from coglet.api import (
    BasePredictor,
    Input,
    Path,
    Secret,
    # ... same exports as cog
)
```

This allows models written for Cog to run unchanged on Coglet.

### Key Differences from Cog

| Aspect | Cog | Coglet |
|--------|-----|--------|
| HTTP Server | Python (FastAPI) | Go (net/http) |
| Process Model | Single Python | Go + Python subprocess(es) |
| IPC | In-process pipes | File-based + HTTP |
| Multi-tenancy | No | Procedure mode |
| Training | Supported | **Not supported** |

### Key Files

| File | Purpose |
|------|---------|
| `coglet/cmd/coglet-server/main.go` | Entry point, CLI |
| `coglet/internal/service/service.go` | Lifecycle management |
| `coglet/internal/server/server.go` | HTTP handlers |
| `coglet/internal/runner/manager.go` | Runner pool |
| `coglet/internal/runner/runner.go` | Subprocess management |
| `coglet/python/coglet/__main__.py` | Python entry point |
| `coglet/python/coglet/file_runner.py` | Prediction loop |
| `coglet/python/cog/` | Compatibility shim |

---

## Prediction Flow

### `cog predict -i prompt="Hello"`

```
┌────────────────────────────────────────────────────────────────────────────┐
│ 1. CLI Initialization                                                       │
│                                                                             │
│    pkg/cli/predict.go                                                       │
│    ├── Parse --input flags                                                  │
│    ├── docker.NewClient()                                                   │
│    └── config.GetConfig("cog.yaml")                                         │
└────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌────────────────────────────────────────────────────────────────────────────┐
│ 2. Build Image (if needed)                                                  │
│                                                                             │
│    If no image argument:                                                    │
│    └── image.BuildBase() or image.Build()                                   │
│        └── Mount project directory as volume                                │
│                                                                             │
│    If image argument:                                                       │
│    └── dockerClient.Pull(imageName)                                         │
└────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌────────────────────────────────────────────────────────────────────────────┐
│ 3. Start Container                                                          │
│                                                                             │
│    predict.NewPredictor()                                                   │
│    └── predictor.Start()                                                    │
│        ├── docker.RunDaemon()     # Start container in background           │
│        └── waitForReady()         # Poll GET /health-check until ready      │
└────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌────────────────────────────────────────────────────────────────────────────┐
│ 4. Send Prediction Request                                                  │
│                                                                             │
│    predictor.Predict(inputs)                                                │
│    └── POST /predictions                                                    │
│        {                                                                    │
│          "input": {"prompt": "Hello"}                                       │
│        }                                                                    │
└────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌────────────────────────────────────────────────────────────────────────────┐
│ 5. Inside Container (Cog Runtime)                                           │
│                                                                             │
│    HTTP Handler receives request                                            │
│    └── PredictionRunner.predict()                                           │
│        ├── Validate inputs against OpenAPI schema                           │
│        ├── Send PredictionInput to Worker                                   │
│        │                                                                    │
│        │   Worker (subprocess):                                             │
│        │   └── predictor.predict(**inputs)                                  │
│        │       └── Returns output or yields for generators                  │
│        │                                                                    │
│        ├── Receive PredictionOutput events                                  │
│        ├── Receive Done event                                               │
│        └── Return JSON response                                             │
└────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌────────────────────────────────────────────────────────────────────────────┐
│ 6. Output Results                                                           │
│                                                                             │
│    CLI receives response                                                    │
│    ├── Write output to file (if -o specified)                               │
│    ├── Print to stdout                                                      │
│    └── predictor.Stop()  # Cleanup container                                │
└────────────────────────────────────────────────────────────────────────────┘
```

---

## Key Design Decisions

### 1. Process Isolation

The predictor runs in a subprocess (Python) or separate process (Coglet) to:
- Isolate crashes from the HTTP server
- Enable clean cancellation via signals
- Support GPU memory cleanup between predictions

### 2. Embedded Wheels

Cog and Coglet wheels are embedded in the Go binary to:
- Ensure version compatibility between CLI and runtime
- Simplify installation (single binary)
- Enable offline builds

### 3. Two Build Modes

- **Standard**: Maximum flexibility for complex builds
- **Fast**: Optimized for rapid iteration with pre-built environments

### 4. Type-Safe Inputs/Outputs

Pydantic models provide:
- Automatic validation
- OpenAPI schema generation
- Self-documenting APIs

### 5. Coglet Architecture

Go HTTP server + Python runner enables:
- Better performance (Go HTTP handling)
- Multi-tenancy (procedure mode)
- Cleaner process lifecycle management
- Future: distributed prediction routing

### 6. Compatibility Layer

Coglet's cog shim ensures:
- Existing models work without modification
- Gradual migration path
- Same user-facing API
