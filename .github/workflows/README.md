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

Releases use a two-workflow system with manual approval via draft releases.

### Release Flow

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           1. DEVELOPER PUSHES TAG                            │
│                              git tag v1.0.0 && git push --tags               │
└─────────────────────────────────────────────────────────────────────────────┘
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                         release-build.yaml (automatic)                       │
│                                                                              │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐                   │
│  │ verify-tag   │───▶│  build-sdk   │───▶│create-draft- │                   │
│  │(must be main)│    │  (wheel)     │    │   release    │                   │
│  └──────────────┘    └──────────────┘    └──────────────┘                   │
│                             │                    ▲                           │
│                             │    ┌───────────────┘                           │
│                             ▼    │                                           │
│                      ┌──────────────┐                                        │
│                      │build-coglet- │  (4 platforms: linux x64/arm64,       │
│                      │   wheels     │   macos x64/arm64)                     │
│                      └──────────────┘                                        │
└─────────────────────────────────────────────────────────────────────────────┘
                                        │
                          Creates DRAFT GitHub Release
                          with all wheel artifacts
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                    2. MAINTAINER PUBLISHES DRAFT RELEASE                     │
│                       (manual approval via GitHub UI)                        │
└─────────────────────────────────────────────────────────────────────────────┘
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                       release-publish.yaml (automatic)                       │
│                                                                              │
│  ┌──────────────┐                                                           │
│  │verify-release│                                                           │
│  │  (tag fmt)   │                                                           │
│  └──────┬───────┘                                                           │
│         │                                                                    │
│         ├─────────────────┬─────────────────┬─────────────────┐             │
│         ▼                 ▼                 ▼                 ▼             │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐    │
│  │publish-pypi- │  │publish-pypi- │  │publish-crates│  │publish-github│    │
│  │   coglet     │  │     sdk      │  │     -io      │  │   -release   │    │
│  │   (PyPI)     │  │   (PyPI)     │  │  (crates.io) │  │  (goreleaser)│    │
│  └──────┬───────┘  └──────────────┘  └──────────────┘  └──────────────┘    │
│         │                 ▲                                                  │
│         └─────────────────┘                                                  │
│         (SDK waits for coglet - cog[coglet] depends on coglet)              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Workflows

#### `release-build.yaml`

Triggered by version tags (`v*.*.*`). Builds artifacts and creates a draft release.

| Job | Purpose |
|-----|---------|
| `verify-tag` | Ensures tag is on main branch |
| `build-sdk` | Build cog SDK wheel and sdist |
| `build-coglet-wheels` | Build coglet wheels (4 platforms) |
| `create-draft-release` | Create draft GitHub release with all artifacts |

**Security**: No secrets required - only builds artifacts.

#### `release-publish.yaml`

Triggered when a draft release is published. Publishes to PyPI and crates.io.

| Job | Depends on | Purpose |
|-----|------------|---------|
| `verify-release` | - | Validate tag format |
| `publish-pypi-coglet` | verify-release | Publish coglet to PyPI |
| `publish-pypi-sdk` | publish-pypi-coglet | Publish SDK to PyPI (after coglet) |
| `publish-crates-io` | verify-release | Publish coglet crate |
| `publish-github-release` | verify-release | Build and attach CLI binaries |

**Security**: 
- Secrets only available via GitHub environment protection rules
- Environments restricted to `v*` tags only
- Only maintainers can publish draft releases

### Package Versioning

All packages use the same version from the git tag:
- **cog SDK**: `cog==1.0.0` (PyPI)
- **coglet**: `coglet==1.0.0` (PyPI + crates.io)
- **CLI**: `cog v1.0.0` (GitHub Release)

The SDK's optional dependency `cog[coglet]` requires `coglet>=0.1.0,<1.0` to ensure compatibility.

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

1. Create environments in **Settings → Environments**:
   - `pypi` - For PyPI publishing (uses OIDC, no secrets needed)
   - `crates-io` - For crates.io publishing

2. Configure protection rules for each environment:
   - **Deployment branches**: "Selected branches and tags"
   - **Add pattern**: `v*` (restricts to version tags)
   - **Required reviewers**: Add maintainers

3. Add secrets:
   - `crates-io`: `CARGO_REGISTRY_TOKEN`

### Performing a Release

```bash
# 1. Ensure you're on main with latest changes
git checkout main
git pull

# 2. Create and push tag
git tag v1.0.0
git push origin v1.0.0

# 3. Wait for release-build.yaml to complete
#    This creates a draft release with all artifacts

# 4. Review the draft release in GitHub UI
#    - Check artifacts are present
#    - Review auto-generated release notes

# 5. Publish the draft release
#    - Click "Publish release" in GitHub UI
#    - This triggers release-publish.yaml
```
