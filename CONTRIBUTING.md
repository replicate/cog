# Contributing guide

## Making a contribution

### Signing your work

Each commit you contribute to Cog must be signed off (not to be confused with **[signing](https://git-scm.com/book/en/v2/Git-Tools-Signing-Your-Work)**). It certifies that you wrote the patch, or have the right to contribute it. It is called the [Developer Certificate of Origin](https://developercertificate.org/) and was originally developed for the Linux kernel.

If you can certify the following:

```
By making a contribution to this project, I certify that:

(a) The contribution was created in whole or in part by me and I
    have the right to submit it under the open source license
    indicated in the file; or

(b) The contribution is based upon previous work that, to the best
    of my knowledge, is covered under an appropriate open source
    license and I have the right under that license to submit that
    work with modifications, whether created in whole or in part
    by me, under the same open source license (unless I am
    permitted to submit under a different license), as indicated
    in the file; or

(c) The contribution was provided directly to me by some other
    person who certified (a), (b) or (c) and I have not modified
    it.

(d) I understand and agree that this project and the contribution
    are public and that a record of the contribution (including all
    personal information I submit with it, including my sign-off) is
    maintained indefinitely and may be redistributed consistent with
    this project or the open source license(s) involved.
```

Then add this line to each of your Git commit messages, with your name and email:

```
Signed-off-by: Sam Smith <sam.smith@example.com>
```

### How to sign off your commits

If you're using the `git` CLI, you can sign a commit by passing the `-s` option: `git commit -s -m "Reticulate splines"`

You can also create a git hook which will sign off all your commits automatically. Using hooks also allows you to sign off commits when using non-command-line tools like GitHub Desktop or VS Code.

First, create the hook file and make it executable:

```sh
cd your/checkout/of/cog
touch .git/hooks/prepare-commit-msg
chmod +x .git/hooks/prepare-commit-msg
```

Then paste the following into the file:

```
#!/bin/sh

NAME=$(git config user.name)
EMAIL=$(git config user.email)

if [ -z "$NAME" ]; then
    echo "empty git config user.name"
    exit 1
fi

if [ -z "$EMAIL" ]; then
    echo "empty git config user.email"
    exit 1
fi

git interpret-trailers --if-exists doNothing --trailer \
    "Signed-off-by: $NAME <$EMAIL>" \
    --in-place "$1"
```

### Acknowledging contributions

We welcome contributions from everyone, and consider all forms of contribution equally valuable. This includes code, bug reports, feature requests, and documentation. We use [All Contributors](https://allcontributors.org/) to maintain a list of all the people who have contributed to Cog.

To acknowledge a contribution, add a comment to an issue or pull request in the following format:

```
@allcontributors please add @username for doc,code,ideas
```

A bot will automatically open a pull request to add the contributor to the project README.

Common contribution types include: `doc`, `code`, `bug`, and `ideas`. See the full list at [allcontributors.org/docs/en/emoji-key](https://allcontributors.org/docs/en/emoji-key)

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
- `pkg/predict/` - Runs predictions on models.
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
- **Predictor**: Defines how Cog runs **predictions** on a **model**.

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
cog predict $TEST_IMAGE -i s=world
stdout 'hello world'

-- cog.yaml --
build:
  python_version: "3.12"
predict: "predict.py:Predictor"

-- predict.py --
from cog import BasePredictor

class Predictor(BasePredictor):
    def predict(self, s: str) -> str:
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

-- predict.py --
class Predictor(BasePredictor):
    def setup(self):
        self.process = subprocess.Popen(["./background.sh"])
    
    def predict(self, s: str) -> str:
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

This project has a [GitHub Actions workflow](https://github.com/replicate/cog/blob/39cfc5c44ab81832886c9139ee130296f1585b28/.github/workflows/ci.yaml#L107) that uses [goreleaser](https://goreleaser.com/quick-start/#quick-start) to facilitate the process of publishing new releases. The release process is triggered by manually creating and pushing a new annotated git tag.

### Choose a version number

> Deciding what the annotated git tag should be requires some interpretation. Cog generally follows [SemVer 2.0.0](https://semver.org/spec/v2.0.0.html), and since the major version
> is `0`, the rules get [a bit more loose](https://semver.org/spec/v2.0.0.html#spec-item-4). Broadly speaking, the rules for when to increment the patch version still hold, but
> backward-incompatible changes **will not** require incrementing the major version. In this way, the minor version may be incremented whether the changes are additive or
> subtractive. This all changes once the major version is incremented to `1`.

### Set up GPG signing (macOS)

Before creating a signed tag, you'll need to set up GPG signing. On macOS, install GPG using Homebrew:

```bash
brew install gnupg
```

Generate a GPG key for signing:

```bash
gpg --quick-generate-key "Your Name <your.email@example.com>" ed25519 default 0
```

Configure Git to use your GPG key:

```bash
# Get your key ID
gpg --list-secret-keys --keyid-format=long
```

This will show output like:
```
sec   ed25519/ABC123DEF456 2024-01-15 [SC]
      ABC123DEF4567890ABCDEF1234567890ABCDEF12
uid                 [ultimate] Your Name <your.email@example.com>
```

The key ID is the part after `ed25519/` (in this example, `ABC123DEF456`).

```bash
# Configure Git (replace YOUR_KEY_ID with your actual key ID)
git config --global user.signingkey YOUR_KEY_ID
git config --global commit.gpgsign true
```
### Create a prerelease (optional)

Prereleases are a useful way to give testers a way to try out new versions of Cog without affecting the documented `latest` download URL which people normally use to install Cog.

To publish a prerelease version, append a [SemVer prerelease identifer](https://semver.org/#spec-item-9) like `-alpha` or `-beta` to the git tag name. Goreleaser will detect this and mark it as a prerelease in GitHub Releases.

    git checkout some-prerelease-branch
    git fetch --all --tags
    git tag -a v0.1.0-alpha -m "Prerelease v0.1.0"
    git push --tags

### Create a release

Run these commands to publish a new release `v0.13.12` referencing commit `fabdadbead`:

    git checkout main
    git fetch --all --tags
    git tag --sign --annotate --message 'Release v0.13.12' v0.13.12 fabdadbead
    git push origin v0.13.12

Then visit [github.com/replicate/cog/actions](https://github.com/replicate/cog/actions) to monitor the release process.


### Get team approval for the PyPI package

The release workflow will halt until another member of the team approves the release.

Ping someone on the team to review and approve the release.

### Convert your git tag to a GitHub release

Once the release is published, convert your git tag to a proper GitHub release:

1. Visit [github.com/replicate/cog/tags](https://github.com/replicate/cog/tags)
2. Click on your tag
3. Click "Create release from tag"
4. Add release notes and publish the release


## Troubleshooting

### `cog command not found`

The compiled `cog` binary will be installed in `$GOPATH/bin/cog`, e.g. `~/go/bin/cog`. Make sure that Golang's bin directory is present on your system PATH by adding it to your shell config (`.bashrc`, `.zshrc`, etc):

    export PATH=~/go/bin:$PATH

---

Still having trouble? Please [open an issue](https://github.com/replicate/cog/issues) on GitHub.
