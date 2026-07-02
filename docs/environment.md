# Environment variables

This reference lists the public Cog-specific environment variables that change how Cog behaves.

## Build-time variables

### `COG_SDK_WHEEL`

Controls which Cog Python SDK wheel is installed in the Docker image during `cog build`. Takes precedence over `build.sdk_version` in `cog.yaml`.

**Supported values:**

| Value                | Description                                          |
| -------------------- | ---------------------------------------------------- |
| `pypi`               | Install latest version from PyPI                     |
| `pypi:0.12.0`        | Install specific version from PyPI                   |
| `dist`               | Use wheel from `dist/` directory (requires git repo) |
| `https://...`        | Install from URL                                     |
| `/path/to/wheel.whl` | Install from local file path                         |

**Default behaviour:**

- Release builds install the latest Cog SDK from PyPI.
- Development builds auto-detect a wheel in `dist/`, then fall back to the latest Cog SDK from PyPI.

```console
$ COG_SDK_WHEEL=pypi:0.11.0 cog build
$ COG_SDK_WHEEL=dist cog build
$ COG_SDK_WHEEL=https://example.com/cog-0.12.0-py3-none-any.whl cog build
```

The `dist` option searches for wheels in:

1. `./dist/` (current directory)
2. `$REPO_ROOT/dist/` (if `REPO_ROOT` is set)
3. `<git-repo-root>/dist/` (via `git rev-parse`, useful when running from subdirectories)

### `COGLET_WHEEL`

Controls which coglet wheel is installed in the Docker image. Coglet is the Rust-based inference server.

**Supported values:** Same as `COG_SDK_WHEEL`.

**Default behaviour:** For development builds, auto-detects a wheel in `dist/`. For release builds, installs the latest version from PyPI.

```console
$ COGLET_WHEEL=dist cog build
$ COGLET_WHEEL=pypi:0.1.0 cog build
```

### `COG_CA_CERT`

Injects a custom CA certificate into the Docker image during `cog build`. This is useful when building behind a corporate proxy or VPN that uses custom certificate authorities (for example, Cloudflare WARP).

**Supported values:**

| Value                            | Description                                                 |
| -------------------------------- | ----------------------------------------------------------- |
| `/path/to/cert.crt`              | Path to a single PEM certificate file                       |
| `/path/to/certs/`                | Directory of `.crt` and `.pem` files (all are concatenated) |
| `-----BEGIN CERTIFICATE-----...` | Inline PEM certificate                                      |
| `LS0tLS1CRUdJTi...`              | Base64-encoded PEM certificate                              |

The certificate is installed into the system CA store and the `SSL_CERT_FILE` and `REQUESTS_CA_BUNDLE` environment variables are set automatically in the built image.

```console
$ COG_CA_CERT=/usr/local/share/ca-certificates/corporate-ca.crt cog build
$ COG_CA_CERT=/etc/custom-certs/ cog build
$ COG_CA_CERT="$(cat /path/to/cert.pem)" cog build
```

### `COG_OPENAPI_SCHEMA`

Uses a pre-built OpenAPI schema instead of generating one from the configured predict or train reference.

The value must be a path to a JSON schema file. Cog reads that file during schema generation and embeds it in the built image.

```console
$ COG_OPENAPI_SCHEMA=./openapi.json cog build
```

## CLI and local cache variables

### `COG_NO_UPDATE_CHECK`

Disables Cog's automatic update check. Set it to any non-empty value.

```console
$ COG_NO_UPDATE_CHECK=1 cog build
```

### `COG_NO_COLOR`

Disables coloured CLI output. Set it to any non-empty value.

Cog also honours the standard `NO_COLOR` environment variable.

```console
$ COG_NO_COLOR=1 cog predict -i prompt="hello"
```

### `COG_SKIP_DOCKER_CHECK`

Skips the `cog doctor` Docker environment check. Set it to any non-empty value.

```console
$ COG_SKIP_DOCKER_CHECK=1 cog doctor
```

### `COG_CACHE_DIR`

Overrides Cog's local cache root.

Cog currently uses this cache for the content-addressed weights store. If unset, Cog uses `$XDG_CACHE_HOME/cog` when `XDG_CACHE_HOME` is set, otherwise `$HOME/.cache/cog`.

```console
$ COG_CACHE_DIR=/mnt/fast-cache cog weights pull
```

## Model reference and registry variables

### `COG_MODEL`

Overrides the full model reference used by commands that need a model destination, such as `cog push` and weights commands.

The value is parsed as a complete model reference (`registry/repo`, `registry/repo:tag`, or `registry/repo@digest`). If no tag is supplied, Cog generates a timestamp tag.

When `COG_MODEL` is set, it takes precedence over `COG_MODEL_REGISTRY`, `COG_MODEL_REPO`, and `COG_MODEL_TAG`.

```console
$ COG_MODEL=r8.im/acme/my-model:v1 cog push
```

### `COG_MODEL_REGISTRY`

Overrides only the registry host of the model reference.

```console
$ COG_MODEL_REGISTRY=registry.example.com cog push
```

### `COG_MODEL_REPO`

Overrides only the repository path of the model reference. The value must not include a registry host, tag, or digest.

```console
$ COG_MODEL_REPO=acme/my-model cog push
```

### `COG_MODEL_TAG`

Overrides only the tag of the model reference.

Tags starting with `cog-` are reserved for tags that Cog generates internally and are rejected.

```console
$ COG_MODEL_TAG=staging cog push
```

### `COG_REGISTRY_HOST`

Changes the default Replicate-compatible registry host used by commands such as `cog login`, base image resolution, and model reference resolution.

The default is `r8.im`.

```console
$ COG_REGISTRY_HOST=registry.example.com cog login
```

## Runtime server variables

These variables affect a running model server. Set them in `cog.yaml` under `environment`, pass them with `cog predict -e` or `cog serve -e`, or set them when running the built Docker image.

### `COG_MAX_CONCURRENCY`

Controls how many predictions the model server can run concurrently. This overrides both `@cog.concurrent(max=N)` and the deprecated `concurrency.max` field in `cog.yaml`.

By default, Cog runs one prediction at a time unless the model uses `@cog.concurrent(max=N)`. Invalid values are ignored and the default of `1` is used.

Values greater than `1` require an async `run()` method. This applies even when `COG_MAX_CONCURRENCY` is set as a runtime operator override.

Concurrency is resolved in this order, from highest to lowest precedence:

1. `COG_MAX_CONCURRENCY` set at runtime
2. Deprecated `concurrency.max` in `cog.yaml`, which is baked into the image as `COG_MAX_CONCURRENCY`
3. `@cog.concurrent(max=N)` on the async `run()` method
4. Default: `1`

```console
$ COG_MAX_CONCURRENCY=4 docker run -p 5000:5000 my-model
```

### `COG_SETUP_TIMEOUT`

Controls the maximum time, in seconds, allowed for the model's `setup()` method to complete. If setup exceeds this timeout, the server reports setup failure.

By default, there is no timeout. Set to `0` to disable the timeout. Invalid values are ignored with a warning.

```console
$ COG_SETUP_TIMEOUT=300 docker run -p 5000:5000 my-model
```

### `COG_LOG_LEVEL`

Controls Coglet runtime log verbosity when `RUST_LOG` is not set.

Supported values are `debug`, `info`, `warn`, `warning`, and `error`. The default is `info`.

```console
$ COG_LOG_LEVEL=debug docker run -p 5000:5000 my-model
```

### `COG_THROTTLE_RESPONSE_INTERVAL`

Controls how often asynchronous webhook `output` and `logs` events are sent, in seconds.

The default is `0.5` seconds. Invalid values are ignored and the default is used. `start` and `completed` webhook events are always sent immediately.

```console
$ COG_THROTTLE_RESPONSE_INTERVAL=1 docker run -p 5000:5000 my-model
```

### `COG_STREAM_HISTORY_CAPACITY`

Controls how many server-sent event stream events are retained per prediction for replay when a client reconnects with `Accept: text/event-stream`.

By default, Cog retains the most recent 1024 events per prediction. Set to `0` to disable replay history while keeping live streaming enabled. Invalid values are ignored with a warning and the default is used.

```console
$ COG_STREAM_HISTORY_CAPACITY=0 docker run -p 5000:5000 my-model
$ COG_STREAM_HISTORY_CAPACITY=4096 docker run -p 5000:5000 my-model
```

### `COG_WEIGHTS`

Provides a weights path or URL to a model whose `setup()` method accepts a `weights` parameter.

```console
$ cog run -e COG_WEIGHTS=https://example.com/weights.tar -i prompt="hello"
```

### `COG_USER_AGENT`

Sets the `User-Agent` header used by Cog when downloading URL-backed `File` inputs.

```console
$ COG_USER_AGENT="my-service/1.0" docker run -p 5000:5000 my-model
```

## Push tuning variables

### `COG_PUSH_OCI`

Enables Cog's OCI chunked push path for container image layers when set to `1`. If the OCI push fails with a non-fatal error, Cog falls back to Docker's native push path.

```console
$ COG_PUSH_OCI=1 cog push
```

### `COG_PUSH_CONCURRENCY`

Controls how many image layers or weight blobs Cog uploads concurrently during push operations.

The default is `5`. Invalid values and values less than `1` are ignored.

```console
$ COG_PUSH_CONCURRENCY=2 cog push
```

### `COG_PUSH_DEFAULT_CHUNK_SIZE`

Sets the default multipart upload chunk size, in bytes, when the registry does not advertise a maximum chunk size.

The default is 96 MiB. Invalid values and values less than `1` are ignored.

```console
$ COG_PUSH_DEFAULT_CHUNK_SIZE=67108864 cog push
```

### `COG_PUSH_MULTIPART_THRESHOLD`

Sets the minimum blob size, in bytes, before Cog uses multipart upload.

The default is 128 MiB. Invalid values and values less than `1` are ignored.

```console
$ COG_PUSH_MULTIPART_THRESHOLD=268435456 cog push
```
