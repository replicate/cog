# CUDA and GPU compatibility

Running machine learning models on GPUs requires a stack of interdependent software components, each with strict version requirements. Managing this stack manually is one of the most common sources of frustration in ML deployment. Cog automates it, but understanding the underlying problem -- and how Cog's solution works -- helps you diagnose issues and make informed choices when the defaults are not sufficient.

## The compatibility challenge

GPU-accelerated machine learning involves a chain of dependencies, each constrained by the others:

```
Your model code
    -> PyTorch / TensorFlow (specific version)
        -> CUDA toolkit (specific version range)
            -> cuDNN (specific version range)
                -> NVIDIA driver (minimum version)
                    -> GPU hardware (compute capability)
```

Every link in this chain must be compatible with its neighbours. For example:

- PyTorch 2.1 supports CUDA 11.8 and CUDA 12.1, but not CUDA 11.7 or CUDA 12.0
- CUDA 12.1 requires cuDNN 8.9 or later
- CUDA 12.1 requires an NVIDIA driver version 530.30.02 or later
- The driver must support the compute capability of your specific GPU

A mismatch at any level produces errors that range from helpful ("CUDA driver version is insufficient for CUDA runtime version") to baffling (silent incorrect results, segfaults, or processes that hang during GPU operations). The difficulty is compounded by the fact that the same symptoms can result from completely different root causes.

The reason this problem is so persistent in the ML community is that the version matrix is large, not well documented in a single place, and changes with every new release. A setup that works today may break when you upgrade PyTorch, even if you do not consciously change anything about your CUDA configuration, because the new PyTorch version may have dropped support for your CUDA version.

## How Cog solves this

When you set `gpu: true` in your `cog.yaml`, Cog takes over the entire CUDA dependency chain. The approach has three parts.

### 1. A maintained compatibility matrix

Cog maintains a compatibility matrix that maps combinations of Python version, PyTorch version, and TensorFlow version to the correct CUDA toolkit version, cuDNN version, and nvidia base image. This matrix is generated and tested by the Cog maintainers and updated with each release.

The reason Cog uses a curated matrix rather than simply reading version requirements from PyTorch's metadata is that the published compatibility information is often incomplete or ambiguous. PyTorch may list "CUDA 11.8" as supported, but there are nuances about specific patch versions, cuDNN requirements, and base image availability that are not captured in a `requires` field. The matrix encodes this hard-won knowledge.

### 2. Automatic version detection

When you run `cog build`, Cog examines your Python requirements (from `python_requirements` or `python_packages`) and identifies which ML frameworks you are using and at which versions. It then looks up the correct CUDA configuration in the compatibility matrix.

For example, if your `requirements.txt` contains `torch==2.1.0`, Cog will:

1. Identify that PyTorch 2.1.0 is present
2. Look up the compatible CUDA version (e.g., CUDA 12.1)
3. Determine the matching cuDNN version
4. Select the correct nvidia base image (e.g., `nvidia/cuda:12.1.1-cudnn8-devel-ubuntu22.04`)

This happens transparently. You do not need to specify CUDA versions, cuDNN versions, or base images -- Cog derives them from your Python dependencies.

### 3. Pre-configured base images

Cog uses NVIDIA's official CUDA base images as the foundation for GPU-enabled containers. These images come with the CUDA toolkit, cuDNN, and related libraries pre-installed and correctly configured. By selecting the right base image tag, Cog ensures that the CUDA runtime inside your container matches the requirements of your ML framework.

Cog also provides its own pre-built base images (`r8.im/cog-base`) that include not just the CUDA stack but also common Python versions and PyTorch installations. These base images speed up builds significantly because the most time-consuming parts of the installation are already done. See [Docker image layer strategy](image-layers.md) for more on how base images affect build and deployment performance.

## When you need to override

In most cases, Cog's automatic detection works correctly. But there are situations where you may need to intervene.

### Using the `cuda` option

The `build.cuda` field in `cog.yaml` lets you explicitly specify a CUDA version:

```yaml
build:
  gpu: true
  cuda: "11.8"
```

This is useful when:

- **You are using a framework Cog does not recognise.** If your requirements include a CUDA-dependent library that is not PyTorch or TensorFlow (for example, a custom CUDA kernel or a less common framework like JAX), Cog cannot auto-detect the correct CUDA version. Specifying it manually ensures the right base image is used.

- **You need a specific CUDA version for compatibility with your host driver.** If your deployment target has an older NVIDIA driver that does not support the latest CUDA version, you may need to pin CUDA to an older version that your driver supports. The CUDA toolkit is forward-compatible (newer drivers support older CUDA), but not backward-compatible (older drivers do not support newer CUDA).

- **You are troubleshooting a compatibility issue.** If you suspect Cog's auto-detection has chosen the wrong CUDA version, explicitly setting it can help isolate the problem.

You can specify a minor version (`11.8`) or a patch version (`11.8.0`). If you specify a minor version, Cog selects the latest patch release for that minor version.

### The `--use-cuda-base-image` flag

When building, you can pass `--use-cuda-base-image=false` to skip the NVIDIA CUDA base image entirely and use a plain Python base image instead:

```
cog build --use-cuda-base-image=false
```

The tradeoff is clear: you get a significantly smaller image (CUDA base images add several gigabytes), but you lose the pre-installed CUDA toolkit. This can be effective for models that bundle their own CUDA dependencies (some PyTorch distributions include CUDA libraries in the pip package itself), but it may cause problems for frameworks that expect system-level CUDA installations.

Some teams prefer this approach because smaller images are faster to push and pull, which matters for deployment pipelines where cold start time is critical. However, the risk of subtle runtime failures from missing CUDA components means you should test thoroughly before deploying images built this way.

## The NVIDIA container runtime

Even with the correct CUDA version installed inside the container, the container needs access to the host's GPU hardware. This is handled by the [NVIDIA Container Toolkit](https://github.com/NVIDIA/nvidia-docker) (formerly known as nvidia-docker).

When you run a Cog container with GPU support, you must pass the `--gpus` flag to Docker:

```
docker run --gpus all my-model
```

This flag tells Docker to use the NVIDIA container runtime, which does several things:

- Mounts the host's NVIDIA driver libraries into the container
- Makes GPU devices visible inside the container
- Sets up the necessary environment variables for CUDA to find the driver

Cog's `cog predict` and `cog run` commands pass `--gpus all` automatically when `build.gpu` is true. When you run a Docker image directly, you must pass this flag yourself.

The reason the driver comes from the host rather than being included in the container is that the GPU driver is tightly coupled to the host's kernel. You cannot install an arbitrary driver version inside a container -- it must match the kernel module loaded on the host. The NVIDIA Container Toolkit solves this by injecting the host's driver at container runtime, so the container only needs the CUDA user-space libraries (which are included in the base image).

This architecture has an important implication: **the host's NVIDIA driver must be at least as new as the CUDA version inside the container requires.** CUDA 12.1 requires driver 530.30.02 or later. If the host has an older driver, the container will fail at runtime with a "CUDA driver version is insufficient" error, even if everything inside the container is correctly configured. This is the one part of the CUDA stack that Cog cannot control, because it depends on the deployment environment.

## Multiple frameworks

Some models use both PyTorch and TensorFlow, or use PyTorch alongside other CUDA-dependent libraries. Cog handles this by selecting a CUDA version that is compatible with all detected frameworks. If no single CUDA version satisfies all constraints, the build will fail with an error explaining the conflict.

In practice, this situation is uncommon because most models use a single primary framework. When it does occur, the resolution is usually to update one of the frameworks to a version that shares a common CUDA requirement, or to explicitly specify a CUDA version with `build.cuda` that you have verified works with all your dependencies.

## The driver compatibility model

NVIDIA's compatibility model is worth understanding because it affects what you can deploy and where:

- **Forward compatibility**: A newer host driver supports older CUDA versions. A host with driver 535 can run containers built for CUDA 11.8, 12.0, or 12.1.
- **No backward compatibility**: An older host driver cannot run containers with newer CUDA requirements. A host with driver 520 cannot run a container built for CUDA 12.1.

This means you should generally aim for the **oldest CUDA version that your framework supports**, unless you need specific features from a newer CUDA release. The older the CUDA version in your container, the wider the range of host driver versions it will work on.

Cog's auto-detection takes this into account and generally selects a CUDA version that balances compatibility with framework support. But if you control your deployment infrastructure and know exactly which driver version is installed, you can use `build.cuda` to select the most appropriate version.

## Further reading

- [How Cog works](how-cog-works.md) -- the build pipeline that resolves CUDA versions
- [Docker image layer strategy](image-layers.md) -- how CUDA base images affect image size and build time
- [`cog.yaml` reference](../yaml.md#cuda) -- the `cuda` and `gpu` configuration options
- [NVIDIA CUDA Compatibility](https://docs.nvidia.com/deploy/cuda-compatibility/) -- NVIDIA's official documentation on driver and toolkit compatibility
