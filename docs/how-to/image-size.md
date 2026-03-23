# How to optimise Docker image size

This guide shows you how to reduce the size of your Cog-built Docker images to speed up builds, pushes, and cold starts.

## Use a Python base image instead of CUDA

By default, GPU-enabled models use an NVIDIA CUDA base image, which is several gigabytes. If your model uses PyTorch, the CUDA runtime is already bundled in the PyTorch wheel, so you can use a smaller Python base image instead:

```console
cog build --use-cuda-base-image=false -t my-model
```

This produces a significantly smaller image. However, it may not work for non-PyTorch projects that depend on system CUDA libraries. Test your model after switching.

This flag also works with `cog push`:

```console
cog push r8.im/your-username/my-model --use-cuda-base-image=false
```

## Pin exact package versions

Unpinned dependencies pull in the latest version on every build, which can bloat the image with unnecessary transitive dependencies. Pin every package in your `requirements.txt`:

```
torch==2.1.0
transformers==4.35.0
Pillow==10.1.0
```

To generate a pinned requirements file from your current environment:

```console
pip freeze > requirements.txt
```

This also makes builds reproducible, which helps with layer caching.

## Clean up in build.run commands

Each `build.run` command creates a Docker layer. Clean up temporary files in the same command to avoid bloating the layer:

```yaml
build:
  run:
    - apt-get update && apt-get install -y --no-install-recommends libgl1 && rm -rf /var/lib/apt/lists/*
    - pip install flash-attn --no-build-isolation && pip cache purge
```

If you download and extract an archive, remove it in the same step:

```yaml
build:
  run:
    - curl -L https://example.com/data.tar.gz -o /tmp/data.tar.gz && tar xzf /tmp/data.tar.gz -C /opt/data && rm /tmp/data.tar.gz
```

## Use .dockerignore

Cog copies your entire project directory into the image. Exclude files that are not needed at runtime:

```
# .dockerignore
.git
.github
*.md
tests/
notebooks/
__pycache__
*.pyc
.env
```

If you store weights in the repository but download them in `setup()`, exclude them too:

```
weights/
*.safetensors
*.bin
```

## Separate weights from code

If your weights are baked into the image, use `--separate-weights` to store them in a dedicated Docker layer:

```console
cog build --separate-weights -t my-model
```

This does not reduce the total image size, but it means code-only changes only push a small layer instead of re-uploading gigabytes of weights. This makes iterative development and CI/CD much faster.

## Choose minimal system packages

Only install the system packages your model actually needs. Each package adds to the image size and pulls in its own dependencies.

Instead of:

```yaml
build:
  system_packages:
    - "ffmpeg"
    - "imagemagick"
    - "libopencv-dev"
    - "vim"
```

Use only what is required:

```yaml
build:
  system_packages:
    - "ffmpeg"
```

If you need a library but not the full development headers, use the runtime package instead of the `-dev` variant. For example, `libgl1` instead of `libgl1-mesa-dev`.

## Avoid unnecessary Python dependencies

Review your `requirements.txt` for packages that are only used during development or testing:

- Remove `jupyter`, `matplotlib`, `tensorboard`, etc. if they are not used at prediction time.
- If a package is only needed for training (not inference), do not include it.

## Inspect image size

To see what is taking up space in your image:

```console
docker images my-model
```

For a detailed breakdown of layer sizes:

```console
docker history my-model:latest
```

If a specific layer is unexpectedly large, check the corresponding build step for leftover temporary files or unnecessary packages.

## Next steps

- See [How to manage model weights](model-weights.md) for weight storage strategies.
- See [How to debug build failures](debug-builds.md) for troubleshooting build issues.
- See the [`cog.yaml` reference](../yaml.md) for all build configuration options.
