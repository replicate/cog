# Contributing guide

## Development environment

To install Replicate from source:

    make install

To run the tests:

    make test

The project is formatted by goimports. To format the source code, run:

    make fmt

## Project structure

As much as possible, this is attempting to follow the [Standard Go Project Layout](https://github.com/golang-standards/project-layout).

- `cmd/` - The root `cog` command.
- `end-to-end-test/` - High-level integration tests for Cog.
- `pkg/cli/` - CLI commands.
- `pkg/client/` - Client used by the CLI to communicate with the server.
- `pkg/database/` - Used by the server to store metadata about models.
- `pkg/docker/` - Various interfaces with Docker for building and running containers.
- `pkg/model/` - Versions, repos, and configs (`cog.yaml`).
- `pkg/server/` - Server for storing data and building Docker images.
- `pkg/serving/` - Runs inferences to test models.
- `pkg/settings/` - Manages `.cog` directory in repos and `.config/cog/` directory for user settings.
- `pkg/storage/` - Used by the server to store models.
- `pkg/util/` - Various packages that aren't part of Cog. They could reasonably be separate re-usable projects.
