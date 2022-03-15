# `cog.yaml` reference

`cog.yaml` defines how to build a Docker image and how to run predictions on your model inside that image.

It has three keys: [`build`](#build), [`image`](#image), and [`predict`](#predict). It looks a bit like this:

```yaml
build:
  python_version: "3.8"
  python_packages:
    - pytorch==1.4.0
  system_packages:
    - "ffmpeg"
    - "libavcodec-dev"
predict: "predict.py:Predictor"
```

Tip: Run [`cog init`](getting-started-own-model#initialization) to generate an annotated `cog.yaml` file that can be used as a starting point for setting up your model.

## `build`

This stanza describes how to build the Docker image your model runs in. It contains various options within it:

<!-- Alphabetical order, please! -->

### `cuda`

Cog automatically picks the correct version of CUDA to install, but this lets you override it for whatever reason.

### `environment`

Set environment variables in the Dockerfile using [`ENV` instructions](https://docs.docker.com/engine/reference/builder/#env).
These will be set in the Docker image, so your `predict.py` and imported libraries will be able to use them.

For example:

```yaml
build:
  environment:
    - SOME_DIR=/src/example
    - ANOTHER_DIR=$SOME_DIR/weights
    - DEBUG=
```

That example would set `$SOME_DIR` to the string `/src/example` and `$ANOTHER_DIR` to `/src/example/weights`.  `DEBUG` would be set to an empty string.

<details>
<summary>Telling libraries where to cache things</summary>

Cog already re-uses `/src/` across invocations; so, if we tell libraries to cache inside of `/src/`, the cached files will be persisted across invocations.

Caching between runs will "just work" for some libraries, including PyTorch.
This is because Cog now sets the default of `XDG_CACHE_HOME=/src/.cache`. You can override it if needed.
[PyTorch](https://pytorch.org/docs/stable/hub.html#:~:text=XDG_CACHE_HOME) and many popular libraries [such as HF](https://huggingface.co/transformers/v4.0.1/installation.html#caching-models)
support using `XDG_CACHE_HOME` to tell them where to put their cache.
(`XDG_CACHE_HOME` is part of [a standard.](https://specifications.freedesktop.org/basedir-spec/basedir-spec-latest.html#:~:text=%24XDG_CACHE_HOME%20defines%20the%20base%20directory%20relative%20to%20which%20user%2Dspecific%20non%2Dessential%20data%20files%20should%20be%20stored.%20If%20%24XDG_CACHE_HOME%20is%20either%20not%20set%20or%20empty%2C%20a%20default%20equal%20to%20%24HOME/.cache%20should%20be%20used.))
 
PyTorch users do not need to set [`TORCH_HOME`](https://pytorch.org/docs/stable/hub.html#:~:text=TORCH_HOME) because [PyTorch respects `XDG_CACHE_HOME`](https://pytorch.org/docs/stable/hub.html#:~:text=XDG_CACHE_HOME) and Cog sets `XDG_CACHE_HOME`. But if you really want to, you can set `TORCH_HOME` or any environment variable you want.

If you need to store additional files inside `/src/.cache`, go ahead! You can refer to `XDG_CACHE_HOME` in the `environment` directive like so:

```yaml
build:
  environment:
    - EXAMPLE=$XDG_CACHE_HOME/example
```

In that case `$EXAMPLE` would be set to `/src/.cache/example`. The default gets interpolated.

</details>

<details>
<summary>You can pre-cache before you do cog push</summary>

Whatever is within `/src/` when you do `cog push` will get "baked" into the image, so you can use this feature to "pre-cache" data. Pre-caching can help your model start faster by skipping data downloads. Just store/read data within `/src/` or `/src/.cache`.

In other words, if your `predict.py` downloads data to `/src/.cache` or `$XDG_CACHE_HOME`, you could do `cog predict` once locally before you do `cog push`.

If you have a separate preparation script to be run on the host machine, it's up to you how to do it. 
We'd recommend using the same environment variable(s) in that script and your `cog.yaml`.
On your host, make sure the data winds up in the working directory that corresponds to `/src/` or `/src/.cache/`.
Often, Cog users make their `predict.py` get-or-fetch data; in such a case, they can run one prediction, verify the output, then do `cog push`.

**Warning:** You should **not** copy the whole `~/.cache` directory from your host, as it could contain sensitive/unrelated files. Copy only what you need.

</details>

<details>
<summary>Tip: git ignore .cache</summary>

You may already have `.cache` in your `.gitignore`. If not, you can add it easily:

```shell
git ignore .cache
git add .gitignore
git commit -m "Ignore .cache"
```

</details>

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

Cog supports all active branches of Python: 3.7, 3.8, 3.9, 3.10.

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

## `predict`

The pointer to the `Predictor` object in your code, which defines how predictions are run on your model.

For example:

```yaml
predict: "predict.py:Predictor"
```

See [the Python API documentation for more information](python.md).
