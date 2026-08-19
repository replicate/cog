# `cog.yaml` reference

`cog.yaml` defines how to build a Docker image and how to run your model inside that image.

It has three keys: [`build`](#build), [`image`](#image), and [`run`](#run). It looks a bit like this:

```yaml
build:
  python_version: "3.13"
  python_requirements: requirements.txt
  system_packages:
    - "ffmpeg"
    - "git"
run: "run.py:Runner"
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

When you use `cog exec` or `cog run`, Cog will automatically pass the `--gpus=all` flag to Docker. When you run a Docker image built with Cog, you'll need to pass this option to `docker run`.

### `python_requirements`

A pip requirements file specifying the Python packages to install. For example:

```yaml
build:
  python_requirements: requirements.txt
```

Your `cog.yaml` file can set either `python_packages` or `python_requirements`, but not both. Use `python_requirements` when you need to configure options like `--extra-index-url` or `--trusted-host` to fetch Python package dependencies.

This follows the standard [requirements.txt](https://pip.pypa.io/en/stable/reference/requirements-file-format/) format.

Requirements files can list a local wheel or source archive:

`requirements.txt`:

```
./dist/mylib-0.1.0-py3-none-any.whl
./vendor/helperlib.zip
./packages/localpkg.tar.gz
```

Cog supports `.whl`, `.zip`, `.tar.gz`, `.tgz`, `.tar.bz2`, and `.tar.xz` files. Paths may contain spaces, are resolved relative to the requirements file, and must stay inside the project directory.

Only bare paths are supported. Local directories, local direct references such as `name @ path`, and options, hashes, extras, or markers on a local artifact line are rejected. Remote direct references remain supported. Cog overrides any `cog` or `coglet` distribution installed by a local artifact; use `build.sdk_version` or `COG_SDK_WHEEL` for Cog, and `COGLET_WHEEL` for Coglet.

To install Git-hosted Python packages, add `git` to the `system_packages` list, then use the `git+https://` syntax to specify the package name. For example:

`cog.yaml`:

```yaml
build:
  system_packages:
    - "git"
  python_requirements: requirements.txt
```

`requirements.txt`:

```
git+https://github.com/huggingface/transformers
```

You can also pin Python package installations to a specific git commit:

`cog.yaml`:

```yaml
build:
  system_packages:
    - "git"
  python_requirements: requirements.txt
```

`requirements.txt`:

```
git+https://github.com/huggingface/transformers@2d1602a
```

Note that you can use a shortened prefix of the 40-character git commit SHA, but you must use at least six characters, like `2d1602a` above.

### `python_packages`

**DEPRECATED**: This will be removed in future versions, please use [python_requirements](#python_requirements) instead.

A list of Python packages to install from the PyPi package index, in the format `package==version`. For example:

```yaml
build:
  python_packages:
    - pillow==8.3.1
    - tensorflow==2.5.0
```

Your `cog.yaml` file can set either `python_packages` or `python_requirements`, but not both.

### `python_version`

The minor (`3.13`) or patch (`3.13.1`) version of Python to use. For example:

```yaml
build:
  python_version: "3.13.1"
```

Cog supports Python 3.10, 3.11, 3.12, and 3.13. If you don't define a version, Cog will use the latest version of Python 3.13 or a version of Python that is compatible with the versions of PyTorch or TensorFlow you specify.

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

Your source code is not available to `run` commands. List local wheels and source archives in `python_requirements` instead.

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

### `sdk_version`

Pin the version of the cog Python SDK installed in the container. Accepts a [PEP 440](https://peps.python.org/pep-0440/) version string. When omitted, the latest release is installed.

```yaml
build:
  python_version: "3.13"
  sdk_version: "0.18.0"
```

Pre-release versions are also supported:

```yaml
build:
  sdk_version: "0.18.0a1"
```

When a pre-release `sdk_version` is set, `--pre` is automatically passed to the pip install commands for both `cog` and `coglet`, so pip will resolve matching pre-release packages.

The minimum supported version is `0.16.0`. Specifying an older version will cause `cog build` to fail with an error.

The `COG_SDK_WHEEL` environment variable takes precedence over `sdk_version`. See [Environment variables](./environment.md) for details.

### `system_packages`

A list of Ubuntu APT packages to install. For example:

```yaml
build:
  system_packages:
    - "ffmpeg"
    - "libavcodec-dev"
```

## `concurrency`

> Added in cog 0.14.0.
> Deprecated: use [`@cog.concurrent(max=N)`](python.md#async-runners-and-concurrency) on your async `run()` method instead.

This stanza describes the concurrency capabilities of the model. It is still supported for backwards compatibility, but new models should use `@cog.concurrent(max=N)`. It has one option:

### `max`

The maximum number of concurrent runs the model can process. If this is set, the model must specify an [async `run()` method](python.md#async-runners-and-concurrency).

If both `concurrency.max` and `@cog.concurrent(max=N)` are set, `concurrency.max` takes precedence and is the value baked into the image. Remove `concurrency.max` when migrating to `@cog.concurrent`.

For example:

```yaml
concurrency:
  max: 10
```

## `observability`

OpenTelemetry tracing is disabled by default. Enable it for an image with:

```yaml
observability:
  traces:
    enabled: true
    sampler: parentbased_always_off
```

`config` is an optional project-relative Python file for customizing the Python tracer provider. It requires `traces.enabled: true`. The file must define `create_tracer_provider()` returning `opentelemetry.sdk.trace.TracerProvider` and may define `configure_instrumentation()`. Cog installs the returned provider before importing the model and flushes and shuts it down with the worker.

This hook affects model-authored Python spans only. Cog's Rust framework spans continue to use the standard runtime OpenTelemetry configuration. See [Observability](observability.md#custom-python-tracing) for examples and lifecycle details.

The default sampler continues sampled caller traces but does not start new traces. Supported sampler names are `always_on`, `always_off`, `traceidratio`, `parentbased_always_on`, `parentbased_always_off`, and `parentbased_traceidratio`. Ratio samplers require `sampler_arg` as a string between `"0"` and `"1"`.

An operator may configure one additional inbound trace header:

```yaml
observability:
  traces:
    enabled: true
    trace_header: x-company-trace
    trace_header_format: w3c # or jaeger
```

Collector endpoints, protocols, authentication headers, and certificates are runtime configuration and cannot be set through `cog.yaml`.

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

## `run`

The pointer to the `Runner` object in your code, which defines how runs are executed on your model.

For example:

```yaml
run: "run.py:Runner"
```

`predict:` is still accepted for existing projects, but it is deprecated. New projects should use `run:`.

See [the Python API documentation for more information](python.md).

## `predict`

Deprecated compatibility field for [`run`](#run). Existing projects can continue using it, but Cog will warn and `cog doctor --fix` can migrate common projects to `run:`.

For example:

```yaml
predict: "predict.py:Predictor"
```

See [the Python API documentation for more information](python.md).
