# AGENTS.md

This file provides guidance to coding agents when working with code in this repository.

## Project Overview

Cog is a tool that packages machine learning models in production-ready containers. 

It consists of:
- **Cog CLI** (`cmd/cog/`) - Command-line interface for building, running, and deploying models, written in Go
- **Python SDK** (`python/cog/`) - Python library for defining model predictors and training in Python

Documentation for the CLI and SDK is available by reading ./docs/llms.txt.

## Development Commands

The main development commands are defined in `Makefile` and the `script/` directory. Here are the key commands:

- `script/setup` - Sets up the development environment (installs dependencies, sets up Python virtualenv)
- `script/format` - Formats Go and Python code
- `script/lint` - Runs linters for Go and Python code

## Code Style Guidelines

### Go
- **Imports**: Organize in three groups separated by blank lines: (1) Standard library, (2) Third-party packages, (3) Internal packages (`github.com/replicate/cog/pkg/...`)
- **Formatting**: Use `goimports` (via `script/format` or `make fmt`)
- **Linting**: Must pass golangci-lint with: errcheck, gocritic, gosec, govet, ineffassign, misspell, revive, staticcheck, unused
- **Error Handling**: Return errors as values; use `pkg/errors.CodedError` for user-facing errors with error codes
- **Naming**: CamelCase for exported, camelCase for unexported
- **Testing**: Use `testify/require` for assertions; prefer table-driven tests

Example import block:
```go
import (
    "fmt"
    
    "github.com/spf13/cobra"
    
    "github.com/replicate/cog/pkg/config"
)
```

### Python
- **Imports**: Automatically organized by ruff/isort (stdlib → third-party → local)
- **Formatting**: Use `ruff format` (via `script/format`)
- **Linting**: Must pass ruff checks: E (pycodestyle), F (Pyflakes), I (isort), W (warnings), S (bandit), B (bugbear), ANN (annotations)
- **Type Annotations**: Required on all function signatures; use `typing_extensions` for compatibility; avoid `Any` where possible
- **Error Handling**: Raise exceptions with descriptive messages; avoid generic exception catching
- **Naming**: snake_case for functions/variables/modules, PascalCase for classes
- **Testing**: Use pytest with fixtures; async tests with pytest-asyncio
- **Compatibility**: Must support Python 3.8-3.13 and both Pydantic 1.x and 2.x (test with tox)

## Working on the CLI and support tooling
The CLI code is in the `cmd/cog/` and `pkg/` directories. Support tooling is in the `tools/` directory. 

The main commands for working on the CLI are:
- `go run ./cmd/cog` - Runs the Cog CLI directly from source (requires wheel to be built first)
- `make cog` - Builds the Cog CLI binary, embedding the Python wheel
- `make install` - Builds and installs the Cog CLI binary to `/usr/local/bin`, or to a custom path with `make install PREFIX=/custom/path`
- `make test-go` - Runs all Go unit tests
- `go test ./pkg/...` - Runs tests directly with `go test`

## Working on the Python SDK
The Python SDK is developed in the `python/cog/` directory. It uses `uv` for virtual environments and `tox` for testing across multiple Python versions.

The main commands for working on the SDK are:
- `make wheel` - Rebuilds the Python wheel from the `python/` directory

### Testing
Go code is tested using the built-in `go test` framework:
- `go test ./pkg/... -run <name>` - Runs specific Go tests by name
- `script/test-go` - Runs all Go unit tests

Python code is tested using `tox`, which allows testing across multiple Python versions and configurations. The `tox.ini` file defines different environments for testing, such as `py313-pydantic2-tests` for Python 3.13 with Pydantic 2.

These commands are used to run Python tests:
- `script/test-python` - Runs all Python unit tests
- `uv run tox -e py312-pydantic2-tests -- python/tests/server/test_http.py::test_openapi_specification_with_yield` - Runs a specific Python test with a specific Pydantic version

The integration test suite in `test-integration/` tests the end-to-end functionality of the Cog CLI and Python SDK. It uses Python scripts to automate a pre-built Cog binary.
- `make test-integration` - Runs the integration tests, which require the Cog CLI binary to be built first. 
- `uv run tox -e integration -- --setup-show  test_integration/test_run.py::test_run_with_unattached_stdin` - Runs a specific integration test.

The integration tests require a built Cog binary, which defaults to the first `cog` in `PATH`. Run tests against a specific binary with the `COG_BINARY` environment variable. For example, to build cog and run integration tests:
```bash
make install PREFIX=./cog
COG_BINARY=./cog/cog make test-integration
```

### Development Workflow
1. Run `script/setup` for initial dev environment setup
2. Run `make wheel` to rebuild the Python wheel after making changes to the `./python` directory
3. Run `script/format` to format both go and python code
4. Run `script/lint` to check code quality
5. Read the `./docs` directory and make sure the documentation is up to date

## Architecture

### CLI Architecture (Go)
The CLI follows a command pattern with subcommands. The main components are:
- `pkg/cli/` - Command definitions (build, run, predict, serve, etc.)
- `pkg/docker/` - Docker client and container management
- `pkg/dockerfile/` - Dockerfile generation and templating
- `pkg/config/` - cog.yaml parsing and validation
- `pkg/image/` - Image building and pushing logic

### Python SDK Architecture
- `python/cog/` - Core SDK
  - `base_predictor.py` - Base class for model predictors
  - `types.py` - Input/output type definitions
  - `server/` - HTTP/queue server implementation
  - `command/` - Runner implementations for predict/train

### Key Design Patterns
1. **Embedded Python Wheel**: The Go binary embeds the Python wheel at build time (`pkg/dockerfile/embed/`)
2. **Docker SDK Integration**: Uses Docker Go SDK for container operations
3. **Type Safety**: Pydantic for Python type validation, strongly typed Go interfaces
4. **Compatibility Matrix**: Automated CUDA/PyTorch/TensorFlow compatibility management

## Common Tasks

### Adding a new CLI command
1. Create command file in `pkg/cli/`
2. Add command to `pkg/cli/root.go`
3. Implement business logic in appropriate `pkg/` subdirectory
4. Add tests

### Modifying Python SDK behavior
1. Edit files in `python/cog/`
2. Run `make wheel` to rebuild embedded wheel
3. Test with `make test-python`
4. Integration test with `make test-integration`

### Updating ML framework compatibility
1. See `tools/compatgen/` for compatibility matrix generation
2. Update framework versions in relevant Dockerfile templates
3. Test with various framework combinations

### Updating the docs
- Documentation is in the `docs/` directory, written in Markdown and generated into HTML using `mkdocs`.

## Important Files
- `cog.yaml` - User-facing model configuration
- `pkg/config/config.go` - Go code for parsing and validating `cog.yaml`
- `pkg/config/data/config_schema_v1.0.json` - JSON schema for `cog.yaml`
- `python/cog/base_predictor.py` - Predictor interface

## Testing Philosophy
- Unit tests for individual components (Go and Python)
- Integration tests for end-to-end workflows
- Tests use real Docker operations (no mocking Docker API)
- Always run `make wheel` after making Python changes before testing Go code
- Both Pydantic 1.x and 2.x must pass tests (use appropriate tox environments)
- Python 3.8-3.13 compatibility is required
