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
              │build-rust│     │ build-sdk│     │ (none)   │
              │ (wheel)  │     │ (wheel)  │     │          │
              └────┬─────┘     └────┬─────┘     └──────────┘
                   │                │
     ┌─────────────┼────────────────┼─────────────────────┐
     │             │                │                     │
     ▼             ▼                ▼                     ▼
┌─────────┐  ┌──────────┐    ┌───────────┐         ┌───────────┐
│fmt-rust │  │test-rust │    │ fmt-go    │         │fmt-python │
│lint-rust│  │coglet-py │    │ lint-go   │         │lint-python│
│  deny   │  │ (matrix) │    │ test-go   │         │test-python│
└─────────┘  └────┬─────┘    └───────────┘         └───────────┘
                  │                │                     │
                  └────────────────┼─────────────────────┘
                                   ▼
                          ┌────────────────┐
                          │test-integration│
                          │   (matrix)     │
                          └───────┬────────┘
                                  ▼
                          ┌───────────────┐
                          │  ci-complete  │  ← Branch protection requires this
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
| `test-go` | go changed | build-sdk | Run Go tests (matrix: ubuntu, macos) |
| `test-rust` | rust changed | changes | Run Rust tests |
| `test-python` | python changed | build-sdk | Run Python tests (matrix: 3.10-3.13) |
| `test-coglet-python` | rust or python changed | build-rust | Test coglet bindings (matrix: 3.10-3.13) |
| `test-integration` | any changed | build-sdk, build-rust | Integration tests (matrix: cog, cog-rust) |
| `ci-complete` | Always | all jobs | Gate job for branch protection |

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
- `pypi-package.yaml` - Replaced by `release-build.yaml` + `release-publish.yaml`.
- `version-bump.yaml` - Removed. Just edit `crates/Cargo.toml` directly.

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

## Release Workflow

Releases use a two-workflow system. There are three release types:

| Type | Example tag | Branch rule | Draft? | PyPI/crates.io? |
|------|-------------|-------------|--------|-----------------|
| **Stable** | `v0.17.0` | Must be on main | Yes (manual publish) | Yes |
| **Pre-release** | `v0.17.0-alpha3` | Must be on main | Yes (manual publish) | Yes |
| **Dev** | `v0.17.0-dev1` | Any branch | No (immediate) | No |

### Stable / Pre-release Flow

```
  Developer pushes tag on main (e.g. v0.17.0, v0.17.0-rc1)
                          │
                          ▼
              release-build.yaml (automatic)
   ┌──────────────────────────────────────────────┐
   │  verify-tag ──▶ build-sdk ──┐                │
   │  (must be       build-coglet ┼──▶ create-    │
   │   main)         build-CLI ──┘    release     │
   │                                  (DRAFT)     │
   └──────────────────────────────────────────────┘
                          │
            Maintainer publishes draft in GitHub UI
                          │
                          ▼
             release-publish.yaml (automatic)
   ┌──────────────────────────────────────────────┐
   │  coglet → PyPI ──▶ SDK → PyPI                │
   │  coglet → crates.io                          │
   └──────────────────────────────────────────────┘
```

### Dev Release Flow

```
  Developer pushes tag from any branch (e.g. v0.17.0-dev1)
                          │
                          ▼
              release-build.yaml (automatic)
   ┌──────────────────────────────────────────────┐
   │  verify-tag ──▶ build-sdk ──┐                │
   │  (no branch     build-coglet ┼──▶ create-    │
   │   restriction)  build-CLI ──┘    release     │
   │                                  (PRE-       │
   │                                   RELEASE)   │
   └──────────────────────────────────────────────┘
                          │
                 Done. No PyPI/crates.io.
          Wheels + CLI binaries on GH release.
```

### Workflows

#### `release-build.yaml`

Triggered by version tags (`v*.*.*`). Builds all artifacts and creates a GitHub release.

| Job | Purpose |
|-----|---------|
| `verify-tag` | Cargo.toml version match + branch rules (main for stable/pre-release, any for dev) |
| `build-sdk` | Build cog SDK wheel and sdist |
| `build-coglet-wheels` | Build coglet wheels (3 platforms via zig cross-compile) |
| `create-release` | Goreleaser builds CLI + creates release, then appends wheels. Dev releases are immediately published as pre-release; stable/pre-release remain as draft. |

**Security**: No secrets needed for dev. Stable/pre-release require maintainer to publish draft.

#### `release-publish.yaml`

Triggered when a release is published. Publishes to PyPI and crates.io.
**Skips entirely for dev releases** (all jobs gated on `is_dev != true`).

| Job | Depends on | Purpose |
|-----|------------|---------|
| `verify-release` | - | Validate tag format, detect dev releases |
| `publish-pypi-coglet` | verify-release | Publish coglet to PyPI (trusted publishing) |
| `publish-pypi-sdk` | publish-pypi-coglet | Publish SDK to PyPI (waits for coglet) |
| `publish-crates-io` | verify-release | Publish coglet crate (OIDC) |

### Package Versioning

All packages use **lockstep versioning** from `crates/Cargo.toml`.

| Package | Registry | Version format | Example |
|---------|----------|----------------|---------|
| cog SDK | PyPI | PEP 440 | `cog==0.17.0`, `cog==0.17.0a3`, `cog==0.17.0.dev1` |
| coglet | PyPI | PEP 440 | `coglet==0.17.0`, `coglet==0.17.0a3` |
| coglet | crates.io | semver | `coglet@0.17.0`, `coglet@0.17.0-alpha3` |
| CLI | GitHub Release | semver | `cog v0.17.0`, `cog v0.17.0-dev1` |

**Version conversion** (semver -> PEP 440):
- `0.17.0-alpha3` -> `0.17.0a3`
- `0.17.0-beta1` -> `0.17.0b1`
- `0.17.0-rc1` -> `0.17.0rc1`
- `0.17.0-dev1` -> `0.17.0.dev1`
- `0.17.0` -> `0.17.0`

### SDK Wheel Sourcing

The CLI installs the cog SDK from PyPI at container build time:

| Scenario | COG_WHEEL env var | Behavior |
|----------|-------------------|----------|
| Released CLI | (unset) | Install `cog==<version>` from PyPI |
| Dev CLI (in repo) | (unset) | Auto-detect `dist/cog-*.whl` if present, else PyPI |
| Force PyPI | `pypi` | Install latest from PyPI |
| Specific version | `pypi:0.12.0` | Install `cog==0.12.0` from PyPI |
| Local wheel | `/path/to/cog.whl` | Install from local file |
| Force dist | `dist` | Install from `dist/` (error if missing) |

Same pattern for `COGLET_WHEEL` (but coglet is optional by default).

### GitHub Environment Setup

1. Create environments in **Settings -> Environments**:
   - `pypi` - For PyPI publishing (trusted publishing, no secrets)
   - `crates-io` - For crates.io publishing (trusted publishing, no secrets)

2. Configure protection rules for each environment:
   - **Deployment branches**: "Selected branches and tags"
   - **Add pattern**: `v*` (restricts to version tags)
   - **Required reviewers**: Add maintainers

3. Configure trusted publishers:
   - **PyPI** (both `cog` and `coglet`): workflow `release-publish.yaml`, environment `pypi`
   - **crates.io** (`coglet`): workflow `release-publish.yaml`, environment `crates-io`

### Performing a Stable / Pre-release

```bash
# 1. Update crates/Cargo.toml version (e.g. "0.17.0" or "0.17.0-alpha3")
# 2. Merge to main

# 3. Tag and push
git tag v0.17.0
git push origin v0.17.0

# 4. Wait for release-build.yaml to complete (creates draft release)
# 5. Review the draft release in GitHub UI
# 6. Click "Publish release" -> triggers release-publish.yaml -> PyPI + crates.io
```

### Performing a Dev Release

```bash
# From any branch:
# 1. Update crates/Cargo.toml version (e.g. "0.17.0-dev1")
# 2. Commit and push

# 3. Tag and push
git tag v0.17.0-dev1
git push origin v0.17.0-dev1

# 4. Done. release-build.yaml creates a pre-release with all artifacts.
#    No PyPI/crates.io publishing. No manual approval needed.
```
