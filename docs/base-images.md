# Base images

Cog builds your model into a Docker image. To speed up builds and reduce cold boot times, Cog uses **prebuilt base images** by default. These images contain the common dependencies that most models need, so Cog doesn't have to install them from scratch every time you build or push.

## What is a Cog base image?

A Cog base image is a Docker image maintained by Replicate that includes:

- **Python runtime** — the Python version specified in your `cog.yaml`.
- **System libraries** — common dependencies for machine learning and media processing, including `ffmpeg`, `git`, `curl`, `build-essential`, `cmake`, OpenCV libraries, audio libraries (`sox`, `libsndfile1`), and graphics libraries (`libgl1`, `libsm6`, `libxext6`).
- **CUDA and PyTorch** (GPU images only) — the appropriate CUDA toolkit and PyTorch stack for your configuration.
- **Cog runtime** — the Cog SDK and coglet prediction server.

Base images are tagged by their configuration, for example:

```
r8.im/cog-base:cuda12-python3.13-torch2.6
r8.im/cog-base:python3.13
```

When you run `cog build` or `cog push`, Cog selects the base image that matches your Python version, CUDA version, and PyTorch version. Because these images are pre-pulled on Replicate's infrastructure, models built on top of them start faster.

## Using the Cog base image

The `--use-cog-base-image` flag controls whether Cog uses a prebuilt base image. It is **enabled by default** on the following commands:

- [`cog build`](cli.md#cog-build)
- [`cog push`](cli.md#cog-push)
- [`cog run`](cli.md#cog-run)
- [`cog serve`](cli.md#cog-serve)
- [`cog exec`](cli.md#cog-exec)

Since it's on by default, you don't need to pass any flags:

```bash
cog push r8.im/your-username/my-model
```

This builds and pushes your model using a prebuilt Cog base image for faster cold boots.

## Disabling the Cog base image

If you encounter build issues or need a custom base layer, you can disable the Cog base image:

```bash
cog build --use-cog-base-image=false
```

When disabled, Cog generates a Dockerfile from scratch using either an NVIDIA CUDA base image or a slim Python base image, depending on the `--use-cuda-base-image` flag.

## Relationship to `--use-cuda-base-image`

The `--use-cuda-base-image` flag controls which underlying base image Cog uses **when the Cog base image is disabled**. It has no effect when `--use-cog-base-image` is enabled (the default), because the Cog base image already includes the appropriate CUDA and Python stack.

When you disable the Cog base image with `--use-cog-base-image=false`, Cog chooses the base image automatically:

- **GPU models** (`gpu: true` in `cog.yaml`): uses an NVIDIA CUDA base image.
- **CPU models**: uses a slim Python base image.

These flags — along with `--dockerfile` — are **mutually exclusive**: you can only set one of them explicitly on a given command. To customize the base image, disable the Cog base image and let Cog choose between CUDA and Python automatically.

## Troubleshooting

If `cog build` or `cog push` fails with the Cog base image enabled, try disabling it:

```bash
cog push --use-cog-base-image=false r8.im/your-username/my-model
```

This falls back to building from a standard CUDA or Python base image, which can help diagnose whether the issue is with the base image or your model's configuration.
