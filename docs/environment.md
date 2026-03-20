# Environment variables

This guide lists the environment variables that change how Cog functions.

## Build-time variables

### `COG_SDK_WHEEL`

Controls which cog Python SDK wheel is installed in the Docker image during `cog build`. Takes precedence over `build.sdk_version` in `cog.yaml`.

**Supported values:**

| Value                | Description                                          |
| -------------------- | ---------------------------------------------------- |
| `pypi`               | Install latest version from PyPI                     |
| `pypi:0.12.0`        | Install specific version from PyPI                   |
| `dist`               | Use wheel from `dist/` directory (requires git repo) |
| `https://...`        | Install from URL                                     |
| `/path/to/wheel.whl` | Install from local file path                         |

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

### `COG_SETUP_TIMEOUT`

Controls the maximum time (in seconds) allowed for the model's `setup()` method to complete. If setup exceeds this timeout, the server will report a setup failure.

By default, there is no timeout — setup runs indefinitely.

Set to `0` to disable the timeout (same as default). Invalid values are ignored with a warning.

```console
$ COG_SETUP_TIMEOUT=300 docker run -p 5000:5000 my-model  # 5-minute setup timeout
```

### `COG_CA_CERT`

Injects a custom CA certificate into the Docker image during `cog build`. This is useful when building behind a corporate proxy or VPN that uses custom certificate authorities (e.g. Cloudflare WARP).

**Supported values:**

| Value                            | Description                                                 |
| -------------------------------- | ----------------------------------------------------------- |
| `/path/to/cert.crt`              | Path to a single PEM certificate file                       |
| `/path/to/certs/`                | Directory of `.crt` and `.pem` files (all are concatenated) |
| `-----BEGIN CERTIFICATE-----...` | Inline PEM certificate                                      |
| `LS0tLS1CRUdJTi...`              | Base64-encoded PEM certificate                              |

The certificate is installed into the system CA store and the `SSL_CERT_FILE` and `REQUESTS_CA_BUNDLE` environment variables are set automatically in the built image.

**Examples:**

```console
# From a file
$ COG_CA_CERT=/usr/local/share/ca-certificates/corporate-ca.crt cog build

# From a directory of certs
$ COG_CA_CERT=/etc/custom-certs/ cog build

# Inline (e.g. from a CI secret)
$ COG_CA_CERT="$(cat /path/to/cert.pem)" cog build
```
