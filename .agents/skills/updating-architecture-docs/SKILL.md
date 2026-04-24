---
name: updating-architecture-docs
description: "Use this skill when updating, reviewing, or creating architecture documentation in the architecture/ directory. This includes after refactors, feature additions, component changes, or when auditing docs for accuracy. Use it any time code changes affect how Cog's internals work -- new packages, changed IPC protocols, modified build pipeline, runtime behavior changes. Also use it proactively when reviewing PRs that touch core systems to check whether the architecture docs need updating."
---

# Updating Architecture Docs

## Purpose

The `architecture/` directory is a bridge. It takes someone who already knows what Cog does and how to use it (that's what `docs/` is for) to the point where they can navigate the source code with confidence. It answers "how does this work and why is it shaped this way" -- the 10,000 foot view of the project and its ecosystem.

The goal is to move a reader from fuzzy concepts to clarity:

- What are the components and what do they own?
- What's the vocabulary? (What does "slot" mean? What's the "envelope"?)
- Where are the boundaries between systems?
- What are the key design decisions and why were they made?
- Where do I look in the source to go deeper?

The architecture docs are **not** a substitute for reading code or a prose summary of the implementation. They give the reader enough context to make the code legible on first contact. Once someone understands that coglet uses a two-process model with slot-based IPC, reading `orchestrator.rs` makes sense. Without that context, it's just code.

## Document structure

The docs are numbered to suggest a reading order. `00-overview.md` orients the reader and points to everything else. Each subsequent doc builds on concepts from the ones before it -- someone reading about the container runtime should already understand the predictor class and the HTTP API.

The current docs and their structure aren't fixed. New docs should be added when a topic becomes important enough to deserve its own section (testing philosophy, deployment model, etc.). Docs can be split, merged, or reorganized as the system evolves. The numbering is for reading order, not a permanent taxonomy. Check `architecture/` for the current set.

## Principles

### Bridge, don't summarize

The architecture docs should give a reader the mental model they need to read the code -- not replace the code. If a section starts reading like a prose restatement of what a module does line by line, it's gone too far. Pull back to the concepts and boundaries.

Good: "The orchestrator spawns a single worker subprocess and manages its lifecycle. Communication happens over two channels: a control channel for lifecycle events (stdin/stdout, JSON lines) and per-slot Unix sockets for prediction data."

Bad: "The `spawn_worker` function in `orchestrator.rs` calls `Command::new('python')` with args `['-c', 'import coglet; coglet.server._run_worker()']` and sets up stdin/stdout pipes. It then reads from the control channel in a loop, matching on `ControlResponse` variants..."

The first gives you the mental model. The second just restates the code. A reader with the first description can read `orchestrator.rs` and follow along. A reader with the second didn't need to read the doc at all.

### Point at packages, not files

Reference source locations at the **package/directory level** with a description of what that package owns. Specific file paths and line numbers rot as code moves around. A pointer like "`crates/coglet/src/bridge/` -- IPC protocol and transport" stays accurate through refactors. "`bridge/protocol.rs:69` -- ControlRequest enum" doesn't.

Only document packages that matter for understanding the system's shape. Generic utility packages (`pkg/util/`, `pkg/path/`, etc.) don't need a mention -- their existence is obvious and they don't help a reader build a mental model. If someone needs them, they'll find them.

When a specific file reference is genuinely useful (a key entry point, a non-obvious starting point for understanding a subsystem), include it -- but prefer "the `PredictionService` in `service.rs`" over a line number.

### Document boundaries, not internals

Focus on interfaces between components: what messages cross the IPC channel, what the HTTP API contract is, what labels a built image carries, what env vars control behavior. These are the things a reader needs to understand to reason about the system. Implementation details behind those boundaries belong in code comments and crate-level READMEs, not in architecture docs.

The practical test: would an internal refactor (changing how something works without changing its interface) require updating this doc? If yes, the doc is too detailed.

### Explain the why

Design decisions are the most valuable content in architecture docs. They're the one thing you can't get from reading the code. "Why two processes?" has an answer (isolation, CUDA contexts, crash resilience) that makes the whole architecture make sense. Without it, a reader sees the complexity of IPC and wonders whether it's accidental.

Every major structural choice should have a short rationale. One or two sentences is enough.

## Current state of the world

These facts should be reflected consistently across all docs. If the code changes and these become stale, the docs need updating.

**One runtime:** Coglet (Rust/Axum + PyO3) is the sole runtime. There's no legacy Python/FastAPI runtime, no toggle, no "experimental" qualifier. Don't frame coglet as an alternative to something else.

**Pydantic is not core:** Neither schema path uses pydantic. The default static path parses Python source in Go with tree-sitter and emits OpenAPI directly. The legacy runtime fallback uses Python's `inspect` module + a custom ADT dataclass system in `_adt.py`/`_schemas.py`. `cog.BaseModel` is a dataclass wrapper. Pydantic BaseModel is supported in user code for compatibility but isn't part of Cog's own type system.

**Static schema path is the default:** The tree-sitter-based static schema generator (Go side, `pkg/schema/`) runs by default on all `cog build` invocations, with automatic fallback to the legacy runtime path on `ErrUnresolvableType`. Users can force the runtime path globally with `COG_LEGACY_SCHEMA=1` as a lifeline for SDK < 0.17.0 or static-parser edge cases. The runtime path remains in the tree as the fallback; it is not going away yet.

**Wheels aren't embedded:** SDK and coglet wheels are resolved at Docker build time from PyPI, env vars, or local `dist/` directory. They're not compiled into the Go binary.

**Three codebases:**

- `cmd/cog/` + `pkg/` -- Go CLI and build tooling
- `python/cog/` -- Python SDK (type definitions, predictor base class, thin server launcher)
- `crates/coglet/` + `crates/coglet-python/` -- Rust prediction server with PyO3 bindings

## How to update

### When to update

After code changes, ask: **did a boundary or interface change?**

Needs doc updates:

- New or removed IPC message types
- New or changed HTTP endpoints
- New CLI commands
- Changes to the build pipeline (new build steps, changed Dockerfile structure)
- New top-level packages in `pkg/` or new crates in `crates/`
- Changes to how components communicate
- New design decisions or changed rationale for existing ones

Doesn't need doc updates:

- Bug fixes within a component
- Internal refactors that don't change interfaces
- Performance improvements
- Test changes

### Auditing for accuracy

When checking docs against the codebase:

1. **Read each doc** and identify claims about the system -- component names, package locations, protocol messages, env vars, build steps, CLI commands
2. **Verify against source** -- check that things exist and work as described
3. **Classify issues**:
   - **Structural** -- describes something that doesn't exist or works fundamentally differently (fix immediately)
   - **Misleading** -- technically true but gives the wrong mental model (fix immediately)
   - **Missing** -- a feature or component exists with no doc coverage (add if it's a boundary/interface concern)
   - **Stale reference** -- file/package renamed but concept is right (fix opportunistically)
4. Fix structural and misleading issues first -- they actively harm readers

### Writing conventions

Read the existing architecture docs before writing. Match their tone and level of detail. New content should feel like it belongs -- if it reads like a different author wrote it, adjust.

The docs are technical writing. Clear, precise, no filler. Don't inflate -- avoid the kind of padded, formal language that agents default to ("it should be noted that", "robust", "comprehensive", "this ensures that"). Just say the thing.

Diagrams use Mermaid or ASCII art. Both are fine. Use whichever communicates the structure more clearly. Keep each diagram focused on one concept.
