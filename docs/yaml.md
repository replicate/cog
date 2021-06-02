# `cog.yaml` reference

`cog.yaml` defines the entrypoint to your model and the environment it runs in.

It has two required keys: `model` and `environment`. It looks a bit like this:

```yaml
model: "model.py:JazzSoloComposerModel"
environment:
  python_version: "3.8"
  python_requirements: "requirements.txt"
  system_packages:
    - "ffmpeg"
    - "libavcodec-dev"
```

## `model`

The pointer to the `cog.Model` object in your code, which defines how predictions are run on your model.

For example:

```yaml
model: "predict.py:HotdogDetector"
```

See [the Python API documentation for more information](python.md).

## `environment`

This defines the environment the model runs in. It contains various options within it:

### `cuda`

Cog automatically picks the correct version of CUDA to install, but this lets you override it for whatever reason.

### `python_requirements`

The path to a `requirements.txt` file to install. For example:

```yaml
environment:
  python_requirements: "requirements.txt"
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

### `architectures`

List of architectures (`cpu` or `gpu`) to build Docker images for. Useful if the model only works on either CPU or GPU. Defaults to [`cpu`, `gpu`].
