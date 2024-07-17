# `cog.yaml` reference

`cog.yaml` defines how to build a Docker image and how to run predictions on your model inside that image.

It has three keys: [`build`](#build), [`image`](#image), and [`predict`](#predict). It looks a bit like this:

```yaml
build:
  python_version: "3.11"
  python_packages:
    - pytorch==2.0.1
  system_packages:
    - "ffmpeg"
    - "git"
predict: "predict.py:Predictor"
```

Tip: Run [`cog init`](getting-started-own-model.md#initialization) to generate an annotated `cog.yaml` file that can be used as a starting point for setting up your model.

## `build`

This stanza describes how to build the Docker image your model runs in. It contains various options within it:

<!-- Alphabetical order, please! -->

### `cuda`

Cog automatically picks the correct version of CUDA to install, but this lets you override it for whatever reason by specifying the minor (`11.8`) or patch (`11.8.0`) version of CUDA to use.

For example:

```yaml
build:
  cuda: "11.8"
```

### `gpu`

Enable GPUs for this model. When enabled, the [nvidia-docker](https://github.com/NVIDIA/nvidia-docker) base image will be used, and Cog will automatically figure out what versions of CUDA and cuDNN to use based on the version of Python, PyTorch, and Tensorflow that you are using.

For example:

```yaml
build:
  gpu: true
```

When you use `cog run` or `cog predict`, Cog will automatically pass the `--gpus=all` flag to Docker. When you run a Docker image built with Cog, you'll need to pass this option to `docker run`.

### `python_packages`

A list of Python packages to install from the PyPi package index, in the format `package==version`. For example:

```yaml
build:
  python_packages:
    - pillow==8.3.1
    - tensorflow==2.5.0
```

To install Git-hosted Python packages, add `git` to the `system_packages` list, then use the `git+https://` syntax to specify the package name. For example:

```yaml
build:
  system_packages:
    - "git"
  python_packages:
    - "git+https://github.com/huggingface/transformers"
```

You can also pin Python package installations to a specific git commit:

```yaml
build:
  system_packages:
    - "git"
  python_packages:
    - "git+https://github.com/huggingface/transformers@2d1602a"
```

Note that you can use a shortened prefix of the 40-character git commit SHA, but you must use at least six characters, like `2d1602a` above.

### `python_requirements`

A pip requirements file specifying the Python packages to install. For example:

```yaml
build:
  python_requirements: requirements.txt
```

Your `cog.yaml` file can set either `python_packages` or `python_requirements`, but not both. Use `python_requirements` when you need to configure options like `--extra-index-url` or `--trusted-host` to fetch Python package dependencies.

### `python_version`

The minor (`3.11`) or patch (`3.11.1`) version of Python to use. For example:

```yaml
build:
  python_version: "3.11.1"
```

Cog supports all active branches of Python: 3.8, 3.9, 3.10, 3.11, 3.12. If you don't define a version, Cog will use the latest version of Python 3.12 or a version of Python that is compatible with the versions of PyTorch or TensorFlow you specify.

Note that these are the versions supported **in the Docker container**, not your host machine. You can run any version(s) of Python you wish on your host machine.

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

Each command in `run` can be either a string or a dictionary in the following format:

```yaml
build:
  run:
    - command: pip install
      mounts:
        - type: secret
          id: pip
          target: /etc/pip.conf
```

You can use secret mounts to securely pass credentials to setup commands, without baking them into the image. For more information, see [Dockerfile reference](https://docs.docker.com/engine/reference/builder/#run---mounttypesecret).

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

If you don't set this, then a name will be generated from the directory name.

If you set this, then you can run `cog push` without specifying the model name. 

If you specify an image name argument when pushing (like `cog push your-username/custom-model-name`), the argument will be used and the value of `image` in cog.yaml will be ignored.

## `predict`

The pointer to the `Predictor` object in your code, which defines how predictions are run on your model.

For example:

```yaml
predict: "predict.py:Predictor"
```

See [the Python API documentation for more information](python.md).
