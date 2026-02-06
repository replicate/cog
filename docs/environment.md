# Environment variables

This guide lists the environment variables that change how Cog functions.

## Build-time variables

### `COG_WHEEL`

Controls which cog Python SDK wheel is installed in the Docker image during `cog build`.

**Supported values:**

| Value | Description |
|-------|-------------|
| `pypi` | Install latest version from PyPI |
| `pypi:0.12.0` | Install specific version from PyPI |
| `dist` | Use wheel from `dist/` directory (requires git repo) |
| `https://...` | Install from URL |
| `/path/to/wheel.whl` | Install from local file path |

**Default behavior:**

- **Release builds**: Installs matching version from PyPI (e.g., cog CLI v0.12.0 installs cog==0.12.0)
- **Development builds**: Auto-detects wheel in `dist/` directory, falls back to latest PyPI

**Examples:**

```console
# Use specific PyPI version
$ COG_WHEEL=pypi:0.11.0 cog build

# Use local development wheel
$ COG_WHEEL=dist cog build

# Use wheel from URL
$ COG_WHEEL=https://example.com/cog-0.12.0-py3-none-any.whl cog build
```

The `dist` option searches for wheels in:
1. `./dist/` (current directory)
2. `$REPO_ROOT/dist/` (if REPO_ROOT is set)
3. `<git-repo-root>/dist/` (via `git rev-parse`, useful when running from subdirectories)

### `COGLET_WHEEL`

Controls which coglet wheel is installed in the Docker image. Coglet is the experimental Rust-based prediction server that provides faster, more stable request handling.

**Supported values:** Same as `COG_WHEEL`

**Default behavior:** Coglet is **not installed** unless explicitly enabled via this variable.

**Examples:**

```console
# Use coglet from PyPI
$ COGLET_WHEEL=pypi cog build

# Use local development wheel
$ COGLET_WHEEL=dist cog build

# Use specific version
$ COGLET_WHEEL=pypi:0.1.0 cog build
```

When coglet is installed, it is used automatically at runtime - no additional configuration is needed.

## Runtime variables

### `COG_NO_UPDATE_CHECK`

By default, Cog automatically checks for updates 
and notifies you if there is a new version available.

To disable this behavior, 
set the `COG_NO_UPDATE_CHECK` environment variable to any value.

```console
$ COG_NO_UPDATE_CHECK=1 cog build  # runs without automatic update check
```
