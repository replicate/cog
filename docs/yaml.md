# `cog.yaml` reference

`cog.yaml` defines how to build a Docker image and how to run predictions on your model inside that image.

It has three keys: `environment`, `image`, and `model`. It looks a bit like this:

```yaml
environment:
  python_version: "3.8"
  python_packages:
    - pytorch==1.4.0
  system_packages:
    - "ffmpeg"
    - "libavcodec-dev"
model: "model.py:JazzSoloComposerModel"
```

## `environment`

This defines the environment the model runs in. It contains various options within it:

<!-- Alphabetical order, please! -->

### `cuda`

Cog automatically picks the correct version of CUDA to install, but this lets you override it for whatever reason.

### `gpu`

Enable GPUs for this model. When enabled, the [nvidia-docker](https://github.com/NVIDIA/nvidia-docker) base image will be used, and Cog will automatically figure out what versions of CUDA and cuDNN to use based on the version of Python, PyTorch, and Tensorflow that you are using.

When you use `cog run` or `cog predict`, Cog will automatically pass the `--gpus=all` flag to Docker. When you run a Docker image build with Cog, you'll need to pass this option to `docker run`.

For example:

```yaml
environment:
  gpu: true
```

### `python_packages`

A list of Python packages to install, in the format `package=version`. For example:

```yaml
environment:
  python_packages:
    - pillow==8.3.1
    - tensorflow==2.5.0
```

### `python_version`

The minor (`3.8`) or patch (`3.8.1`) version of Python to use. For example:

```yaml
environment:
  python_version: "3.8.1"
```

### `system_packages`

A list of Ubuntu APT packages to install. For example:

```yaml
environment:
  system_packages:
    - "ffmpeg"
    - "libavcodec-dev"
```

## `image`

The name given to built Docker images. If you want to push to a registry, this should also include the registry name.

If you don't provide this, a name will be generated from the directory name.

For example:

```yaml
image: "registry.hooli.corp/jazz-solo-model"
```

## `model`

The pointer to the `cog.Model` object in your code, which defines how predictions are run on your model.

For example:

```yaml
model: "predict.py:HotdogDetector"
```

See [the Python API documentation for more information](python.md).
