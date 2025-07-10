# Cog (base-images repo) – Project Guide for Cursor

> This file is consumed by Cursor to give the AI assistant high-level context about the repository.  
> Keep it brief but comprehensive; update when conventions or structure change.

---

## What’s here?
This repository is **Cog**, an open-source CLI/tooling suite that packages machine-learning models into OCI-compliant container images.  
The codebase is mostly Go, with some Python runtime code, tests, and docs.

Key sub-domains:
• **builder / cogpack** – next-generation build system (see @builder.cursor.md & @cogpack.deepdive.cursor.md).  
• **base_images** – logic & data for choosing CUDA/CPU base images.  
• **cli** – user-facing commands (`cog build`, `cog predict`, …).  
• **docker** – thin wrappers around Docker / BuildKit APIs.  
• **python/** – Python runtime (FastAPI server, validation helpers, etc.).

---

## Repo layout (abridged)
| Path | Purpose |
|------|---------|
| `cmd/` | Go `main` packages (`cog`, internal helpers) |
| `pkg/` | All Go libraries, grouped by domain (api, cli, docker, cogpack, …) |
| `python/` | Python runtime code & tests used inside built images |
| `script/` | Top-level helper scripts (`format`, `lint`, `setup`) |
| `docs/` | MkDocs source for public docs site |
| `test-integration/` | Pytest-based E2E tests (fixtures, helpers) |
| `Makefile` | One-stop for build / test / lint tasks |

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
| Action | Command |
|--------|---------|
| Run Go unit tests | `make test-go` |
| Run Python unit tests | `make test-python` |
| Full test suite | `make test` |
| Lint & vet | `script/lint` |
| Auto-format | `script/format` |
| Build CLI binaries | `make` or `make cog` |
| Build docs locally | `make run-docs-server` then open http://localhost:8000 |

---

## Contributing workflow
1. Create feature branch from **main** (or topical branch).  
2. Keep commits small & descriptive (present-tense imperative).  
3. Include tests and update docs as needed.  
4. Run `script/format && script/lint && make test` before pushing.  
5. Open PR; reviewers will enforce CI green & convention compliance.

---

## Further reading
- https://cog.run/llms.txt - Cog documentation, formatted for LLMs
- `builder.cursor.md` – high-level roadmap + checklist for cogpack builder.  
- `cogpack.deepdive.cursor.md` – detailed decision log.  
- `docs/` – public user documentation.

---
*Happy hacking!* 
