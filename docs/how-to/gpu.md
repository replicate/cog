# How to use GPUs

This guide shows you how to enable GPU support in your Cog model, configure CUDA versions, and troubleshoot common GPU issues.

## Enable GPU support

To run your model on a GPU, set `gpu: true` in your `cog.yaml`:

```yaml
build:
  gpu: true
  python_version: "3.12"
  python_requirements: requirements.txt
predict: "predict.py:Predictor"
```

When `gpu: true` is set, Cog uses an NVIDIA CUDA base image and automatically installs the correct versions of CUDA and cuDNN based on your Python, PyTorch, and TensorFlow versions.

## Let Cog auto-detect the CUDA version

In most cases, you do not need to specify a CUDA version. Cog inspects your `requirements.txt` for PyTorch or TensorFlow and selects a compatible CUDA version automatically.

For example, if your `requirements.txt` contains:

```
torch==2.1.0
```

Cog will select a CUDA version that is compatible with PyTorch 2.1.0. This is the recommended approach.

## Override the CUDA version

If you need a specific CUDA version -- for example, to match a pre-compiled library -- set the `cuda` field explicitly:

```yaml
build:
  gpu: true
  cuda: "11.8"
```

You can specify a minor version (`11.8`) or a patch version (`11.8.0`).

Only override CUDA when you have a specific reason to. Letting Cog auto-detect avoids version mismatches between CUDA, cuDNN, and your ML framework.

## Test locally with GPU

When you use `cog predict` or `cog run`, Cog automatically passes the `--gpus=all` flag to Docker. No extra configuration is needed:

```console
cog predict -i prompt="hello world"
```

If you want to specify a particular GPU device:

```console
cog predict --gpus='"device=0"' -i prompt="hello world"
```

When running a built image directly with `docker run`, you must pass the GPU flag yourself:

```console
docker run -d -p 5001:5000 --gpus all my-model
```

## Troubleshoot common GPU issues

### CUDA version mismatch

If you see errors like `CUDA error: no kernel image is available for execution on the device`, your CUDA version does not match what your ML framework expects.

To fix this:

1. Remove the explicit `cuda` setting from `cog.yaml` and let Cog auto-detect.
2. If you must pin CUDA, check the compatibility matrix for your framework (e.g. [PyTorch's CUDA compatibility](https://pytorch.org/get-started/previous-versions/)).

### Out of memory errors

If you see `CUDA out of memory` errors:

- Reduce your batch size or input resolution.
- Use `torch.cuda.empty_cache()` between predictions if your model accumulates GPU memory.
- Check that your `setup()` method is not loading multiple copies of the model.

### GPU not detected inside the container

If `torch.cuda.is_available()` returns `False` inside the container:

1. Confirm `gpu: true` is set in `cog.yaml`.
2. Confirm the NVIDIA Container Toolkit is installed on your host: `nvidia-smi` should work.
3. If running `docker run` directly, confirm you passed `--gpus all`.

## Next steps

- See the [`cog.yaml` reference](../yaml.md) for full details on `gpu`, `cuda`, and other build options.
- See [How to debug build failures](debug-builds.md) if your GPU-enabled build is failing.
