---
name: cog-review
description: Cog architecture and cross-cutting review guidelines
---

## Cog architecture review guidelines

Cog packages ML models into production-ready containers. Use this skill for
changes that cross language boundaries or touch core architecture.

### Component overview

- **CLI** (Go): `cmd/cog/` and `pkg/` -- builds, runs, and deploys models
- **Python SDK**: `python/cog/` -- predictor interface, types, HTTP/queue server
- **Coglet** (Rust): `crates/` -- prediction server inside containers (HTTP, worker management, IPC)

### Key design patterns

**Wheel resolution**: The CLI discovers SDK and coglet wheels from `dist/` at
Docker build time. Wheels are NOT embedded in the binary. Changes to build
artifacts need to account for this.

**Dockerfile generation**: `pkg/dockerfile/` generates Dockerfiles from `cog.yaml`
config. Template injection and escaping matter here.

**Config parsing**: `pkg/config/config.go` parses `cog.yaml`. Schema is at
`pkg/config/data/config_schema_v1.0.json`. Changes must keep schema and Go
code in sync.

**Two-process coglet**: Parent process (HTTP server + orchestrator) and child
worker process (Python predictor execution) communicate via IPC. Changes to
the IPC protocol affect both Rust and Python code.

**Compatibility matrix**: CUDA/PyTorch/TensorFlow compatibility is managed in
`tools/compatgen/`. Framework version changes have wide blast radius.

### Cross-cutting concerns

- **VERSION.txt** is the single source of truth for versioning. `Cargo.toml` must match.
- Changes to `python/cog/base_predictor.py` affect all downstream model authors.
- Changes to `docs/` require running `mise run docs:llm` to regenerate `docs/llms.txt`.
- CLI reference docs are auto-generated -- changes to `cmd/` or `pkg/cli/` require `mise run docs:cli`.
- Integration tests in `integration-tests/` use Go's testscript and need a built cog binary.

### What to watch for

- Breaking changes to the predictor interface or cog.yaml schema
- Docker build changes that affect layer caching or build time
- IPC protocol changes without updating both Rust and Python sides
- Version bumps that miss one of the places VERSION.txt needs to match
- New ML framework versions without compatibility testing
