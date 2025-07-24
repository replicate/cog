# Design Decisions Index

This document indexes key architectural decisions made during cogpack development. For detailed implementation notes, see the individual session logs.

## Core Architecture Decisions

### Phase & Composer System
- **[Single Phase List Architecture](./devlog/2025-07-17-single-phase-list-architecture.md)** (2025-07-17)
  - Unified build/export phases into single ordered list
  - Eliminated artificial phase boundaries
  - Simplified cross-phase references

- **Phase Organization** (2024-07-11)
  - Logical grouping of build operations
  - Enables auto input resolution within/across phases

- **Composer Pattern** (2025-07-15)  
  - Separates plan assembly (mutable) from execution (immutable)
  - Provides clean API for blocks

### BuildKit Integration
- **[UV Block Simplification and Critical Bug Fix](./devlog/2025-07-18-uv-block-simplification-and-env-var-fix.md)** (2025-07-18)
  - Fixed critical environment variable inheritance bug
  - Implemented UV-native approach for all Python projects

- **Environment Variable Inheritance via LLB State** (2025-07-16)
  - Extract env vars from BuildKit LLB state for proper metadata flow
  - Ensures runtime dependencies like Python are available

- **Image Config Inspection for Base Images** (2025-07-16)
  - Extract environment variables during Plan → LLB translation
  - Required because BuildKit's llb.Diff() loses environment context

### Build System Design
- **[Source Copy Implementation](./devlog/2025-07-17-source-copy-implementation.md)** (2025-07-17)
  - Directory removal before copy to prevent nesting
  - Proper BuildKit Copy operation semantics

- **Mount-based Contexts** (2025-07-14)
  - Use fs.FS mounts instead of MkFile for flexibility
  - Enables embedded files, remote URLs

- **Single Stack per Build** (2024-07-11)
  - First matching stack wins
  - Simplifies orchestration, avoids conflicts

### Python-Specific Decisions
- **UV Block UV-Native Architecture** (2025-07-18)
  - Treat all Python projects as UV projects at build time
  - Convert legacy projects via generated pyproject.toml
  - Leverages UV's deterministic lockfile approach

- **Explicit Wheel Filename Resolution** (2025-07-16)
  - Replace wildcard patterns with exact filenames
  - UV requires explicit wheel filenames

- **Working Directory vs Shell Commands** (2025-07-18)
  - Use LLB working directory instead of shell cd commands
  - Shell builtins not available in minimal containers

## Implementation Patterns
- **Operation Input Resolution** (2025-07-16): Phase references in Copy operations
- **Phase Pre-registration** (2025-07-16): All phases registered at composer creation
- **Separate Phase Resolution Methods** (2025-07-16): Input vs output resolution semantics

## Testing & Quality
- **Enhanced Plan->LLB Translation Testing**: Comprehensive testing framework
- **Complete Integration Test Framework**: Testcontainers-based verification
- **Docker Test Environment Package**: Unified testing infrastructure

---

For current development status, see [../CLAUDE.cogpack.md](../CLAUDE.cogpack.md)