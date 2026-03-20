# Docker image layer strategy

Docker images are not monolithic files -- they are composed of layers, each representing a set of filesystem changes. The order, content, and size of these layers directly affect how fast your image builds, how fast it pushes and pulls from registries, and how quickly your model starts serving predictions on a new machine. Cog structures its layers deliberately to optimise all three.

## How Docker layers work

Every instruction in a Dockerfile that modifies the filesystem (installing packages, copying files, running scripts) creates a new layer. When Docker builds an image, it caches each layer and reuses it as long as the inputs to that instruction have not changed.

The caching rule is strict and sequential: if layer N changes, all layers after N are invalidated and must be rebuilt, even if their own inputs have not changed. This means the order of operations in a Dockerfile has a profound impact on build performance.

Consider a naive Dockerfile that copies your code first, then installs Python packages:

```dockerfile
COPY . /src
RUN pip install -r requirements.txt
```

Every time you change a single line of Python code, Docker invalidates the `COPY` layer, which invalidates the `pip install` layer, which reinstalls all your packages from scratch. For a model with heavy dependencies like PyTorch (several gigabytes), this turns a 5-second code change into a 10-minute rebuild.

## Cog's layer ordering

Cog generates Dockerfiles with a carefully chosen layer order that maximises cache hits during iterative development:

```
1. Base image (OS + CUDA + system libraries)
2. System packages (apt-get install)
3. Python installation
4. Python packages (pip install)
5. Cog SDK and coglet
6. Build-time commands (the `run` stanza in cog.yaml)
7. Your model code
```

The reason for this specific ordering is that it follows the frequency of change, from least to most:

- **Base image and system packages** almost never change. Once you have settled on your OS, CUDA version, and system dependencies, these layers remain cached indefinitely.
- **Python packages** change occasionally -- when you add a new dependency or upgrade a version. But between those changes, the layer is cached.
- **Your model code** changes constantly during development. By placing it last, code changes never invalidate the expensive package installation layers above.

This means that during normal development, `cog build` only needs to re-execute the final `COPY` step. Rebuilds take seconds, not minutes.

## The `--separate-weights` flag

Model weights are a unique challenge for Docker images. A single model's weights can be anywhere from hundreds of megabytes to tens of gigabytes. Without special handling, these weights are baked into the same layer as your code, which creates two problems:

1. **Every code change re-uploads the weights.** When you push an image to a registry, Docker pushes only the layers that have changed. If your weights and code are in the same layer, changing one line of your `predict.py` forces a re-push of the entire layer, including all the weights. For a model with 7 GB of weights, this turns a quick code fix into a lengthy upload.

2. **Layer deduplication is lost.** If you have multiple versions of the same model that share the same weights but differ in code, Docker cannot deduplicate the weights because they are mixed in with different code in each version.

The `--separate-weights` flag solves this by placing model weights in a dedicated layer, separate from your code:

```
cog build --separate-weights -t my-model
```

With this flag, Cog generates a multi-stage build where:

- **Weights layer**: Contains your model weight files (large, changes rarely)
- **Code layer**: Contains your Python code and other non-weight files (small, changes often)

Cog uses file naming patterns to identify which files are likely weights (e.g., `.pth`, `.bin`, `.safetensors`, `.gguf`, and others). It also examines directories in your project to distinguish model data from code. The classification is based on heuristics, and the `.dockerignore` file is temporarily modified during the build to exclude weights from the code layer and vice versa.

The practical benefit is significant. After the initial push of a model with large weights, subsequent code-only changes result in pushes of just a few megabytes rather than several gigabytes. For teams iterating quickly on model code, this can reduce push times from minutes to seconds.

### When to use `--separate-weights`

Using `--separate-weights` is beneficial when:

- Your model weights are stored in the image (as opposed to being downloaded during `setup()`)
- The weights are significantly larger than your code
- You expect to iterate on code without changing weights

It is less useful when your weights are downloaded during `setup()` (they are not in the image at all), or when your weights change as often as your code (the weights layer would be invalidated anyway).

Some teams prefer to download weights during `setup()` to keep image sizes small. Others prefer to bake weights into the image for faster cold starts and better reproducibility. The `--separate-weights` flag is designed for the latter approach, mitigating its main disadvantage (large pushes on code changes) while preserving its benefits (self-contained images, no external dependencies at runtime). See the [`setup()` documentation](../python.md#predictorsetup) for a more detailed discussion of these trade-offs.

## Cog base images

Cog provides pre-built base images (`r8.im/cog-base`) that include the operating system, CUDA toolkit, Python, and common ML framework installations. These base images exist to solve two performance problems.

### Faster builds

Without a base image, every `cog build` must install the CUDA toolkit, Python, and your framework from scratch. For a GPU model using PyTorch, this can take 5-15 minutes even with good network connectivity. With a Cog base image, these components are already installed in the base layer, and `cog build` only needs to install your additional Python packages and copy your code.

The base image is tagged with its exact configuration -- for example, `r8.im/cog-base:cuda12.1-python3.12-torch2.3.1`. Cog selects the appropriate tag based on your `cog.yaml` configuration. If no matching base image exists (because you are using an unusual combination of versions), Cog falls back to building from a standard NVIDIA CUDA base image.

### Faster cold starts

When a container is pulled to a new machine for the first time, Docker must download all layers of the image. With a Cog base image, the base layers are likely already present on the machine if it has previously run other Cog models with the same Python and CUDA configuration. Docker only needs to download the layers unique to your model.

This is particularly valuable in deployment environments where many different models run on a shared pool of machines. The base image layers are effectively shared infrastructure, amortised across all models that use them.

### The `--use-cog-base-image` flag

Cog uses base images by default. You can disable this with:

```
cog build --use-cog-base-image=false
```

The reason you might want to disable base images is control. Base images are maintained by the Cog team and updated on their release schedule. If you need a specific patch version of a system library, or want to audit every component in your image, building from the raw NVIDIA base image gives you that control at the cost of longer build times.

## The `--use-cuda-base-image` flag

This flag controls whether Cog starts from an NVIDIA CUDA base image or a plain Python base image:

```
cog build --use-cuda-base-image=false
```

The tradeoff is straightforward: CUDA base images add several gigabytes to your image because they include the full CUDA toolkit and cuDNN. Some PyTorch distributions ship their own CUDA libraries bundled in the pip package, which means the system-level CUDA installation is technically redundant.

By using `--use-cuda-base-image=false`, you can produce significantly smaller images. Smaller images are faster to push, pull, and store. However, this approach carries risk: if any part of your dependency chain expects system-level CUDA (a custom CUDA kernel, a library compiled against system CUDA, or a future PyTorch version that changes its bundling strategy), your model will fail at runtime.

Some teams use this flag aggressively for deployment speed and accept the risk. Others prefer the safety of the full CUDA base image. There is no universally correct answer -- it depends on your specific dependencies and how thoroughly you can test the resulting image.

## How `.dockerignore` affects the image

Docker sends your entire project directory as the "build context" to the Docker daemon. The `.dockerignore` file controls which files are excluded from this context, and therefore which files can end up in your image.

A well-configured `.dockerignore` is important for two reasons:

1. **Build performance.** Sending unnecessary files (training data, datasets, logs, `.git` directories) to the Docker daemon slows down the build, even if those files are not ultimately copied into the image.

2. **Image size.** Files that are copied into the image but not needed at runtime (test files, documentation, training scripts) waste space in the final image and in every subsequent push and pull.

When you run `cog init`, Cog generates a `.dockerignore` file with sensible defaults. During builds with `--separate-weights`, Cog temporarily modifies the `.dockerignore` to separate weight files from code files across the two build stages. This modification is reverted after the build completes.

As a general principle, your `.dockerignore` should exclude everything that is not needed to run predictions. This typically includes:

- `.git/` (repository history)
- Training data and datasets
- Notebooks and training scripts (unless referenced by your predictor)
- Test files
- Documentation
- Virtual environments and local build artifacts

## Putting it all together

The layer strategy in a typical Cog image looks like this for a GPU model with `--separate-weights`:

```
Layer 1:  Base image (OS + CUDA + Python + PyTorch) [shared across models]
Layer 2:  System packages from cog.yaml              [changes rarely]
Layer 3:  Python packages from requirements.txt      [changes occasionally]
Layer 4:  Cog SDK + coglet                            [tied to cog version]
Layer 5:  Build commands from `run` stanza            [changes rarely]
Layer 6:  Model weights                               [changes rarely]
Layer 7:  Your model code                             [changes frequently]
```

During development, only layer 7 changes. When you push to a registry, only the changed layers are uploaded. When a new machine pulls the image, shared base layers may already be cached. The result is that the most common operations -- rebuilding after a code change, pushing a code update, pulling to a machine that has run similar models -- are all fast.

This is not accidental. The layer ordering reflects a deliberate design philosophy: optimise for the common case (iterative code changes) while making the uncommon case (changing dependencies or weights) as painless as possible.

## Further reading

- [How Cog works](how-cog-works.md) -- the build pipeline that generates these layers
- [CUDA and GPU compatibility](cuda-compat.md) -- how base image selection relates to CUDA version management
- [`cog.yaml` reference](../yaml.md) -- configuration options that affect the generated Dockerfile
- [Deploy models with Cog](../deploy.md) -- running the built images in production
