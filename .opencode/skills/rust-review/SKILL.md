---
name: rust-review
description: Rust code review guidelines for Coglet
---

## Rust review guidelines

This project uses Rust for Coglet (`crates/`), the prediction server that runs
inside Cog containers. It handles HTTP requests, worker process management, and
prediction execution.

### What linters already catch (skip these)

clippy runs in CI. cargo-deny audits dependencies for license/advisory issues.
Don't flag issues these would catch.

### What to look for

**Error handling**

- Use `thiserror` for typed errors in library code, `anyhow` for application errors
- Don't use `.unwrap()` or `.expect()` in non-test code unless the invariant is documented
- Error context: use `.context()` or `.with_context()` from anyhow, not bare `?`

**Ownership and lifetimes**

- Unnecessary cloning where a borrow would work
- Lifetime issues that suggest a design problem (not just annotation noise)
- Arc/Mutex when simpler patterns exist

**Async (tokio)**

- Blocking operations inside async contexts (use `spawn_blocking`)
- Missing `.await` on futures (compiler catches some, but not all logical issues)
- Proper cancellation handling and cleanup
- Task/JoinHandle leaks

**Safety**

- Any `unsafe` block needs justification and a safety comment
- FFI boundaries with Python (PyO3) -- check for panics across FFI, GIL handling
- Memory safety in IPC between parent and worker processes

**Architecture**

- `crates/coglet/` is the core: HTTP server, worker orchestration, IPC
- `crates/coglet-python/` is PyO3 bindings for Python predictor integration
- Two-process architecture: parent (HTTP + orchestrator) and worker (Python execution)
- Don't mix IPC concerns with HTTP handling
- Snapshot tests use `insta` -- check if snapshots need updating

**Dependencies**

- New dependencies should be justified -- `crates/deny.toml` audits them
- Prefer std or existing deps over adding new ones for small tasks
