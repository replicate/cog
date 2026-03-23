# How to debug build failures

This guide shows you how to diagnose and fix common issues when `cog build` fails.

## Read the full build output

By default, Cog uses a compact build output format. To see the full output from every build step, use `--progress=plain`:

```console
cog build --progress=plain -t my-model
```

This shows the complete output from each Docker build step, including pip install logs, compilation output, and error messages that the default view may truncate.

## Inspect the build environment

If the build succeeds but the model does not work as expected, use `cog run` to open a shell inside the built environment:

```console
cog run bash
```

This drops you into an interactive shell with the same packages, system libraries, and Python version as the built image. You can inspect installed packages, test imports, and check file paths:

```console
# Inside the container
python -c "import torch; print(torch.__version__)"
pip list
ls /src
```

If you want to run a specific script:

```console
cog run python debug_script.py
```

## Bypass stale cache layers

If a build fails in a way that does not make sense (for example, a package that should be available is missing), a stale cache layer may be the cause. Force a clean rebuild:

```console
cog build --no-cache -t my-model
```

This is also useful when an external dependency has been updated but Docker is reusing a cached layer that installed the old version.

## Fix common errors

### Python version conflicts

**Symptom**: A package fails to install with a message like `requires Python >=3.11` or `no matching distribution found`.

**Fix**: Check that your `python_version` in `cog.yaml` is compatible with all your dependencies. Some packages only support certain Python versions:

```yaml
build:
  python_version: "3.12"
```

Cog supports Python 3.10, 3.11, 3.12, and 3.13. If you are unsure which version a package requires, check its PyPI page.

### Missing system packages

**Symptom**: A Python package fails to compile with errors like `fatal error: xyz.h: No such file or directory` or `ModuleNotFoundError` at runtime for packages that depend on shared libraries.

**Fix**: Add the required system package to `system_packages` in `cog.yaml`. Common examples:

```yaml
build:
  system_packages:
    - "libgl1"          # for OpenCV
    - "libglib2.0-0"    # for OpenCV
    - "ffmpeg"           # for audio/video processing
    - "libsndfile1"      # for audio libraries
    - "git"              # for pip packages installed from git
```

To find which package provides a missing header file, use `cog run` to search:

```console
cog run bash -c "apt-get update && apt-file search missing_header.h"
```

If `apt-file` is not available, install it first:

```console
cog run bash -c "apt-get update && apt-get install -y apt-file && apt-file update && apt-file search missing_header.h"
```

### CUDA version mismatches

**Symptom**: The build succeeds but the model fails at runtime with errors like `CUDA error: no kernel image is available` or `undefined symbol`.

**Fix**: In most cases, remove any explicit `cuda` setting from `cog.yaml` and let Cog auto-detect the correct version based on your PyTorch or TensorFlow version:

```yaml
build:
  gpu: true
  # Do NOT set cuda: unless you have a specific reason
```

If you must pin a CUDA version, verify it is compatible with your ML framework. See [How to use GPUs](gpu.md) for details.

### pip install timeouts or network errors

**Symptom**: `pip install` fails with connection timeout or SSL errors.

**Fix**: If you are behind a corporate proxy or VPN, set the `COG_CA_CERT` environment variable to inject your CA certificate:

```console
COG_CA_CERT=/path/to/corporate-ca.crt cog build -t my-model
```

If the issue is a slow network, increase pip's timeout using a `build.run` command:

```yaml
build:
  run:
    - pip config set global.timeout 120
```

### Private package installation

**Symptom**: `pip install` fails with `401 Unauthorized` or `403 Forbidden` when installing from a private index.

**Fix**: Use secret mounts in `build.run` to pass credentials without baking them into the image:

```yaml
build:
  run:
    - command: pip install -r requirements-private.txt
      mounts:
        - type: secret
          id: pip
          target: /etc/pip.conf
```

Then pass the secret at build time:

```console
cog build --secret id=pip,src=$HOME/.pip/pip.conf -t my-model
```

## Check Docker logs for runtime failures

If the image builds but the container crashes at startup, check the Docker logs:

```console
docker run -d --name debug-model -p 5001:5000 my-model
docker logs debug-model
```

If setup fails, the `/health-check` endpoint reports `SETUP_FAILED` with logs:

```console
curl http://localhost:5001/health-check | python3 -m json.tool
```

## Enable debug output

For more verbose output from the Cog CLI itself:

```console
cog --debug build -t my-model
```

This shows additional information about Dockerfile generation, build arguments, and Docker API calls.

## Next steps

- See [How to use GPUs](gpu.md) for GPU-specific troubleshooting.
- See [How to optimise Docker image size](image-size.md) to address slow builds.
- See the [`cog.yaml` reference](../yaml.md) for all build configuration options.
- See the [environment variables reference](../environment.md) for build-time variables.
