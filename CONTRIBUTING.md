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

## Development environment

You'll need to [install Go 1.16](https://golang.org/doc/install). If you're using a newer Mac with an M1 chip, be sure to download the `darwin-arm64` installer package. Alternatively you can run `brew install go` which will automatically detect and use the appropriate installer for your system architecture.

Once you have Go installed, then run:

    make install

This installs the `cog` binary to `$GOPATH/bin/cog`.

To run the tests:

    make test

The project is formatted by goimports. To format the source code, run:

    make fmt

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

## Publishing a release

This project has a [GitHub Actions workflow](https://github.com/replicate/cog/blob/39cfc5c44ab81832886c9139ee130296f1585b28/.github/workflows/ci.yaml#L107) that uses [goreleaser](https://goreleaser.com/quick-start/#quick-start) to facilitate the process of publishing new releases. The release process is triggered by manually creating and pushing a new git tag.

To publish a new release, run the following in your local checkout of cog:

    git checkout main
    git fetch --all --tags
    git tag v0.0.11
    git push --tags

Then visit [github.com/replicate/cog/actions](https://github.com/replicate/cog/actions) to monitor the release process.

## Troubleshooting

### `invalid command 'bdist_wheel'`

If you get this error running `make install`, you'll need to run `pip install wheel` first.

### `cog command not found`

The compiled `cog` binary will be installed in `$GOPATH/bin/cog`, e.g. `~/go/bin/cog`. Make sure that Golang's bin directory is present on your system PATH by adding it to your shell config (`.bashrc`, `.zshrc`, etc):

    export PATH=~/go/bin:$PATH

---

Still having trouble? Please [open an issue](https://github.com/replicate/cog/issues) on GitHub.
