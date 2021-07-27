# Contributing guide

## Making a contribution

### Signing your work

Each commit you contribute to Cog must be signed off. It certifies that you wrote the patch, or have the right to contribute it. It is called the [Developer Certificate of Origin](https://developercertificate.org/) and was originally developed for the Linux kernel.

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

You can sign your commit automatically by passing the `-s` option to Git commit: `git commit -s -m "Reticulate splines"`

## Development environment

You'll need Go 1.16, then run:

    make install

This installs the `cog` binary to `$GOPATH/bin/cog`.

To run the tests:

    make test

The project is formatted by goimports. To format the source code, run:

    make fmt

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
