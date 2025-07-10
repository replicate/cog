# Cogpack – Deep Dive Notes & Decision Log

> Last updated: <!-- YYYY-MM-DD -->
>
> This file supplements `builder.cursor.md` with fine-grained context captured from architecture discussions.  
> **Statuses:** ✅ Settled • 🟡 Current assumption (likely to change) • ⚠️ Deferred/Punt

## System Overview (author narrative)
The **cogpack** build system is *inspired* by CNCF Buildpacks but intentionally diverges to suit Cog’s model-building needs.

### High-level flow
1. **Stack selection** – Each registered *Stack* inspects the project (`cog.yaml`, source tree, CLI/env) via `Detect`. The **first** Stack that returns `true` wins; only one Stack runs per build (starting with the Python stack).
2. **Block orchestration** – The chosen Stack owns an **ordered list** of *Blocks* (hard-coded for now). Blocks do *not* auto-discover each other.
3. **Dependency emission** – Each Block may emit `Dependency{Name, Constraint}` records (semver ranges).
4. **Resolution loop** – A central resolver repeatedly processes all constraints, consulting compatibility matrices (Python↔CUDA↔Torch, etc.) until either a fixed set of versions is produced or resolution fails → build error.
5. **Base image** – Using the resolved versions & accelerator needs, we pick a *Cogpack Image* (currently from `pkg/base_images`).
6. **Plan construction** – Blocks append/mutate a **Plan** consisting of one or more *Stage*s:
   • **Stage** ≈ a Dockerfile stage. Fields: `Name`, `LayerID` (merge key), `Inputs` (other stage, external image, or scratch), and a list of *Op*s.
   • **Op**s (initial set): `Exec` (RUN), `Copy`, `Add`; future ENV / PATH tweaks may get special handling.
7. **Builder execution** – A dedicated Builder package converts the Plan into **BuildKit LLB** and builds the OCI image with precise layer boundaries.

### Blocks (examples for Python stack)
• **uv** – manage/create `uv.lock`, install deps.  
• **pip-requirements** – fallback when no `uv` project present.  
• **apt-packages** – install `cog.yaml` system packages.  
• **python-interpreter** – ensure requested Python version.  
• **torch / tensorflow / cuda** – detect DL frameworks & GPU needs.  
• **cog-wheel** – build & install the model wheel.  
• **weights** – gather model weights files.

### Design tenets reiterated
- **Precise layer control**: heavyweight deps (torch, cuda libs) land in isolated layers for maximal cache reuse.
- **Fail fast & clear**: Any Block error or unsatisfied dependency aborts the build with rich messaging (Cog fault vs. user fault distinction).
- **Internal first**: Only Cog’s CLI consumes cogpack; no external plugin API for Blocks/Stacks yet.
- **Ruthless scope**: TODO stubs acceptable. Post-MVP concerns (remote cache, secrets UX, metrics) are deferred.
- **Tests & docs**: Unit tests per Block, snapshot Plan tests, end-to-end BuildKit runs; docs live in repo (Mermaid diagrams welcome).

This section captures the full narrative (as of 2025-07-10) so future contributors can understand *why* the system looks the way it does.

---

## 1. Stacks & Blocks
| Topic | Status | Notes |
|-------|--------|-------|
| Naming: **Stack** (collection) & **Block** (lego brick) | ✅ | Good enough unless we discover a better term. |
| Block ordering | 🟡 | For the Python stack we will hard-code an ordered slice of Blocks. Blocks do **not** self-declare dependencies (yet). |
| Block mutability vs. append-only | ⚠️ | Leaning towards allowing Blocks to mutate the ever-growing Plan (Stages & Ops). Final model TBD. |
| Multiple stacks | 🟡 | Only Python stack needed short-term, but design should allow future stacks (Node, etc.). |

## 2. Dependency Resolution
| Topic | Status | Notes |
|-------|--------|-------|
| Dependency object | 🟡 | Each Block can emit `Dependency{Name, Constraint}` where `Constraint` is semver-style. |
| Resolver strategy | 🟡 | Central multi-pass solver that repeatedly resolves intertwined deps (e.g., python↔torch↔cuda). |
| Compat data location | ⚠️ | Currently `pkg/config/*.json`; will likely migrate to `pkg/base_images` or temp `compat`. Separate repo will own data generation. |
| Conflict handling | ✅ | Resolver failure = build failure with rich error message distinguishing Cog vs. user fault. |

## 3. Plan & Builder
| Topic | Status | Notes |
|-------|--------|-------|
| Plan schema stability | 🟡 | Free to change until externalized; lifespan = one `cog` invocation. |
| Ownership of `LayerID`, artifact names | ⚠️ | TBD during builder work. |
| Builder location | ✅ | Internal Go package within Cog repo; invoked by CLI code-path behind an env-var flag. |
| Execution backend | 🟡 | Aim for **BuildKit LLB** (gateway API). Dockerfile generator prototype lives in `pkg/factory` but is **paused**. |
| LLB debug artifacts | ⚠️ | Maybe emit LLB JSON next to image for inspection—decide later. |

## 4. Failure & Error Handling
| Scenario | Policy |
|----------|--------|
| Block `Detect` returns `error` | Build fails fast. |
| Dependency resolution fails | Build fails fast. |
| Non-critical optional feature unavailable | TBD per feature; default is fail-fast. |

## 5. Caching & Layers
| Topic | Status | Notes |
|-------|--------|-------|
| Precise layer control | ✅ | Core requirement. |
| Re-use / remote cache | ⚠️ | Desired eventually (push dev layers to registry) but out-of-scope for first milestone. |

## 6. Secrets & Credentials
| Topic | Status | Notes |
|-------|--------|-------|
| Basic secret mounts | 🟡 | Support env/CLI/file-based secrets; minimal first pass. |
| Secret declaration | ⚠️ | Likely via Plan (mount op) sourced from CLI flags. |

## 7. Source Inspection & IO
| Topic | Status | Notes |
|-------|--------|-------|
| Filesystem interface | 🟡 | Provide Blocks with an `os.Root` abstraction for safe path inspection. |
| Project ignores (.dockerignore) | ⚠️ | Not yet specified. |
| Network access during Detect | ⚠️ | Not restricted initially; revisit for reproducibility. |

## 8. Config Surface
| Topic | Status | Notes |
|-------|--------|-------|
| Using existing `cog.yaml` keys | ✅ | Blocks read current keys/env vars. |
| Block-specific config sections | ⚠️ | Punt for now. |

## 9. Versioning & Metadata
| Topic | Status | Notes |
|-------|--------|-------|
| Plan schema version | 🟡 | Stamp with `1` if needed; internal use only. |
| Image metadata (labels) | ⚠️ | Future enhancement; builder may attach provenance, dep graph, etc. |
| Build timing metrics | ⚠️ | Out-of-scope for first milestone; Result struct may get timing later. |

## 10. Milestones (rolling)
1. Plan interfaces & directory layout nailed down.  
2. Python Stack + minimal Blocks produce deterministic Plan for CPU hello-world.  
3. BuildKit builder executes Plan → OCI image.  
4. Add GPU + Torch compatibility resolution.  
5. Extend to TensorFlow variants.  
6. Replace env-flag with default path; deprecate old builder.

---
### Editing Guidelines
• Keep this file focused on *why/decision status*—no code snippets.  
• Move “settled” items out of Open Questions table in `builder.cursor.md` when resolved.  
• Update statuses diligently to avoid stale context. 
