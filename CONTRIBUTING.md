# Contributing guide

## Development environment

Development tasks are managed with [mise](https://mise.jdx.dev/). Run `mise tasks` to see all available tasks.

### Prerequisites

- [mise](https://mise.jdx.dev/getting-started.html): Manages Go, Rust, Python, and other tools
- [Docker](https://docs.docker.com/desktop) or [OrbStack](https://orbstack.dev)

### Setup

```sh
# Trust the mise configuration and install tools
mise trust
mise install

# Create Python virtualenv and install dependencies
uv venv
uv sync --all-groups
```

### Building

Cog is composed of three components that are built separately:

- **Python SDK** (`python/cog/`) — the Python library that model authors use. Built into a wheel that gets installed inside containers.
- **Coglet** (`crates/`) — a Rust prediction server that runs inside containers. Cross-compiled into a Linux wheel.
- **Cog CLI** (`cmd/cog/`, `pkg/`) — the Go command-line tool. Embeds the SDK wheel and picks up the coglet wheel from `dist/`.

```sh
# Build everything and install
mise run build:sdk                        # build the Python SDK wheel
mise run build:coglet:wheel:linux-x64     # cross-compile the coglet wheel for Linux containers
mise run build:cog                        # build the Go CLI (embeds SDK, picks up coglet from dist/)
sudo mise run install                     # symlink the binary to /usr/local/bin
```

After making changes, rebuild only the component you changed and then `build:cog`:

```sh
mise run build:sdk                        # after changing python/cog/
mise run build:coglet:wheel:linux-x64     # after changing crates/
mise run build:cog                        # after changing cmd/cog/ or pkg/, or to pick up new wheels
```

### Common tasks

```sh
# Run all tests
mise run test:go
mise run test:python
mise run test:rust

# Run specific tests
mise run test:go -- ./pkg/config
uv run tox -e py312-tests -- python/tests/server/test_http.py -k test_name

# Format code (all languages)
mise run fmt:fix

# Lint code (all languages)
mise run lint
```

Run `mise tasks` for the complete list of available tasks.

If you encounter any errors, see the troubleshooting section below.

## Project structure

As much as possible, this is attempting to follow the [Standard Go Project Layout](https://github.com/golang-standards/project-layout).

- `cmd/` - The root `cog` command.
- `pkg/cli/` - CLI commands.
- `pkg/config` - Everything `cog.yaml` related.
- `pkg/docker/` - Low-level interface for Docker commands.
- `pkg/dockerfile/` - Creates Dockerfiles.
- `pkg/image/` - Creates and manipulates Cog Docker images.
- `pkg/run/` - Runs predictions on models.
- `pkg/util/` - Various packages that aren't part of Cog. They could reasonably be separate re-usable projects.
- `python/` - The Cog Python library.
- `integration-tests/` - Go-based integration tests using testscript.
- `tools/compatgen/` - Tool for generating CUDA/PyTorch/TensorFlow compatibility matrices.

For deeper architectural understanding, see the [architecture documentation](./architecture/00-overview.md).

## Updating compatibility matrices

The CUDA base images and framework compatibility matrices in `pkg/config/` are checked into source control and only need to be regenerated when adding support for new versions of CUDA, PyTorch, or TensorFlow.

To regenerate the compatibility matrices, run:

```sh
# Regenerate all matrices
mise run generate:compat

# Or regenerate specific matrices
mise run generate:compat cuda
mise run generate:compat torch
mise run generate:compat tensorflow
```

The generated files are:
- `pkg/config/cuda_base_images.json` - Available NVIDIA CUDA base images
- `pkg/config/torch_compatibility_matrix.json` - PyTorch/CUDA/Python compatibility
- `pkg/config/tf_compatibility_matrix.json` - TensorFlow/CUDA/Python compatibility

## CI tool dependencies

Development tools are managed in **two places** that must be kept in sync:

1. **`mise.toml`** — Tool versions for local development (uses aqua backend for prebuilt binaries)
2. **`.github/workflows/ci.yaml`** — Tool installation for CI (uses dedicated GitHub Actions)

CI deliberately avoids aqua downloads from GitHub Releases to prevent transient 502 failures. Instead, it uses dedicated actions (`taiki-e/install-action`, `go install`, `PyO3/maturin-action`, etc.) that are more reliable.

Tools disabled in CI are listed in `MISE_DISABLE_TOOLS` in `ci.yaml`.

**When updating a tool version**, update both:
- The version in `mise.toml` (for local dev)
- The corresponding version pin in `.github/workflows/ci.yaml` (for CI)

See the [CI Tool Dependencies section in AGENTS.md](./AGENTS.md#ci-tool-dependencies) for the full mapping of tools to their CI installation methods.

## Concepts

There are a few concepts used throughout Cog that might be helpful to understand.

- **Config**: The `cog.yaml` file.
- **Image**: Represents a built Docker image that serves the Cog API, containing a **model**.
- **Input**: Input from a **prediction**, as key/value JSON object.
- **Model**: A user's machine learning model, consisting of code and weights.
- **Output**: Output from a **prediction**, as arbitrarily complex JSON object.
- **Prediction**: A single run of the model, that takes **input** and produces **output**.
- **Runner**: Defines how Cog runs **predictions** on a **model**.

## Running tests

**To run the entire test suite:**

```sh
mise run test:go
mise run test:python
mise run test:rust
```

**To run just the Go unit tests:**

```sh
mise run test:go
```

**To run just the Python tests:**

```sh
mise run test:python
```

> [!INFO]
> This runs the Python test suite across all supported Python versions (3.10-3.13) using tox.

### Integration Tests

Integration tests are in `integration-tests/` using [testscript](https://pkg.go.dev/github.com/rogpeppe/go-internal/testscript). Each test is a self-contained `.txtar` file in `integration-tests/tests/`, with some specialized tests as Go test functions in subpackages.

```sh
# Run all integration tests
mise run test:integration

# Run a specific test
mise run test:integration string_predictor

# Run fast tests only (skip slow GPU/framework tests)
cd integration-tests && go test -short -v

# Run with a custom cog binary
COG_BINARY=/path/to/cog mise run test:integration
```

### Writing Integration Tests

When adding new functionality, add integration tests in `integration-tests/tests/`. They are:
- Self-contained (embedded fixtures in `.txtar` files)
- Faster to run (parallel execution with automatic cleanup)
- Easier to read and write (simple command script format)

Example test structure:

```txtar
# Test string predictor
cog build -t $TEST_IMAGE
cog run $TEST_IMAGE -i s=world
stdout 'hello world'

-- cog.yaml --
build:
  python_version: "3.12"
run: "run.py:Runner"

-- run.py --
from cog import BaseRunner

class Runner(BaseRunner):
    def run(self, s: str) -> str:
        return "hello " + s
```

For testing `cog serve`, use `cog serve` and the `curl` command:

```txtar
cog build -t $TEST_IMAGE
cog serve
curl POST /predictions '{"input":{"s":"test"}}'
stdout '"output":"hello test"'
```

#### Advanced Test Commands

For tests that require subprocess initialization or async operations, use `retry-curl`:

**`retry-curl` - HTTP request with automatic retries:**

```txtar
# Make HTTP request with retry logic (useful for subprocess initialization delays)
# retry-curl [method] [path] [body] [max-attempts] [retry-delay]
retry-curl POST /predictions '{"input":{"s":"test"}}' 30 1s
stdout '"output":"hello test"'
```

**Example: Testing predictor with subprocess in setup**

```txtar
cog build -t $TEST_IMAGE
cog serve

# Use generous retries since setup spawns a background process
retry-curl POST /predictions '{"input":{"s":"test"}}' 30 1s
stdout '"output":"hello test"'

-- run.py --
class Runner(BaseRunner):
    def setup(self):
        self.process = subprocess.Popen(["./background.sh"])
    
    def run(self, s: str) -> str:
        return "hello " + s
```

#### Test Conditions

Use conditions to control when tests run based on environment:

**`[short]` - Skip slow tests in short mode:**

```txtar
[short] skip 'requires GPU or long build time'

cog build -t $TEST_IMAGE
# ... rest of test
```

Run with `go test -short` to skip these tests.

**`[linux]` / `[!linux]` - Platform-specific tests:**

```txtar
[!linux] skip 'requires Linux'

# Linux-specific test
cog build -t $TEST_IMAGE
```

**`[amd64]` / `[!amd64]` - Architecture-specific tests:**

```txtar
[!amd64] skip 'requires amd64 architecture'

# amd64-specific test
cog build -t $TEST_IMAGE
```

**`[linux_amd64]` - Combined platform and architecture:**

```txtar
[!linux_amd64] skip 'requires Linux on amd64'

# Test that requires both Linux and amd64
cog build -t $TEST_IMAGE
```

**Combining conditions:**

Conditions can be negated with `!`. Examples:
- `[short]` - True when `go test -short` is used (skip this test in short mode)
- `[!short]` - True when NOT running with `-short` flag (only run this in full test mode)
- `[!linux]` - True when NOT on Linux
- `[linux_amd64]` - True when on Linux AND amd64

See existing tests in `integration-tests/tests/`, especially `setup_subprocess_*.txtar`, for more examples.

## Running the docs server

To run the docs website server locally:

```sh
mise run docs:serve
```

## Publishing a release

Releases are managed by GitHub Actions workflows. See [`.github/workflows/README.md`](.github/workflows/README.md) for full details.

All packages use **lockstep versioning** from `crates/Cargo.toml`. There are three release types:

| Type | Example tag | Branch rule | PyPI/crates.io? |
|------|-------------|-------------|-----------------|
| **Stable** | `v0.17.0` | Must be on main | Yes |
| **Pre-release** | `v0.17.0-alpha3` | Must be on main | Yes |
| **Dev** | `v0.17.0-dev1` | Any branch | No |

### Stable / Pre-release

```bash
# 1. Update crates/Cargo.toml version (e.g. "0.17.0" or "0.17.0-alpha3")
# 2. Merge to main
# 3. Tag and push
git tag v0.17.0
git push origin v0.17.0
# 4. Wait for release-build.yaml to create a draft release
# 5. Review the draft in GitHub UI, then click "Publish release"
#    This triggers release-publish.yaml -> PyPI + crates.io
```

### Dev release

```bash
# From any branch:
# 1. Update crates/Cargo.toml version (e.g. "0.17.0-dev1")
# 2. Commit and push
# 3. Tag and push
git tag v0.17.0-dev1
git push origin v0.17.0-dev1
# 4. Done. Artifacts are built and published as a GH pre-release.
#    No PyPI/crates.io. No manual approval.
```


## Troubleshooting

### `cog command not found`

The compiled `cog` binary will be installed in `$GOPATH/bin/cog`, e.g. `~/go/bin/cog`. Make sure that Golang's bin directory is present on your system PATH by adding it to your shell config (`.bashrc`, `.zshrc`, etc):

    export PATH=~/go/bin:$PATH

---

Still having trouble? Please [open an issue](https://github.com/replicate/cog/issues) on GitHub.
