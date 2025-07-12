# Cog – Project Guide for LLM Assistants

> **For LLM Assistants**: This file provides high-level context about the Cog repository structure, conventions, and current development focus. For cogpack-specific work, see `CURSOR.COGPACK.md`.
> 
> **Last Updated**: 2025-07-11  
> **Current Branch Focus**: cogpack build system implementation

---

## What’s here?
This repository is **Cog**, an open-source CLI/tooling suite that packages machine-learning models into OCI-compliant container images. Used by thousands of ML engineers to containerize models for production deployment.

The codebase is primarily Go (backend/CLI) with Python runtime components.

### Key Sub-Domains & Current Status:
• **cogpack** – 🚧 **Active Development**: Next-generation build system with mount-based contexts (see `CURSOR.COGPACK.md`)  
• **base_images** – 📦 **Stable**: Logic & data for choosing CUDA/CPU base images  
• **cli** – 🔧 **Maintenance**: User-facing commands (`cog build`, `cog predict`, `cog push`, etc.)  
• **docker** – 🔧 **Maintenance**: Thin wrappers around Docker / BuildKit APIs  
• **python/** – 🔧 **Maintenance**: Python runtime (FastAPI server, validation helpers, etc.)  
• **config** – 🔧 **Maintenance**: cog.yaml parsing and validation  
• **util** – 🔧 **Maintenance**: Shared utilities (console output, JSON, etc.)

---

## Repo layout (detailed)
| Path | Purpose | Notes |
|------|---------|-------|
| `cmd/` | Go `main` packages (`cog`, internal helpers) | Entry points for CLI |
| `pkg/` | All Go libraries, grouped by domain | Core business logic |
| `├── pkg/cogpack/` | 🚧 **Next-gen build system** | Mount-based contexts, stacks & blocks |
| `├── pkg/cli/` | CLI command implementations | `build`, `predict`, `push`, etc. |
| `├── pkg/docker/` | Docker/BuildKit API wrappers | Container orchestration |
| `├── pkg/config/` | cog.yaml parsing & validation | Project configuration |
| `├── pkg/util/` | Shared utilities | Console, JSON, file helpers |
| `python/` | Python runtime code & tests | Used inside built images |
| `script/` | Shell scripts for dev tasks | `format`, `lint`, `setup` |
| `docs/` | MkDocs source for public docs | Published to cog.run |
| `test-integration/` | E2E tests with real projects | Pytest-based fixtures |
| `Makefile` | Build, test, lint orchestration | One-stop dev commands |

---

## Current Development Focus (cogpack branch)

### 🚧 Active Work: Mount-Based Context System
The main development effort is on the **cogpack** build system, specifically implementing a mount-based context system for flexible file handling during builds.

### Key Files for LLM Assistants:
| File | Purpose | Status |
|------|---------|--------|
| `CURSOR.COGPACK.md` | **Complete cogpack context** | 📖 Read this for cogpack work |
| `pkg/cogpack/plan/plan.go` | Core data structures (Plan, Stage, BuildContext) | ✅ Complete |
| `pkg/cogpack/builder/buildkit.go` | BuildKit LLB translation with mount support | ✅ Complete |
| `pkg/cogpack/stacks/python/` | Python stack implementation | ✅ Core complete |
| `pkg/cogpack/builder/context.go` | Generic context management | ✅ Complete |
| `docs/mount-based-contexts.md` | Technical documentation | ✅ Complete |

### Current Feature Status:
- ✅ **Mount-based contexts** - Generic fs.FS mounting system
- ✅ **Plan validation** - Comprehensive validation including contexts
- ✅ **BuildKit integration** - Full LLB translation with mounts
- ✅ **Integration tests** - End-to-end testing with Docker
- 🚧 **Additional blocks** - TorchBlock, AptBlock, etc. (in progress)

---

## Technology stack
| Layer | Details |
|-------|---------|
| Language | **Go 1.24** (primary), Python 3.11+ for runtime/tests |
| Containers | Docker, BuildKit (via `github.com/moby/buildkit`), OCI image-spec |
| Dependency Mgmt | Go modules; Python uses **uv** + `pyproject.toml` |
| Lint / Format | `golangci-lint`, `goimports`, `ruff` |
| Testing | Go: `go test`, gotestsum; Python: `pytest`, `tox`; Integration: docker-based fixtures |

---

## Coding conventions (Go)
1. **Package layout** – prefer small, cohesive packages under `pkg/`; avoid import cycles.
2. **Contexts** – accept `context.Context` as the *first* arg for long-running / IO funcs.
3. **Errors**
   • Wrap with `%w` (`fmt.Errorf("xyz: %w", err)`).  
   • Use sentinel errors in domain packages (e.g., `ErrNoMatch`).
4. **Logging** – use `pkg/util/console` for CLI output; avoid global loggers in libraries.
5. **Tests** – table-driven; place in same pkg with `_test.go`; aim for ≥80 % coverage of new code.
6. **Formatting** – run `script/format` (make fmt) before committing.
7. **Lint** – run `script/lint` (golangci-lint + vet + Ruff) in CI & locally.
8. **Generics** – welcome where clarity outweighs complexity.
9. **Imports** – std-lib first, third-party, then internal (`github.com/replicate/cog/...`).

### Python conventions
• Follow PEP8/PEP484; enforced by Ruff & MyPy (via tox).  
• Use `pydantic` models for request/response schemas.  
• Keep runtime package import-safe (no heavy deps at import time).

---

## Common tasks
| Action | Command | Notes |
|--------|---------|-------|
| Run Go unit tests | `make test-go` | Fast feedback loop |
| Run Python unit tests | `make test-python` | Runtime component tests |
| Full test suite | `make test` | CI-equivalent |
| Lint & vet | `script/lint` | Fix before committing |
| Auto-format | `script/format` | Go + Python formatting |
| Build CLI binaries | `make` or `make cog` | Output to `.build/cog` |
| Build docs locally | `make run-docs-server` | http://localhost:8000 |
| **Test cogpack** | `COGPACK=1 go test ./pkg/cogpack/...` | Enable feature flag |
| **Test integration** | `COGPACK_INTEGRATION=1 go test ./pkg/cogpack/...` | Requires Docker |
| **Debug build** | `COGPACK=1 go run cmd/cog/main.go build --debug` | See plan output |

### Cogpack-Specific Commands:
| Action | Command | Purpose |
|--------|---------|---------|
| Test mount system | `go test ./pkg/cogpack/builder/... -v` | Context & mount tests |
| Test Python stack | `go test ./pkg/cogpack/stacks/python/... -v` | Stack implementation |
| Run integration test | `COGPACK_INTEGRATION=1 go test ./pkg/cogpack/ -run TestBuildKit` | End-to-end validation |

---

## Contributing workflow
1. Create feature branch from **main** (or topical branch).  
2. Keep commits small & descriptive (present-tense imperative).  
3. Include tests and update docs as needed.  
4. Run `script/format && script/lint && make test` before pushing.  
5. Open PR; reviewers will enforce CI green & convention compliance.

---

## For LLM Assistants: Key Patterns & Context

### 🔍 When Working on Cogpack:
1. **Always read `CURSOR.COGPACK.md` first** - Contains complete technical context
2. **Use feature flag** - Set `COGPACK=1` for testing cogpack features
3. **Follow mount-based patterns** - Use contexts and mounts, not MkFile operations
4. **Validate comprehensively** - Use `ValidatePlan()` for all plan validation
5. **Test with integration** - Use `COGPACK_INTEGRATION=1` for BuildKit tests

### 🧩 Architecture Key Points:
- **Stack + Blocks + Plan + Builder** - Clear separation of concerns
- **Mount-based contexts** - Generic fs.FS mounting system
- **Plan as source of truth** - All build state flows through Plan
- **BuildKit LLB backend** - Uses "moby" exporter for local Docker

### 🚨 Common Gotchas:
- **Platform specification** - Always include `linux/amd64` in LLB operations
- **Context validation** - Ensure referenced contexts exist in Plan.Contexts
- **Input types** - Use Input struct (not strings) for all source references
- **Import cycles** - Keep packages under `pkg/` decoupled

### 📁 File Organization Patterns:
```
pkg/cogpack/
├── plan/          # Core data structures (Plan, Stage, etc.)
├── stacks/        # Stack implementations (python/, etc.)
├── builder/       # BuildKit integration & LLB translation
└── project/       # Source introspection & analysis
```

### 🔧 Testing Strategy:
- **Unit tests** - Individual components with mocks
- **Integration tests** - Real BuildKit builds with Docker
- **Context tests** - Mount system and filesystem handling
- **Validation tests** - Plan validation and error handling

---

## Further reading
- **🎯 `CURSOR.COGPACK.md`** – **Start here for cogpack work** - Complete technical context
- **📚 `docs/mount-based-contexts.md`** – Technical deep-dive on context system
- **🌐 https://cog.run/llms.txt** - Cog documentation, formatted for LLMs  
- **📖 `docs/`** – Public user documentation
- **🧪 `test-integration/test_integration/fixtures/`** – Example projects for testing

---
*Happy hacking! 🚀* 
