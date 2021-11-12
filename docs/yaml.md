# `cog.yaml` reference

`cog.yaml` defines how to build a Docker image and how to run predictions on your model inside that image.

It has three keys: [#build](`build`), [#image](`image`), and [#predict](`predict`). It looks a bit like this:

```yaml
build:
  python_version: "3.8"
  python_packages:
    - pytorch==1.4.0
  system_packages:
    - "ffmpeg"
    - "libavcodec-dev"
predict: "predict.py:JazzSoloComposerPredictor"
```

Tip: Run [`cog init`](getting-started-own-model#initialization) to generate an annotated `cog.yaml` file that can be used as a starting point for setting up your model.

## `build`

This stanza describes how to build the Docker image your model runs in. It contains various options within it:

<!-- Alphabetical order, please! -->

### `cuda`

Cog automatically picks the correct version of CUDA to install, but this lets you override it for whatever reason.

### `gpu`

Enable GPUs for this model. When enabled, the [nvidia-docker](https://github.com/NVIDIA/nvidia-docker) base image will be used, and Cog will automatically figure out what versions of CUDA and cuDNN to use based on the version of Python, PyTorch, and Tensorflow that you are using.

For example:

```yaml
build:
  gpu: true
```

When you use `cog run` or `cog predict`, Cog will automatically pass the `--gpus=all` flag to Docker. When you run a Docker image built with Cog, you'll need to pass this option to `docker run`.

### `python_packages`

A list of Python packages to install, in the format `package==version`. For example:

```yaml
build:
  python_packages:
    - pillow==8.3.1
    - tensorflow==2.5.0
```

### `python_version`

The minor (`3.8`) or patch (`3.8.1`) version of Python to use. For example:

```yaml
build:
  python_version: "3.8.1"
```

### `run`

A list of setup commands to run in the environment after your system packages and Python packages have been installed. If you're familiar with Docker, it's like a `RUN` instruction in your `Dockerfile`.

For example:

```yaml
build:
  run:
    - curl -L https://github.com/cowsay-org/cowsay/archive/refs/tags/v3.7.0.tar.gz | tar -xzf -
    - cd cowsay-3.7.0 && make install
```

Your code is _not_ available to commands in `run`. This is so we can build your image efficiently when running locally.

### `system_packages`

A list of Ubuntu APT packages to install. For example:

```yaml
build:
  system_packages:
    - "ffmpeg"
    - "libavcodec-dev"
```

## `image`

The name given to built Docker images. If you want to push to a registry, this should also include the registry name.

For example:

```yaml
image: "r8.im/your-username/your-model"
```

r8.im is Replicate's registry, but this can be any Docker registry.

If you don't provide this, a name will be generated from the directory name.

## `predictor`

The pointer to the `cog.Predictor` object in your code, which defines how predictions are run on your model.

For example:

```yaml
predict: "predict.py:HotdogPredictor"
```

See [the Python API documentation for more information](python.md).
