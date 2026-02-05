# AGENTS.md

This file provides guidance to coding agents when working with code in this repository.

## Project Overview

Cog is a tool that packages machine learning models in production-ready containers. 

It consists of:
- **Cog CLI** (`cmd/cog/`) - Command-line interface for building, running, and deploying models, written in Go
- **Python SDK** (`python/cog/`) - Python library for defining model predictors and training in Python
- **Coglet** (`crates/`) - Rust-based prediction server that runs inside containers, with Python bindings via PyO3

Documentation for the CLI and SDK is available by reading ./docs/llms.txt.

## Development Commands

Development tasks are managed with [mise](https://mise.jdx.dev/). Run `mise tasks` to see all available tasks.

### Quick Reference

| Task | Description |
|------|-------------|
| `mise run fmt` | Check formatting (all languages) |
| `mise run fmt:fix` | Fix formatting (all languages) |
| `mise run lint` | Run linters (all languages) |
| `mise run lint:fix` | Fix lint issues (all languages) |
| `mise run test:go` | Run Go tests |
| `mise run test:rust` | Run Rust tests |
| `mise run test:python` | Run Python tests |
| `mise run test:integration` | Run integration tests |
| `mise run build:cog` | Build cog CLI binary |
| `mise run build:coglet` | Build coglet wheel (dev) |
| `mise run build:sdk` | Build SDK wheel |

### Task Naming Convention

Tasks follow a consistent naming pattern:

- **Language-based tasks** for fmt/lint/test/typecheck: `task:go`, `task:rust`, `task:python`
- **Component-based tasks** for build: `build:cog`, `build:coglet`, `build:sdk`
- **Check vs Fix**: `fmt` and `lint` default to check mode (non-destructive); use `:fix` suffix to auto-fix

### All Tasks by Category

**Format:**
- `mise run fmt` / `mise run fmt:check` - Check all (alias)
- `mise run fmt:fix` - Fix all
- `mise run fmt:go` / `mise run fmt:rust` / `mise run fmt:python` - Per-language

**Lint:**
- `mise run lint` / `mise run lint:check` - Check all (alias)
- `mise run lint:fix` - Fix all
- `mise run lint:go` / `mise run lint:rust` / `mise run lint:python` - Per-language
- `mise run lint:rust:deny` - Check Rust licenses/advisories

**Test:**
- `mise run test:go` - Go unit tests
- `mise run test:rust` - Rust unit tests
- `mise run test:python` - Python unit tests (via tox)
- `mise run test:coglet:python` - Coglet Python binding tests
- `mise run test:integration` - Integration tests

**Build:**
- `mise run build:cog` - Build cog CLI (development)
- `mise run build:cog:release` - Build cog CLI (release)
- `mise run build:coglet` - Build coglet wheel (dev install)
- `mise run build:coglet:wheel` - Build coglet wheel (native platform)
- `mise run build:coglet:wheel:linux-x64` - Build for Linux x86_64
- `mise run build:coglet:wheel:linux-arm64` - Build for Linux ARM64
- `mise run build:sdk` - Build SDK wheel

**Other:**
- `mise run typecheck` - Type check all languages
- `mise run generate` - Run code generation
- `mise run clean` - Clean all build artifacts
- `mise run docs` - Build documentation
- `mise run docs:serve` - Serve docs locally

## Code Style Guidelines

### Go
- **Imports**: Organize in three groups separated by blank lines: (1) Standard library, (2) Third-party packages, (3) Internal packages (`github.com/replicate/cog/pkg/...`)
- **Formatting**: Use `mise run fmt:go:fix`
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
- **Formatting**: Use `mise run fmt:python:fix`
- **Linting**: Must pass ruff checks: E (pycodestyle), F (Pyflakes), I (isort), W (warnings), S (bandit), B (bugbear), ANN (annotations)
- **Type Annotations**: Required on all function signatures; use `typing_extensions` for compatibility; avoid `Any` where possible
- **Error Handling**: Raise exceptions with descriptive messages; avoid generic exception catching
- **Naming**: snake_case for functions/variables/modules, PascalCase for classes
- **Testing**: Use pytest with fixtures; async tests with pytest-asyncio
- **Compatibility**: Must support Python 3.10-3.13

### Rust
- **Formatting**: Use `mise run fmt:rust:fix`
- **Linting**: Must pass `mise run lint:rust` (clippy)
- **Dependencies**: Audited with `cargo-deny` (see `crates/deny.toml`); run `mise run lint:rust:deny`
- **Error Handling**: Use `thiserror` for typed errors, `anyhow` for application errors
- **Naming**: snake_case for functions/variables, PascalCase for types
- **Testing**: Use `cargo test`; snapshot tests use `insta`
- **Async**: tokio runtime; async/await patterns

## Working on the CLI and support tooling
The CLI code is in the `cmd/cog/` and `pkg/` directories. Support tooling is in the `tools/` directory. 

The main commands for working on the CLI are:
- `go run ./cmd/cog` - Runs the Cog CLI directly from source (requires wheel to be built first)
- `mise run build:cog` - Builds the Cog CLI binary, embedding the Python wheel
- `make install` - Builds and installs the Cog CLI binary to `/usr/local/bin`, or to a custom path with `make install PREFIX=/custom/path`
- `mise run test:go` - Runs all Go unit tests
- `go test ./pkg/...` - Runs tests directly with `go test`

## Working on the Python SDK
The Python SDK is developed in the `python/cog/` directory. It uses `uv` for virtual environments and `tox` for testing across multiple Python versions.

The main commands for working on the SDK are:
- `mise run build:sdk` - Builds the Python wheel
- `mise run test:python` - Runs Python tests across all supported versions

## Working on Coglet (Rust)
Coglet is the Rust-based prediction server that runs inside Cog containers, handling HTTP requests, worker process management, and prediction execution.

The code is in the `crates/` directory:
- `crates/coglet/` - Core Rust library (HTTP server, worker orchestration, IPC)
- `crates/coglet-python/` - PyO3 bindings for Python predictor integration (requires Python 3.10+)

For detailed architecture documentation, see `crates/README.md` and `crates/coglet/README.md`.

The main commands for working on Coglet are:
- `mise run build:coglet` - Build and install coglet wheel for development
- `mise run test:rust` - Run Rust unit tests
- `mise run lint:rust` - Run clippy linter
- `mise run fmt:rust:fix` - Format code

### Testing
Go code is tested using the built-in `go test` framework:
- `go test ./pkg/... -run <name>` - Runs specific Go tests by name
- `mise run test:go` - Runs all Go unit tests

Python code is tested using `tox`, which allows testing across multiple Python versions and configurations:
- `mise run test:python` - Runs all Python unit tests
- `uv run tox -e py312-tests -- python/tests/server/test_http.py::test_openapi_specification_with_yield` - Runs a specific Python test

The integration test suite in `integration-tests/` tests the end-to-end functionality of the Cog CLI and Python SDK using Go's testscript framework:
- `mise run test:integration` - Runs the integration tests
- `mise run test:integration string_predictor` - Runs a specific integration test

The integration tests require a built Cog binary, which defaults to the first `cog` in `PATH`. Run tests against a specific binary with the `COG_BINARY` environment variable:
```bash
make install PREFIX=./cog
COG_BINARY=./cog/cog mise run test:integration
```

### Development Workflow
1. Run `mise install` to set up the development environment
2. Run `mise run build:sdk` after making changes to the `./python` directory
3. Run `mise run fmt:fix` to format code
4. Run `mise run lint` to check code quality
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

### Coglet Architecture (Rust)
The prediction server that runs inside Cog containers. Uses a two-process architecture: a parent process (HTTP server + orchestrator) and a worker subprocess (Python predictor execution).

See `crates/README.md` for detailed architecture documentation.

- `crates/coglet/` - Core Rust library (HTTP server, worker orchestration, IPC bridge)
- `crates/coglet-python/` - PyO3 bindings for Python predictor integration

### Key Design Patterns
1. **Embedded Python Wheel**: The Go binary embeds the Python wheel at build time (`pkg/dockerfile/embed/`)
2. **Docker SDK Integration**: Uses Docker Go SDK for container operations
3. **Type Safety**: Dataclasses for Python type validation, strongly typed Go interfaces
4. **Compatibility Matrix**: Automated CUDA/PyTorch/TensorFlow compatibility management

For comprehensive architecture documentation, see [`architecture/`](./architecture/00-overview.md).

## Common Tasks

### Adding a new CLI command
1. Create command file in `pkg/cli/`
2. Add command to `pkg/cli/root.go`
3. Implement business logic in appropriate `pkg/` subdirectory
4. Add tests

### Modifying Python SDK behavior
1. Edit files in `python/cog/`
2. Run `mise run build:sdk` to rebuild wheel
3. Test with `mise run test:python`
4. Integration test with `mise run test:integration`

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
- `crates/Cargo.toml` - Rust workspace configuration
- `crates/README.md` - Coglet architecture overview
- `mise.toml` - Task definitions for development workflow

## Testing Philosophy
- Unit tests for individual components (Go and Python)
- Integration tests for end-to-end workflows
- Tests use real Docker operations (no mocking Docker API)
- Always run `mise run build:sdk` after making Python changes before testing Go code
- Python 3.10-3.13 compatibility is required
