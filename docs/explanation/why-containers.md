# Why containers for machine learning?

Shipping a machine learning model to production is a fundamentally different problem from training one. Training is exploratory -- you iterate on data, architectures, and hyperparameters in a notebook or script. Deployment demands reproducibility, portability, and a well-defined interface. The gap between these two worlds is where most ML engineering pain lives, and it is the problem Cog was built to solve.

## The deployment problem

Consider what it takes to run someone else's ML model. You need:

- The correct Python version (and often a specific patch release)
- The right versions of PyTorch, TensorFlow, or other frameworks
- Compatible CUDA and cuDNN libraries, if the model uses a GPU
- System-level dependencies like `ffmpeg`, `libgl`, or custom C libraries
- The model weights, which may be gigabytes in size
- Pre-processing and post-processing code that transforms raw inputs into model-ready tensors and back
- Some way to actually invoke the model -- a script, a Flask server, a gRPC endpoint

Every one of these is a potential source of failure. Python version mismatches cause subtle bugs. CUDA version conflicts produce cryptic errors or silent incorrect results. Missing system libraries crash the process at import time. And even when everything is installed correctly, there is no standard way to call the model -- every project has its own bespoke invocation pattern.

The result is that researchers routinely spend days helping engineers deploy their models, and engineers spend days debugging environment issues that worked fine on the researcher's machine. This is not a niche problem. Companies like Uber, Spotify, and others have built internal platforms specifically to address it.

## Why Docker is the right foundation

Docker containers solve the environment problem by packaging an entire filesystem -- operating system, libraries, Python, CUDA drivers, and application code -- into a single, portable artifact. A container built on one machine runs identically on another, regardless of the host's configuration.

For ML specifically, Docker provides several properties that matter:

**Reproducibility.** A Docker image is a snapshot of a working environment. If a model works in the image today, it will work in the same image a year from now, even if upstream packages have changed or been removed. This is stronger than pinning versions in a `requirements.txt`, because the image captures the entire operating system state, not just Python packages.

**Portability.** Docker images run on any machine with a Docker runtime -- your laptop, a cloud VM, a Kubernetes cluster, or a specialised ML inference platform. The same image you test locally is the same image that runs in production. There is no "it works on my machine" problem.

**Isolation.** Each container has its own filesystem and process space. You can run multiple models with conflicting dependencies on the same host without interference. A model that requires CUDA 11.8 and another that requires CUDA 12.1 can coexist without any conflict.

**Ecosystem.** Docker has mature tooling for building, pushing, pulling, and orchestrating images. Container registries, CI/CD pipelines, and orchestration platforms like Kubernetes all speak Docker natively. Choosing Docker means you inherit this entire ecosystem rather than building custom deployment tooling.

## What Cog adds on top of Docker

Docker solves the environment problem, but introduces its own complexity. Writing a Dockerfile for an ML model is not straightforward:

- You need to know which nvidia base image tag corresponds to your CUDA version
- You need to layer your dependencies in the right order for efficient caching
- You need to write an HTTP server to handle prediction requests
- You need to define an API schema for your model's inputs and outputs
- You need to handle file uploads and downloads, streaming output, health checks, and graceful shutdown

Cog eliminates this complexity. Instead of writing a Dockerfile, you write a `cog.yaml` that declares your dependencies, and a `predict.py` that defines your model's interface using standard Python types. Cog handles everything else.

### No Dockerfile authoring

Cog generates an optimised Dockerfile from your `cog.yaml`. The generated Dockerfile follows best practices that most ML engineers would not know to implement: efficient layer ordering for cache utilisation, multi-stage builds for smaller images, and correct nvidia base image selection.

### Automatic CUDA management

Specifying `gpu: true` in `cog.yaml` is enough. Cog maintains a [compatibility matrix](cuda-compat.md) of Python, PyTorch, TensorFlow, CUDA, and cuDNN versions, and automatically selects the right combination. This eliminates an entire category of environment bugs that are notoriously difficult to debug.

### A typed prediction API

By defining your `predict()` method with Python type annotations and `Input()` descriptors, Cog generates an OpenAPI schema and a validated HTTP API automatically. Your model's interface is defined in one place -- your Python code -- and the HTTP server, input validation, and API documentation all derive from it.

### A production-grade HTTP server

Every Cog container includes coglet, a Rust-based prediction server. It handles synchronous and asynchronous predictions, webhook delivery, streaming output, cancellation, health checks, concurrency control, and file uploads. Building this from scratch for every model would be a significant engineering effort.

### Consistent invocation

Every Cog model is invoked the same way: `cog predict -i input=value` for local testing, or `POST /predictions` for HTTP. There is no need to figure out how each model's ad-hoc prediction script works.

## Alternative approaches and their tradeoffs

Cog is not the only way to deploy ML models. Understanding the alternatives helps clarify when Cog is and is not the right choice.

### Manual Dockerfiles

Writing your own Dockerfile gives you maximum control. You can choose any base image, install any software, and structure layers however you like. Some teams prefer this when they have unusual requirements or deep Docker expertise.

The tradeoff is effort and maintenance. You must manage CUDA compatibility yourself, write your own HTTP server, implement health checks, and ensure your layer ordering is efficient. For a single model, this is manageable. For a team shipping many models, the duplicated effort and inconsistency across Dockerfiles becomes a real cost.

### Virtual environments and conda

Tools like `venv`, `conda`, and `poetry` solve Python dependency isolation, but they do not solve the full deployment problem. They do not capture system packages, CUDA libraries, or operating system state. A `conda` environment that works on your Ubuntu 22.04 laptop may fail on a cloud VM running Amazon Linux, because a system library is missing.

These tools are excellent for development and training, but they are a partial solution for deployment. Some teams combine them with Docker -- using conda inside a container -- but this adds complexity without clear benefit over Cog's approach.

### Cloud-specific solutions

Most cloud providers offer ML-specific deployment services: AWS SageMaker, Google Vertex AI, Azure ML, and so on. These services handle infrastructure, scaling, and often provide their own model packaging formats.

The tradeoff is portability. A model packaged for SageMaker cannot run on Vertex AI, and vice versa. If you later switch cloud providers, or want to run inference on your own hardware, you must repackage the model. Cog produces standard Docker images that run anywhere, avoiding lock-in to any single platform.

That said, cloud-specific services often provide features that Cog does not, such as auto-scaling, A/B testing, and integrated monitoring. Some teams use Cog to package their models and then deploy the resulting Docker images to a cloud service, getting the benefits of both approaches.

### Model serving frameworks

Frameworks like TensorFlow Serving, TorchServe, and Triton Inference Server are designed specifically for serving ML models at scale. They offer advanced features like dynamic batching, model versioning, and GPU memory management.

These frameworks are best suited for teams with dedicated ML infrastructure engineers and models that fit their supported formats. They require more configuration and infrastructure knowledge than Cog, and they typically expect models in specific serialised formats (SavedModel, TorchScript, ONNX) rather than arbitrary Python code.

Cog occupies a different niche: it is designed for individual researchers or small teams who want to deploy a model with minimal infrastructure work. If you are running a single model and your primary goal is getting it into production quickly, Cog is simpler. If you are managing hundreds of models at scale with complex routing and batching requirements, a dedicated serving framework may be more appropriate.

## The broader perspective

The reason Cog exists is a belief that the gap between ML research and production deployment is unnecessarily wide. Researchers should not need to become Docker experts or backend engineers to ship their work. Engineers should not need to reverse-engineer a researcher's ad-hoc environment setup.

Cog's creators -- Andreas Jansson (previously at Spotify, where he built internal ML deployment tools) and Ben Firshman (creator of Docker Compose) -- observed that many companies were independently building similar tools internally. Cog is the open-source version of that pattern: a standard way to package ML models in Docker containers with a typed prediction API.

The design reflects pragmatic choices. Cog uses Docker because Docker is ubiquitous, not because it is theoretically optimal. It uses Python type annotations for the API because Python is the language ML researchers already use, not because type annotations are the best schema language. It generates Dockerfiles rather than using a custom image format because Dockerfiles are understood by existing tooling.

These choices optimise for adoption and compatibility over theoretical purity, which is appropriate for a tool whose value comes from standardisation.

## Further reading

- [How Cog works](how-cog-works.md) -- the architecture of Cog containers
- [CUDA and GPU compatibility](cuda-compat.md) -- how Cog manages the GPU dependency chain
- [Getting started](../getting-started.md) -- try Cog with an example model
- [Getting started with your own model](../getting-started-own-model.md) -- package your own model
