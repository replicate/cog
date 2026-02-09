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
│                      │build-coglet- │  (3 platforms: linux x64/arm64,       │
│                      │   wheels     │   macos arm64; all via zig)            │
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
│         ├─────────────────┬─────────────────┐                               │
│         ▼                 ▼                 ▼                                │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐                      │
│  │publish-pypi- │  │publish-crates│  │publish-pypi- │                      │
│  │   coglet     │──│     -io      │  │     sdk      │                      │
│  │   (PyPI)     │  │  (crates.io) │  │   (PyPI)     │                      │
│  └──────┬───────┘  └──────────────┘  └──────────────┘                      │
│         │                                    ▲                               │
│         └────────────────────────────────────┘                               │
│         (SDK waits for coglet via needs: dependency)                        │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Workflows

#### `release-build.yaml`

Triggered by version tags (`v*.*.*`). Builds artifacts and creates a draft release.

| Job | Purpose |
|-----|---------|
| `verify-tag` | Verify tag and version (branch rules + Cargo.toml match) |
| `build-sdk` | Build cog SDK wheel |
| `build-coglet-wheels` | Build coglet wheels (3 platforms via zig cross-compile) |
| `create-draft-release` | Goreleaser builds CLI + creates draft, then appends wheels |

**Security**: Requires `contents: write` for release creation.

#### `release-publish.yaml`

Triggered when a draft release is published. Publishes to PyPI and crates.io.

| Job | Depends on | Purpose |
|-----|------------|---------|
| `verify-release` | - | Validate tag format |
| `publish-pypi-coglet` | verify-release | Publish coglet to PyPI (trusted publishing) |
| `publish-pypi-sdk` | publish-pypi-coglet | Publish SDK to PyPI (waits for coglet) |
| `publish-crates-io` | verify-release | Publish coglet crate (OIDC via crates-io-auth-action) |

**Security**: 
- All publishing uses OIDC trusted publishing (no long-lived tokens)
- Environments restricted to `v*` tags only
- Only maintainers can publish draft releases

### Package Versioning

All packages use **lockstep versioning** from a single source of truth: `crates/Cargo.toml`.

| Package | Registry | Version format | Example |
|---------|----------|----------------|---------|
| cog SDK | PyPI | PEP 440 | `cog==0.17.0`, `cog==0.17.0a3` |
| coglet | PyPI | PEP 440 | `coglet==0.17.0`, `coglet==0.17.0a3` |
| coglet | crates.io | semver | `coglet@0.17.0`, `coglet@0.17.0-alpha3` |
| CLI | GitHub Release | semver | `cog v0.17.0`, `cog v0.17.0-alpha3` |

**Version conversion** (semver → PEP 440):
- `0.17.0-alpha3` → `0.17.0a3`
- `0.17.0-beta1` → `0.17.0b1`
- `0.17.0-rc1` → `0.17.0rc1`
- `0.17.0-dev1` → `0.17.0.dev1`
- `0.17.0` → `0.17.0`

The SDK's optional dependency `cog[coglet]` requires `coglet>=<version>,<1.0` to ensure compatibility.

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
   - `pypi` - For PyPI publishing (trusted publishing, no secrets)
   - `crates-io` - For crates.io publishing (trusted publishing, no secrets)

2. Configure protection rules for each environment:
   - **Deployment branches**: "Selected branches and tags"
   - **Add pattern**: `v*` (restricts to version tags)
   - **Required reviewers**: Add maintainers

3. Configure trusted publishers:
   - **PyPI** (both `cog` and `coglet`): workflow `release-publish.yaml`, environment `pypi`
   - **crates.io** (`coglet`): workflow `release-publish.yaml`, environment `crates-io`

### Stable Releases

Stable releases (`v0.17.0`) are tagged from `main`.

```bash
# 1. Ensure Cargo.toml version matches the release
grep '^version' crates/Cargo.toml  # should be "0.17.0"

# 2. Tag and push
git tag v0.17.0
git push origin v0.17.0

# 3. Wait for release-build.yaml to complete
#    Goreleaser builds CLI and creates draft, wheels are appended

# 4. Review the draft release in GitHub UI

# 5. Click "Publish release" → triggers release-publish.yaml
#    Publishes coglet to PyPI + crates.io, then SDK to PyPI
```

### Pre-releases

Pre-releases (`v0.17.0-alpha3`) are tagged from `prerelease/*` branches.

```bash
# 1. Create prerelease branch (if not exists)
git checkout -b prerelease/0.17.0

# 2. Set version in Cargo.toml
#    This is the single source of truth for ALL package versions
#    Edit crates/Cargo.toml: version = "0.17.0-alpha3"

# 3. Commit and push
git add crates/Cargo.toml
git commit -m "chore: bump version to 0.17.0-alpha3"
git push origin prerelease/0.17.0

# 4. Tag and push
git tag v0.17.0-alpha3
git push origin v0.17.0-alpha3

# 5. Same flow as stable: review draft → publish
```

#### Pre-release lifecycle

```
prerelease/0.17.0 branch:
  v0.17.0-alpha1 → v0.17.0-alpha2 → ... → v0.17.0-alpha5

When ready for stable:
  1. Update Cargo.toml to "0.17.0" (remove pre-release suffix)
  2. Merge prerelease/0.17.0 → main
  3. Tag v0.17.0 on main
```

#### Branch rules

- **Stable tags** (`v0.17.0`): must be on `main`
- **Pre-release tags** (`v0.17.0-alpha3`): must be on `prerelease/*`
- **Tags are immutable** (GitHub ruleset)
- **Only maintainers** can create `prerelease/*` branches
