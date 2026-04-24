---
name: release-cog
description: Guide and automate the Cog release process
---

# Cog Release Skill

This skill helps you release new versions of Cog. Cog is a multi-language, multi-artifact project with a carefully orchestrated release process.

## Overview

Cog releases include:
- **CLI binaries** (Go) - macOS and Linux, x86_64 and ARM64
- **coglet Python wheels** (Rust/PyO3) - Linux x86_64/ARM64, macOS ARM64
- **cog SDK Python wheel** (Python) - Universal
- **coglet Rust crate** - Published to crates.io

## Release Types

| Type | Format | Example | Branch | PyPI | crates.io | Homebrew |
|------|--------|---------|--------|------|-----------|----------|
| **Stable** | `v0.17.0` | v0.18.0 | main | ✓ | ✓ | ✓ |
| **Pre-release** | `v0.17.0-alpha3`, `v0.17.0-rc1` | v0.18.0-rc1 | main | ✓ | ✓ | ✗ |
| **Dev** | `v0.17.0-dev1` | v0.18.0-dev2 | any | ✗ | ✗ | ✗ |

## Quick Release Commands

### 1. Bump Version (if needed)

```bash
# Check current version
mise run version

# Bump to new version (updates VERSION.txt, Cargo.toml, Cargo.lock, commits)
mise run version:bump 0.18.0
```

### 2. Create a release branch and push

```bash
git checkout -b release/v0.18.0
git push origin release/v0.18.0
```

### 3. Create PR to main

```bash
# Open a pull request to main, get it reviewed and merged. Then you can create the release tag from main.
gh pr create --base main --head release/v0.18.0 --title "Release v0.18.0" --body "Release description and notes"
```

### 4. Create and Push Tag

```bash
git checkout main && git pull origin main

# For stable release
git tag v0.18.0
git push origin v0.18.0

# For pre-release
git tag v0.18.0-rc1
git push origin v0.18.0-rc1

# For dev release (can be from any branch)
git tag v0.18.0-dev1
git push origin v0.18.0-dev1
```

### 5. Monitor Release Build

```bash
# Watch the release build workflow
gh workflow view release-build.yaml

# Or watch in real-time
gh run watch
```

### 6. Publish Release (stable/pre-release only)

- Go to GitHub Releases page
- Find the draft release
- Review release notes
- Click "Publish release"
- This triggers `release-publish.yaml` which publishes to PyPI and crates.io

## Release Process Details

### Automated Workflows

1. **`release-build.yaml`** - Triggered on version tags
   - Verifies tag matches VERSION.txt and Cargo.toml
   - Verifies stable/pre-release tags are on main branch
   - Builds SDK wheel (with updated coglet version constraint)
   - Builds coglet wheels for all platforms (Linux x64/ARM64, macOS ARM64)
   - Uses GoReleaser to build CLI binaries and create draft release
   - Uploads wheels to GitHub release
   - For dev releases: immediately publishes as pre-release

2. **`release-publish.yaml`** - Triggered when release is published
   - Publishes coglet wheels to PyPI
   - Publishes coglet crate to crates.io
   - Publishes cog SDK to PyPI (depends on coglet)
   - Updates Homebrew tap (stable releases only)

3. **`homebrew-tap.yaml`** - Updates Homebrew cask
   - Generates cask from `.github/cog.rb.tmpl`
   - Creates PR in `replicate/homebrew-tap`

### Version Files

| File | Purpose |
|------|---------|
| `VERSION.txt` | Canonical version (single source of truth) |
| `crates/Cargo.toml` | Rust workspace version |
| `crates/Cargo.lock` | Locked dependency versions |

### Version Constraints

The SDK (`pyproject.toml`) has a dependency on coglet:
```toml
coglet>=0.1.0,<1.0
```

During release build, this is updated to:
```toml
coglet>=0.18.0,<1.0
```

This ensures the SDK depends on the matching coglet version.

## Pre-Release Checklist

Before creating a release tag:

- [ ] All tests pass: `mise run test`
- [ ] Lint passes: `mise run lint`
- [ ] Version is correct in `VERSION.txt`
- [ ] `mise run version:check` passes
- [ ] `crates/Cargo.toml` matches `VERSION.txt`
- [ ] Changelog is updated (if applicable)
- [ ] Documentation is updated (`mise run docs:llm`)

## Troubleshooting

### Version mismatch error
```
Version mismatch! VERSION.txt has X but tag is vY
```
Fix: Run `mise run version:bump Y`, push, then re-tag.

### Tag not on main
```
Release tags must be on the main branch
```
Fix: Merge your changes to main, then tag from main.

### Rebuilding a failed release
1. Delete the GitHub release if it was created: `gh release delete v0.18.0 --yes`
2. Delete the tag: `git push --delete origin v0.18.0 && git tag -d v0.18.0`
3. Fix the issue
4. Re-create and push the tag

### Manual PyPI publish (emergency)
If the automated publish fails:
```bash
# Download wheels from GitHub release
gh release download v0.18.0 -p "coglet-*.whl" -D dist
gh release download v0.18.0 -p "cog-*.whl" -D dist

# Publish with twine
 twine upload dist/coglet-*.whl  # First!
twine upload dist/cog-*.whl       # After coglet is uploaded
```

## Architecture Notes

- **Trusted Publishing**: PyPI and crates.io use OIDC trusted publishing (no API tokens in secrets)
- **Environments Required**: Configure `pypi`, `crates-io`, and `homebrew` environments in GitHub repo settings
- **CGO**: Required for go-tree-sitter (static Python schema parser)
- **Zig**: Used for Linux cross-compilation (CC=zig cc)
- **macOS builds**: Use native compiler (zig lacks macOS SDK stubs)
- **Wheel discovery**: CLI discovers wheels from `dist/` at Docker build time, not embedded in binary
