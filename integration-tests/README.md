# Cog Integration Tests

This directory contains Go-based integration tests for the Cog CLI using the [testscript](https://pkg.go.dev/github.com/rogpeppe/go-internal/testscript) framework.

## Test Formats

Most integration tests use the txtar format (`.txtar` files in `tests/`), which provides a simple declarative way to define test scripts and fixtures.

However, some tests require capabilities that don't fit txtar's sequential execution model and are written as standard Go test functions instead:

| Test | Location | Why Go instead of txtar |
|------|----------|-------------------------|
| `TestConcurrentPredictions` | `concurrent/` | Requires parallel HTTP requests with precise timing coordination |
| `TestInteractiveTTY` | `pty/` | Requires bidirectional PTY interaction |
| `TestLogin*` | `login/` | Login requires interactive PTY input and mock HTTP servers |

## Quick Start

```bash
# Run all tests
make test-integration

# Run fast tests only (skip slow GPU/framework tests)
cd integration-tests && go test -short -v

# Run a specific test
cd integration-tests && go test -v -run TestIntegration/string_predictor

# Run with a custom cog binary
COG_BINARY=/path/to/cog make test-integration
```

## Directory Structure

```
integration-tests/
├── README.md           # This file
├── suite_test.go       # Main test runner (txtar tests)
├── harness/
│   └── harness.go      # Test harness with custom commands
├── tests/
│   └── *.txtar         # Test files (one per test case)
├── concurrent/
│   └── concurrent_test.go  # Concurrent request tests
├── pty/
│   └── pty_test.go     # Interactive TTY tests
└── .bin/
    └── cog             # Cached cog binary (auto-generated)
```

## Writing Tests

Tests are `.txtar` files in the `tests/` directory. Each file is a self-contained test with embedded fixtures.

### Editor Support

For syntax highlighting of `.txtar` files:

**VS Code:**
- [testscript](https://marketplace.visualstudio.com/items?itemName=twpayne.vscode-testscript) by twpayne - Syntax highlighting with embedded file support
- [txtar](https://github.com/brody715/vscode-txtar) by brody715 - Alternative txtar extension

Install via VS Code:
```
ext install twpayne.vscode-testscript
```

**Zed:**
- [zed-txtar](https://github.com/FollowTheProcess/zed-txtar) - Syntax highlighting for txtar files

Install via Zed extensions panel or add to your extensions.

**Vim/Neovim:**
- Use [tree-sitter-go-template](https://github.com/ngalaiko/tree-sitter-go-template) for basic support
- Or set filetype manually: `:set ft=conf` for basic highlighting

### Basic Test Structure

```txtar
# Comments describe what the test does
# This is a test for basic string prediction

# Build the Docker image
cog build -t $TEST_IMAGE

# Run a prediction
cog predict $TEST_IMAGE -i s=world
stdout 'hello world'

# Test that invalid input fails
! cog predict $TEST_IMAGE -i wrong=value
stderr 'Field required'

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

### Test File Format

- Lines starting with `#` are comments
- Lines starting with `-- filename --` begin embedded files
- Commands are executed in order
- Use `!` prefix for commands expected to fail
- Use `stdout` and `stderr` to assert on command output

## Environment Variables

The harness automatically sets these environment variables:

| Variable | Description |
|----------|-------------|
| `$TEST_IMAGE` | Unique Docker image name for test isolation |
| `$WORK` | Test's temporary working directory |
| `$SERVER_URL` | URL of running cog server (after `cog serve`) |
| `$HOME` | Real home directory (for Docker credentials) |

You can also use:

| Variable | Description |
|----------|-------------|
| `COG_BINARY` | Path to cog binary (defaults to auto-build) |
| `TEST_PARALLEL` | Number of parallel tests (default: 4) |

Use `go test -short` to skip slow tests.

## Custom Commands

### `cog` - Run cog CLI commands

```txtar
cog build -t $TEST_IMAGE
cog predict $TEST_IMAGE -i name=value
! cog predict $TEST_IMAGE -i invalid=input  # Expected to fail
```

Special handling for `cog serve`:
- Runs in background automatically
- Allocates a random port
- Waits for health check before continuing
- Sets `$SERVER_URL` for subsequent commands
- Cleans up on test completion

### `curl` - Make HTTP requests to cog server

```txtar
cog serve
curl GET /health-check
curl POST /predictions '{"input":{"s":"hello"}}'
stdout '"output":"hello"'
```

Usage: `curl METHOD PATH [BODY]`

The `curl` command includes built-in retry logic (10 attempts, 500ms delay) for resilience against timing issues in integration tests.

### `wait-for` - Wait for conditions

**Note**: This command waits for conditions on the **host machine**, not inside Docker containers. For Docker-based tests, use `curl` instead (which has built-in retry logic).

```txtar
# Wait for a file to exist (host filesystem only)
wait-for file output.txt 30s

# Wait for HTTP endpoint (host-accessible URLs only)
wait-for http http://localhost:8080/health 200 60s

# Wait for file with content
wait-for not-empty results.json 30s
```

Usage: `wait-for CONDITION TARGET [ARGS] [TIMEOUT]`

## Conditions

Use conditions to control when tests run based on environment. Conditions are evaluated by the test runner and can be used with `skip` to conditionally skip tests.

### Available Conditions

| Condition | Evaluates to True When | Negated | Example Use Case |
|-----------|------------------------|---------|------------------|
| `[short]` | `go test -short` is used | `[!short]` | Use `[short] skip` to skip GPU tests, long builds, or slow framework installs when running in short mode |
| `[linux]` | Running on Linux | `[!linux]` | Tests requiring Linux-specific features |
| `[amd64]` | Running on amd64/x86_64 architecture | `[!amd64]` | Tests requiring specific CPU architecture |
| `[linux_amd64]` | Running on Linux AND amd64 | `[!linux_amd64]` | Tests requiring both Linux and amd64 (e.g., `--use-cog-base-image` builds) |

### Usage Examples

**Skip slow tests:**

```txtar
[short] skip 'requires GPU or long build time'

cog build -t $TEST_IMAGE
# ... rest of test
```

Skip slow tests with: `go test -short ./...`

**Platform-specific tests:**

```txtar
[!linux] skip 'requires Linux'

# Linux-specific test
cog build -t $TEST_IMAGE
```

**Architecture-specific tests:**

```txtar
[!amd64] skip 'requires amd64 architecture'

# amd64-specific test
cog build -t $TEST_IMAGE
```

**Combined platform and architecture:**

```txtar
[!linux_amd64] skip 'requires Linux on amd64'

# Test that requires both (e.g., --use-cog-base-image builds)
cog build -t $TEST_IMAGE --use-cog-base-image
```

### Condition Logic

Conditions can be negated with `!`:
- `[short]` - True when `go test -short` is used
  - Use `[short] skip` to skip a slow test when running in short mode
- `[!short]` - True when NOT running with `-short` flag
  - Use `[!short] skip` to only run a test in short mode (rare)
- `[!linux]` - True when NOT on Linux
  - Use `[!linux] skip` to skip non-Linux tests
- `[linux_amd64]` - True when on Linux AND amd64
  - Use `[!linux_amd64] skip` to skip tests that need this specific platform

Multiple conditions can be used on separate lines:

```txtar
[short] skip 'requires long build time'
[!linux] skip 'requires Linux'

# Only runs on Linux when not using -short flag
cog build -t $TEST_IMAGE
```

## Built-in Commands

These are provided by testscript itself:

| Command | Description |
|---------|-------------|
| `exec` | Run an arbitrary command |
| `stdout PATTERN` | Assert stdout matches regex |
| `stderr PATTERN` | Assert stderr matches regex |
| `exists FILE` | Assert file exists |
| `! exists FILE` | Assert file does not exist |
| `cp SRC DST` | Copy file |
| `rm FILE` | Remove file |
| `mkdir DIR` | Create directory |
| `cd DIR` | Change directory |
| `env KEY=VALUE` | Set environment variable |
| `skip MESSAGE` | Skip the test |
| `stop MESSAGE` | Stop test early (success) |

See [testscript documentation](https://pkg.go.dev/github.com/rogpeppe/go-internal/testscript) for the full list.

## Test Patterns

### Testing predictions

```txtar
cog build -t $TEST_IMAGE
cog predict $TEST_IMAGE -i s=hello
stdout 'hello'
```

### Testing server endpoints

```txtar
cog build -t $TEST_IMAGE
cog serve
curl POST /predictions '{"input":{"s":"test"}}'
stdout '"output":'
```

### Testing expected failures

```txtar
# Build should fail without predictor
! cog build -t $TEST_IMAGE
stderr 'predict'

-- cog.yaml --
build:
  python_version: "3.12"
# Note: no predict field
```

### Testing with subprocess initialization

```txtar
cog build -t $TEST_IMAGE
cog serve

# curl has built-in retry logic for timing resilience
curl POST /predictions '{"input":{"s":"test"}}'
stdout '"output":"hello test"'

-- predict.py --
import subprocess
from cog import BasePredictor

class Predictor(BasePredictor):
    def setup(self):
        self.bg = subprocess.Popen(["./background.sh"])
    
    def predict(self, s: str) -> str:
        return "hello " + s
```

### Slow tests (GPU/frameworks)

```txtar
[fast] skip 'requires long build time'

cog build -t $TEST_IMAGE
cog predict $TEST_IMAGE
stdout 'torch'

-- cog.yaml --
build:
  python_version: "3.12"
  gpu: true
  python_packages:
    - torch==2.7.0
```

## How It Works

1. **Test Discovery**: The test runner finds all `.txtar` files in `tests/`
2. **Setup**: For each test, the harness:
   - Creates a fresh temporary directory
   - Extracts embedded files from the txtar
   - Sets environment variables (`$TEST_IMAGE`, etc.)
   - Registers cleanup (Docker image removal, server shutdown)
3. **Execution**: Commands run sequentially in the temp directory
4. **Assertions**: `stdout`/`stderr` commands verify output
5. **Cleanup**: Docker images are removed, servers are stopped

## Debugging Failed Tests

### View verbose output

```bash
go test -v -run TestIntegration/test_name
```

### Keep work directory

```bash
# Add to test or set in harness
env TESTWORK=1
```

### Run single test interactively

```bash
cd integration-tests
go test -v -run TestIntegration/string_predictor -timeout 10m
```

### Check Docker images

```bash
# List test images (should be cleaned up)
docker images | grep cog-test
```

## Adding New Tests

1. Create a new `.txtar` file in `tests/`
2. Name it descriptively (e.g., `async_predictor.txtar`)
3. Add comments explaining what's being tested
4. Include all necessary fixture files inline
5. Run your test: `go test -v -run TestIntegration/your_test_name`

## Common Issues

### Test times out waiting for server

The server health check has a 30-second timeout. If your model takes longer to load:
- Consider if it should be a `[slow]` test
- Check for errors in the predictor's `setup()` method

### "SERVER_URL not set" error

Make sure `cog serve` is called before `curl`.

### Docker build output cluttering logs

Build output is suppressed by default (`BUILDKIT_PROGRESS=quiet`). Errors are still shown.

### Files created in container not visible

The `wait-for file` command checks the **host** filesystem, not inside Docker containers. Use `curl` for Docker-based synchronization (it has built-in retry logic).

### Test works locally but fails in CI

- CI environments may be slower - increase retry attempts
- Check for hardcoded paths or assumptions about the environment
- Make sure the test is properly isolated (no shared state)
