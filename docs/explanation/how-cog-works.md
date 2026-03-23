# How Cog works

Cog turns your machine learning model into a production-ready Docker container. Understanding what happens behind the scenes -- from `cog.yaml` to a running prediction server -- helps you make better decisions about how to structure your project, troubleshoot build issues, and optimise performance.

## The build pipeline

When you run `cog build`, a multi-stage pipeline transforms your configuration and code into a Docker image. The pipeline has three main phases.

### Phase 1: Configuration parsing

Cog reads your `cog.yaml` and resolves the full environment specification. The reason this is a separate phase -- rather than just templating a Dockerfile directly -- is that Cog needs to make several interdependent decisions:

- Which Python version to use (specified, or inferred from framework compatibility)
- Whether a GPU base image is needed (based on `build.gpu`)
- Which CUDA and cuDNN versions to install (auto-detected from your PyTorch or TensorFlow version)
- Which base image to start from (an nvidia CUDA image, a Cog pre-built base image, or a plain Python image)

These decisions depend on one another. For example, PyTorch 2.x requires CUDA 11.8 or 12.1, which in turn constrains the nvidia base image tag. Cog's compatibility matrix handles this resolution automatically, which is one of the main reasons cog.yaml exists rather than asking you to write Dockerfiles directly.

### Phase 2: Dockerfile generation

Cog generates a Dockerfile from the resolved configuration. This is not a simple template -- the generator produces an optimised, multi-stage Dockerfile that follows Docker best practices:

- System packages are installed early (they change infrequently)
- Python dependencies come next (they change occasionally)
- Your model code is copied last (it changes often)

This ordering is deliberate. Docker caches layers, and by placing infrequently-changing steps first, Cog ensures that rebuilds after code changes are fast -- typically seconds rather than minutes. See [Docker image layer strategy](image-layers.md) for a deeper discussion of why layer ordering matters.

The generated Dockerfile also installs two Cog components into the image:

- **The Cog Python SDK** (`cog` package) -- provides `BasePredictor`, `Input`, `Path`, and other types your model code imports
- **Coglet** -- the prediction server that handles HTTP requests, manages your model process, and orchestrates predictions

### Phase 3: Docker image build

Cog invokes Docker Buildx to build the image. The build context is your project directory (filtered by `.dockerignore`), and the result is a standard Docker image you can run anywhere.

If you have specified an `image` name in `cog.yaml`, the image is tagged accordingly. Otherwise, Cog generates a name from your directory.

The entire pipeline is deterministic for a given set of inputs: the same `cog.yaml`, requirements, and code will produce the same Dockerfile and, in most cases, the same image.

## What is inside a Cog container

A built Cog image contains everything needed to serve predictions as an HTTP API. When a container starts, it runs the coglet prediction server, which manages your model's lifecycle.

### The two-process architecture

Cog containers use a parent-child process architecture:

- **Parent process (coglet)**: A Rust-based HTTP server built on the Axum web framework. It handles incoming HTTP requests, manages health checks, orchestrates predictions, and delivers webhooks. The reason this is written in Rust rather than Python is performance and reliability -- the HTTP server needs to remain responsive even when the Python process is fully occupied with a GPU-intensive prediction.

- **Worker subprocess**: A Python process that loads and runs your model. This is where your `setup()` and `predict()` methods execute. The worker communicates with the parent through an inter-process communication (IPC) protocol.

The reason for this separation is isolation. If your model code crashes, exhausts memory, or enters a bad state, the parent process survives and can report the failure, restart the worker, or return appropriate error responses. A single-process design would mean that a segfault in a CUDA operation could silently kill the entire server with no error reported.

### How a container starts up

When a container starts, the following sequence occurs:

1. The coglet parent process starts and immediately begins serving HTTP on port 5000
2. The health check endpoint returns `STARTING` -- the model is not yet ready
3. Coglet spawns the Python worker subprocess
4. The worker loads your predictor class and calls `setup()` -- this is where you load model weights, initialise pipelines, and perform other one-time work
5. Once `setup()` completes, the worker signals readiness to the parent
6. The health check now returns `READY`, and the server begins accepting predictions

The fact that the HTTP server starts before `setup()` completes is a deliberate design choice. It allows orchestration systems like Kubernetes to monitor the container's health from the moment it starts. They can distinguish between "still loading" (`STARTING`) and "something went wrong" (`SETUP_FAILED`), which is essential for reliable deployments.

## How `cog predict` works

`cog predict` is a convenience command that combines several steps:

1. **Build** the Docker image (if needed) -- this is identical to `cog build`
2. **Run** the container with your project directory mounted as a volume
3. **Wait** for the server to become `READY`
4. **Send** a prediction request with your `-i` inputs
5. **Stream** the output back to your terminal
6. **Stop** the container

The volume mount means your local code changes are reflected inside the container without rebuilding. However, changes to `cog.yaml` or your Python dependencies will trigger a rebuild.

When you pass `-i image=@photo.jpg`, the `@` prefix tells Cog to read the file from disk and upload it to the running container. Inputs without `@` are passed as literal values.

## How `cog serve` works

`cog serve` builds (if needed) and starts the container, then keeps it running so you can send requests manually with `curl` or other HTTP clients. Unlike `cog predict`, it does not send a prediction -- it just exposes the server.

By default, the server listens on port 5000 inside the container. You can map it to a different host port with `-p`:

```
cog serve -p 8080
```

This is useful during development when you want to test your model's HTTP API interactively, inspect the OpenAPI schema at `/openapi.json`, or integrate with other local services.

## How predictions are routed

When a prediction request arrives at the HTTP server, coglet acquires a "slot" -- a communication channel to the worker process. By default, there is one slot, meaning predictions run sequentially. If you configure [`concurrency.max`](../yaml.md#max) in `cog.yaml`, multiple slots are created, each backed by its own Unix domain socket, allowing concurrent predictions.

The slot-based design avoids head-of-line blocking: each concurrent prediction has its own communication channel, so a slow prediction does not delay the delivery of another prediction's streaming output or logs.

For more detail on what happens during a prediction, see [The prediction lifecycle](prediction-lifecycle.md).

## The role of the OpenAPI schema

Cog inspects your `predict()` method's type annotations and `Input()` descriptors at startup to generate an [OpenAPI](https://swagger.io/specification/) schema. This schema serves multiple purposes:

- **Input validation**: The server validates incoming JSON against the schema before calling your code, catching type errors and constraint violations early
- **Documentation**: The schema at `/openapi.json` describes your model's API in a machine-readable format
- **Client generation**: Tools can use the schema to generate typed client libraries for your model

The reason Cog derives the schema from Python types rather than requiring a separate schema file is to keep the source of truth in one place. Your `predict()` signature *is* the API definition -- there is no separate specification to keep in sync.

## Further reading

- [The prediction lifecycle](prediction-lifecycle.md) -- what happens from request to response
- [CUDA and GPU compatibility](cuda-compat.md) -- how Cog manages the GPU stack
- [Docker image layer strategy](image-layers.md) -- how layers are structured for efficient builds and pushes
- [`cog.yaml` reference](../yaml.md) -- full configuration reference
- [Prediction interface reference](../python.md) -- the Python API for defining models
