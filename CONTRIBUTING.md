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

We use the ["scripts to rule them all"](https://github.blog/engineering/engineering-principles/scripts-to-rule-them-all/) philosophy to manage common tasks across the project. These are mostly backed by a Makefile that contains the implementation.

You'll need the following dependencies installed to build Cog locally:
- [Go](https://golang.org/doc/install): We're targeting 1.24, but you can install the latest version since Go is backwards compatible. If you're using a newer Mac with an M1 chip, be sure to download the `darwin-arm64` installer package. Alternatively you can run `brew install go` which will automatically detect and use the appropriate installer for your system architecture.
- [uv](https://docs.astral.sh/uv/): Python versions and dependencies are managed by uv.
- [Docker](https://docs.docker.com/desktop) or [OrbStack](https://orbstack.dev)

Install the Python dependencies:

    script/setup

Once you have Go installed you can install the cog binary by running:

    make install PREFIX=$(go env GOPATH)

This installs the `cog` binary to `$GOPATH/bin/cog`.

To run ALL the tests:

    script/test-all

To run per-language tests (forwards arguments to test runner):

    script/test-python --no-cov python/tests/cog/test_files.py -k test_put_file_to_signed_endpoint_with_location

    script/test-go ./pkg/config

The project is formatted by goimports and ruff. To format the source code, run:

    script/format

To run code linting across all files:

    script/lint

For more information check the Makefile targets for more specific commands.

If you encounter any errors, see the troubleshooting section below?

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
- `test-integration/` - High-level integration tests for Cog.

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
script/test # see also: make test
```

**To run just the Golang tests:**

```sh
script/test-go # see also: make test-go
```

**To run just the Python tests:**

```sh
script/test-python # see also: make test-python
```

> [!INFO]
> Note that this will run the Python test suite using only the current version of Python defined in .python-version. To run a more comprehensive Python test suite then use `make test-python`.

**To run just the integration tests:**

```sh
make test-integration
```

**To run a specific Python test:**

```sh
script/test-python python/tests/server/test_http.py::test_openapi_specification_with_yield
```

**To run a specific Python test under a specific environment**

```sh
uv run tox -e py312-pydantic2-tests -- python/tests/server/test_http.py::test_openapi_specification_with_yield
```

_You can see all the available test environments under `env_list` in the tox.ini file_

**To stand up a server for one of the integration tests:**

```sh
make install
pip install -r requirements-dev.txt
make test
cd test-integration/test_integration/fixtures/file-project
cog build
docker run -p 5001:5000 --init --platform=linux/amd64 cog-file-project
```

Then visit [localhost:5001](http://localhost:5001) in your browser.

## Running the docs server

To run the docs website server locally:

```sh
make run-docs-server
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
