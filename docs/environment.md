# Environment variables

This guide lists the environment variables that change how Cog functions.

## Build-time variables

### `COG_SDK_WHEEL`

Controls which cog Python SDK wheel is installed in the Docker image during `cog build`. Takes precedence over `build.sdk_version` in `cog.yaml`.

**Supported values:**

| Value | Description |
|-------|-------------|
| `pypi` | Install latest version from PyPI |
| `pypi:0.12.0` | Install specific version from PyPI |
| `dist` | Use wheel from `dist/` directory (requires git repo) |
| `https://...` | Install from URL |
| `/path/to/wheel.whl` | Install from local file path |

**Default behavior:**

- **Release builds**: Installs latest cog from PyPI
- **Development builds**: Auto-detects wheel in `dist/` directory, falls back to latest PyPI

**Examples:**

```console
# Use specific PyPI version
$ COG_SDK_WHEEL=pypi:0.11.0 cog build

# Use local development wheel
$ COG_SDK_WHEEL=dist cog build

# Use wheel from URL
$ COG_SDK_WHEEL=https://example.com/cog-0.12.0-py3-none-any.whl cog build
```

The `dist` option searches for wheels in:
1. `./dist/` (current directory)
2. `$REPO_ROOT/dist/` (if REPO_ROOT is set)
3. `<git-repo-root>/dist/` (via `git rev-parse`, useful when running from subdirectories)

### `COGLET_WHEEL`

Controls which coglet wheel is installed in the Docker image. Coglet is the Rust-based prediction server.

**Supported values:** Same as `COG_SDK_WHEEL`

**Default behavior:** For development builds, auto-detects a wheel in `dist/`. For release builds, installs the latest version from PyPI. Can be overridden with an explicit value.

**Examples:**

```console
# Use local development wheel
$ COGLET_WHEEL=dist cog build

# Use specific version from PyPI
$ COGLET_WHEEL=pypi:0.1.0 cog build
```

## Runtime variables

### `COG_NO_UPDATE_CHECK`

By default, Cog automatically checks for updates 
and notifies you if there is a new version available.

To disable this behavior, 
set the `COG_NO_UPDATE_CHECK` environment variable to any value.

```console
$ COG_NO_UPDATE_CHECK=1 cog build  # runs without automatic update check
```
