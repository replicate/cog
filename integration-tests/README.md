# Cog Integration Tests

This directory contains Go-based integration tests for the Cog CLI using the [testscript](https://pkg.go.dev/github.com/rogpeppe/go-internal/testscript) framework.

## Quick Start

```bash
# Run all tests
make test-integration-go

# Run fast tests only (skip slow GPU/framework tests)
COG_TEST_FAST=1 make test-integration-go

# Run a specific test
cd integration-tests && go test -v -run TestIntegration/string_predictor

# Run with a custom cog binary
COG_BINARY=/path/to/cog make test-integration-go
```

## Directory Structure

```
integration-tests/
├── README.md           # This file
├── suite_test.go       # Main test runner
├── harness/
│   └── harness.go      # Test harness with custom commands
├── tests/
│   └── *.txtar         # Test files (one per test case)
└── .bin/
    └── cog             # Cached cog binary (auto-generated)
```

## Writing Tests

Tests are `.txtar` files in the `tests/` directory. Each file is a self-contained test with embedded fixtures.

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
| `COG_TEST_FAST` | Set to `1` to skip slow tests |
| `TEST_PARALLEL` | Number of parallel tests (default: 4) |

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

### `retry-curl` - HTTP request with retries

Useful for tests where initialization may take time (e.g., subprocess tests).

```txtar
cog serve
retry-curl POST /predictions '{"input":{"s":"test"}}' 30 1s
stdout '"output":"hello test"'
```

Usage: `retry-curl METHOD PATH [BODY] [MAX_ATTEMPTS] [RETRY_DELAY]`

- `MAX_ATTEMPTS`: Number of retries (default: 10)
- `RETRY_DELAY`: Delay between retries (default: 1s)

### `wait-for` - Wait for conditions

**Note**: This command waits for conditions on the **host machine**, not inside Docker containers. For Docker-based tests, use `retry-curl` instead.

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

Use conditions to skip tests based on environment:

### `[slow]` - Mark slow tests

```txtar
[slow] skip 'requires GPU or long build time'

cog build -t $TEST_IMAGE
# ... rest of test
```

Skip slow tests with: `COG_TEST_FAST=1 make test-integration-go`

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

# Use generous retries for subprocess startup
retry-curl POST /predictions '{"input":{"s":"test"}}' 30 1s
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
[slow] skip 'requires long build time'

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

Make sure `cog serve` is called before `curl` or `retry-curl`.

### Docker build output cluttering logs

Build output is suppressed by default (`BUILDKIT_PROGRESS=quiet`). Errors are still shown.

### Files created in container not visible

The `wait-for file` command checks the **host** filesystem, not inside Docker containers. Use `retry-curl` for Docker-based synchronization.

### Test works locally but fails in CI

- CI environments may be slower - increase retry attempts
- Check for hardcoded paths or assumptions about the environment
- Make sure the test is properly isolated (no shared state)
