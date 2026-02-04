# CI Architecture

This document describes the CI/CD architecture for the Cog repository.

## Design Principles

1. **Single gate job** - Branch protection uses one required check (`ci-complete`) that depends on all other jobs
2. **Path-based filtering** - Jobs skip when irrelevant files change (Go changes don't trigger Rust tests)
3. **Build once, test many** - Artifacts built once and reused across test jobs
4. **Parallel execution** - Independent jobs run concurrently
5. **Skipped = passing** - Jobs that skip due to path filtering count as passing for the gate

## Workflows

### `ci.yaml` - Main CI Pipeline

The primary CI workflow that runs on all PRs and pushes to main.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              CHANGES DETECTION                               │
│  Determines which components changed: go, rust, python, integration-tests   │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                    ┌─────────────────┼─────────────────┐
                    ▼                 ▼                 ▼
              ┌──────────┐     ┌──────────┐     ┌──────────┐
              │ build-go │     │build-rust│     │build-sdk │
              │ (binary) │     │ (wheel)  │     │ (wheel)  │
              └────┬─────┘     └────┬─────┘     └────┬─────┘
                   │                │                │
     ┌─────────────┴───┐    ┌──────┴──────┐   ┌────┴────────┐
     ▼                 ▼    ▼             ▼   ▼             ▼
┌─────────┐      ┌─────────┐ ┌─────────┐ ┌─────────┐  ┌──────────┐
│ fmt-go  │      │fmt-rust │ │lint-rust│ │fmt-python│ │lint-python│
│ lint-go │      │  deny   │ │test-rust│ │lint-python│ │test-python│
│ test-go │      └─────────┘ └─────────┘ └──────────┘ │ (matrix) │
└────┬────┘                                           └─────┬────┘
     │                                                      │
     └──────────────────────┬───────────────────────────────┘
                            ▼
                   ┌────────────────┐
                   │test-integration│
                   │   (matrix)     │
                   └───────┬────────┘
                           ▼
                   ┌───────────────┐
                   │  ci-complete  │  ← Branch protection requires this
                   └───────────────┘
                           │
                           ▼ (on tag)
                   ┌───────────────┐
                   │    release    │
                   └───────────────┘
```

#### Jobs

| Job | Runs when | Depends on | Purpose |
|-----|-----------|------------|---------|
| `changes` | Always | - | Detect which components changed |
| `build-sdk` | python changed | changes | Build cog SDK wheel |
| `build-rust` | rust changed | changes | Build coglet ABI3 wheel |
| `fmt-go` | go changed | changes | Check Go formatting |
| `fmt-rust` | rust changed | changes | Check Rust formatting |
| `fmt-python` | python changed | changes | Check Python formatting |
| `lint-go` | go changed | changes | Lint Go code |
| `lint-rust` | rust changed | changes | Run clippy |
| `lint-rust-deny` | rust changed | changes | Check licenses/advisories |
| `lint-python` | python changed | build-sdk | Lint Python code |
| `test-go` | go changed | build-sdk, build-rust | Run Go tests (matrix: ubuntu, macos) |
| `test-rust` | rust changed | changes | Run Rust tests |
| `test-python` | python changed | build-sdk | Run Python tests (matrix: 3.10-3.13) |
| `test-coglet-python` | rust or python changed | build-rust | Test coglet bindings (matrix: 3.10-3.13) |
| `test-integration` | any changed | build-sdk, build-rust, test-go | Integration tests (matrix: cog, cog-rust) |
| `ci-complete` | Always | all jobs | Gate job for branch protection |
| `release` | Tag push | ci-complete | Create GitHub release |

#### Python Version Matrix

Python versions are defined once at the workflow level:

```yaml
env:
  SUPPORTED_PYTHONS: '["3.10", "3.11", "3.12", "3.13"]'
```

Jobs that need the matrix reference it via `fromJson(env.SUPPORTED_PYTHONS)`.

### `codeql.yml` - Security Analysis

Runs CodeQL security scanning for Go, Python, and Rust.

- **Triggers**: Push to main, PRs to main, weekly schedule
- **Languages**: go, python, rust

### Deleted Workflows

- `rust.yaml` - Consolidated into `ci.yaml`. The separate workflow was redundant.

## Caching Strategy

### Rust Cache
- **Save**: Only on `main` branch pushes (to avoid PR cache pollution)
- **Restore**: On all runs (PRs restore from main's cache)
- Uses `Swatinem/rust-cache@v2` with workspace path `crates -> target`

### Go Cache
- Built into `actions/setup-go` via `cache-dependency-path`

### Python/uv Cache
- Built into `jdx/mise-action` and `astral-sh/setup-uv`

## Artifacts

| Artifact | Contents | Retention |
|----------|----------|-----------|
| `CogPackage` | cog-*.whl, cog-*.tar.gz | Default (90 days) |
| `CogletRustWheel` | coglet-*-cp310-abi3-*.whl | Default (90 days) |

The ABI3 wheel is built with Python 3.10 minimum but works on all 3.10+ versions.

## Local Development

Use mise tasks to run the same checks locally:

```bash
# Format (check)
mise run fmt

# Format (fix)
mise run fmt:fix

# Lint
mise run lint

# Test
mise run test:go
mise run test:rust
mise run test:python

# Build
mise run build:cog
mise run build:coglet
mise run build:sdk
```

## Adding New Checks

1. Add a mise task in `mise.toml`
2. Add a job in `ci.yaml` with appropriate `needs` and path filtering
3. Add the job to `ci-complete`'s needs list
4. Update this README

## Branch Protection

Configure branch protection to require only `ci-complete`:

```
Settings > Branches > main > Require status checks:
  ✓ ci-complete
```

Skipped jobs (from path filtering) are treated as passing by the gate job.
