# Cogpack Build System - Design Document

> **Last Updated**: 2025-01-27  
> **Status**: Design Complete, Ready for Implementation  
> **Context**: This document captures the complete architectural design for the cogpack build system as developed through iterative discussion and refinement.

## Table of Contents
1. [Goals & Objectives](#goals--objectives)
2. [What We're Optimizing For](#what-were-optimizing-for)
3. [What We're Deferring](#what-were-deferring)
4. [System Overview](#system-overview)
5. [Core Components](#core-components)
6. [Data Structures](#data-structures)
7. [Workflow & Interactions](#workflow--interactions)
8. [Design Reasoning](#design-reasoning)
9. [Implementation Checklist](#implementation-checklist)
10. [Context for Future Sessions](#context-for-future-sessions)

---

## Goals & Objectives

### Primary Goals
- **Replace existing Cog build system** with a more maintainable, extensible architecture
- **Precise layer control** for optimal Docker image caching and build performance
- **Deterministic builds** with explicit dependency resolution
- **Modular design** where components (Stacks, Blocks) are loosely coupled and easily testable
- **Support complex ML workflows** including GPU/CUDA dependencies, multiple Python package managers, and multi-stage builds

### Success Criteria
- Build and run Python models with CPU and GPU variants
- Support PyTorch and TensorFlow frameworks
- Handle different Python package managers (uv, pip) automatically
- Generate reproducible builds with clear dependency resolution
- Maintain build performance through intelligent layer caching

---

## What We're Optimizing For

### Developer Experience
- **Ease of hacking** - internal engineers should be able to understand and extend the system quickly
- **Clear error messages** - distinguish between Cog faults and user faults
- **Debugging support** - JSON-serializable plans for inspection and testing

### Build Performance
- **Layer caching** - heavy dependencies (PyTorch, CUDA) get isolated layers
- **Parallel builds** - BuildKit handles dependency graph optimization
- **Incremental builds** - only rebuild what changed

### Maintainability
- **Modular architecture** - Stacks orchestrate, Blocks implement
- **Explicit interfaces** - clear contracts between components
- **Testable components** - unit tests for Blocks, integration tests for Stacks

---

## What We're Deferring

### Out of Scope for Initial Implementation
- **Complex dependency resolution** - start with simple cases, expand as needed
- **Remote caching** - local builds only initially
- **Multiple language stacks** - focus on Python first
- **Advanced BuildKit features** - use stable LLB operations only
- **Secrets management** - basic implementation, expand later
- **Performance optimization** - focus on correctness first
- **Plan serialization for debugging** - JSON support exists but tooling deferred
- **Comprehensive validation** - implement basic checks, expand as needed

### Technical Debt Acknowledged
- **Error handling sophistication** - basic errors initially
- **Metrics/logging** - minimal instrumentation
- **Documentation generation** - manual docs for now
- **Configuration flexibility** - hardcoded decisions initially

---

## System Overview

The cogpack system follows a **Stack → Blocks → Plan → Builder** architecture:

```
┌─────────────────────────────────────────────────────────────┐
│                    Build Orchestration                      │
├─────────────────────────────────────────────────────────────┤
│ 1. Stack Detection    │ Find the right stack (Python, etc.) │
│ 2. Block Composition  │ Stack orchestrates relevant blocks   │
│ 3. Dependency Collection │ Blocks emit dependency requirements │
│ 4. Dependency Resolution │ Central resolver handles conflicts │
│ 5. Plan Generation    │ Blocks contribute stages to plan     │
│ 6. Plan Execution     │ Builder converts plan to BuildKit   │
└─────────────────────────────────────────────────────────────┘
```

### Key Principles
1. **Single Stack per build** - first stack to detect wins
2. **Deterministic block selection** - based on project analysis
3. **Explicit dependency resolution** - no hidden version conflicts
4. **Phase-based planning** - logical build phases with clear boundaries
5. **BuildKit LLB backend** - precise layer control through squash pattern

---

## Core Components

### 1. Stack
**Purpose**: Intelligent orchestrator for a specific project type (e.g., Python)

**Responsibilities**:
- Detect if it can handle the project
- Compose and orchestrate relevant blocks
- Manage the overall build workflow for its domain

**Key Insight**: Stacks contain sophisticated composition logic, not just static block lists. They make decisions about which blocks to use based on project characteristics.

### 2. Block
**Purpose**: Self-contained build component (e.g., install apt packages, manage Python dependencies)

**Responsibilities**:
- Detect if it's needed for the current project
- Emit dependency requirements
- Contribute build operations to the plan
- Handle both build-time and export-time operations

**Key Insight**: Blocks are focused on implementation details while staying decoupled from other blocks.

### 3. Plan
**Purpose**: Complete build specification that can be executed by a builder

**Responsibilities**:
- Store resolved dependencies
- Organize build operations into logical phases
- Provide stage lookup and validation
- Serve as the contract between planning and building

### 4. Builder
**Purpose**: Execute a plan using BuildKit LLB

**Responsibilities**:
- Convert plan stages to BuildKit operations
- Handle the squash pattern for layer control
- Manage build context and output

---

## Data Structures

### Plan Structure
```go
type Plan struct {
    Platform      Platform                 `json:"platform"`      // linux/amd64
    Dependencies  map[string]Dependency    `json:"dependencies"`  // resolved versions
    BaseImage     BaseImage               `json:"base_image"`    // build/runtime images
    BuildPhases   []Phase                 `json:"build_phases"`  // organized build work
    ExportPhases  []Phase                 `json:"export_phases"` // runtime image assembly
    Export        *ExportConfig           `json:"export"`        // final image config
}
```

### Phase Structure
```go
type Phase struct {
    Name   StagePhase `json:"name"`   // PhaseSystemDeps, PhaseFrameworkDeps, etc.
    Stages []Stage    `json:"stages"` // all stages within this phase
}
```

### Stage Structure
```go
type Stage struct {
    ID         string   `json:"id"`         // unique identifier (set by block)
    Name       string   `json:"name"`       // human-readable name
    Source     Input    `json:"source"`     // input dependency
    Operations []Op     `json:"operations"` // build operations
    Env        []string `json:"env"`        // environment state
    Dir        string   `json:"dir"`        // working directory
    Provides   []string `json:"provides"`   // what this stage provides
}
```

### Base Image Metadata
```go
type BaseImageMetadata struct {
    Packages map[string]Package `json:"packages"` // "python", "cuda", "git", etc.
}

type Package struct {
    Name         string `json:"name"`                     // "python", "cuda"
    Version      string `json:"version"`                  // "3.11.8", "11.8"
    Source       string `json:"source"`                   // "apt", "base-image", "uv"
    Executable   string `json:"executable,omitempty"`     // "/usr/bin/python3"
    SitePackages string `json:"site_packages,omitempty"`  // "/usr/local/lib/python3.11/site-packages"
    LibPath      string `json:"lib_path,omitempty"`       // "/usr/local/cuda/lib64"
}
```

---

## Workflow & Interactions

### 1. Main Orchestration
```go
func Plan(ctx context.Context, src *SourceInfo) (*PlanResult, error) {
    // 1. Initialize plan
    plan := &Plan{Platform: Platform{OS: "linux", Arch: "amd64"}}
    
    // 2. Select stack (first match wins)
    stack := selectStack(ctx, src)
    
    // 3. Let stack orchestrate the build
    if err := stack.Plan(ctx, src, plan); err != nil {
        return nil, err
    }
    
    // 4. Validate and return
    return &PlanResult{Plan: plan}, nil
}
```

### 2. Stack Orchestration (Python Example)
```go
func (s *PythonStack) Plan(ctx context.Context, src *SourceInfo, plan *Plan) error {
    // Phase 1: Compose blocks
    blocks := s.composeBlocks(ctx, src) // intelligent composition
    
    // Phase 2: Collect dependencies
    var allDeps []Dependency
    for _, block := range blocks {
        if active, _ := block.Detect(ctx, src); active {
            deps, _ := block.Dependencies(ctx, src)
            allDeps = append(allDeps, deps...)
        }
    }
    
    // Phase 3: Resolve dependencies
    resolved, err := ResolveDependencies(ctx, allDeps)
    if err != nil {
        return err
    }
    plan.Dependencies = resolved
    
    // Phase 4: Generate plan
    for _, block := range blocks {
        if active, _ := block.Detect(ctx, src); active {
            block.Plan(ctx, src, plan)
        }
    }
    
    return nil
}
```

### 3. Block Implementation Pattern
```go
func (b *TorchBlock) Plan(ctx context.Context, src *SourceInfo, plan *Plan) error {
    // Check if already available
    if plan.HasProvider("torch") {
        return nil
    }
    
    // Build phase
    buildStage, err := plan.AddStage(PhaseFrameworkDeps, "Install PyTorch", "torch-install")
    if err != nil {
        return err
    }
    
    buildStage.Operations = append(buildStage.Operations, Exec{
        Command: "pip install torch==2.1.0+cpu",
    })
    buildStage.Provides = []string{"torch"}
    
    // Export phase
    exportStage, err := plan.AddStage(ExportPhaseRuntime, "Export PyTorch", "torch-export")
    if err != nil {
        return err
    }
    
    exportStage.Operations = append(exportStage.Operations, Copy{
        From: "torch-install",
        Src:  []string{"/usr/local/lib/python3.11/site-packages/torch*"},
        Dest: "/usr/local/lib/python3.11/site-packages/",
    })
    
    return nil
}
```

---

## Design Reasoning

### Why Stacks Over Global Block Registry?
**Problem**: How do we handle complex composition logic (e.g., "use uv OR pip, but not both")?

**Solution**: Stacks contain intelligent composition logic rather than just managing static lists. This allows for:
- Conditional block selection based on project analysis
- Complex decision trees (if pyproject.toml exists, use uv; else use pip)
- Domain-specific orchestration while keeping blocks focused

### Why Phases Over Flat Stage Lists?
**Problem**: How do blocks reference "the result of system dependency installation" without knowing specific stage names?

**Solution**: Explicit phase structure with logical boundaries:
- Blocks can reference phase results: `plan.GetPhaseResult(PhaseSystemDeps)`
- Clear build progression: base → system-deps → runtime → framework-deps → app-deps
- BuildKit handles the actual dependency resolution and parallelization

### Why Stage IDs + Names?
**Problem**: How do we balance human-readable logging with precise references?

**Solution**: Dual identification system:
- **ID**: Unique identifier for precise references between stages
- **Name**: Human-readable for logs and debugging
- Blocks set IDs explicitly, ensuring they can reference exactly what they need

### Why Squash Pattern Over LayerID Matching?
**Problem**: How do we guarantee each logical unit of work becomes exactly one layer?

**Solution**: BuildKit's `llb.Diff()` + `llb.Copy()` pattern:
- Each stage becomes one layer automatically
- No coordination needed between blocks
- Clear layer boundaries with explicit diff operations
- Eliminates the complexity of LayerID matching

### Why Base Image Metadata as Dependency Map?
**Problem**: How do we handle diverse pre-installed packages without rigid struct fields?

**Solution**: Flexible map structure similar to plan dependencies:
- Can represent any package without schema changes
- Supports different sources (apt, base-image, uv, etc.)
- Extensible metadata per package
- Consistent pattern with plan dependencies

---

## Implementation Checklist

### Core Infrastructure
- [ ] **Plan data structures** - Plan, Phase, Stage, BaseImage types
- [ ] **Plan methods** - AddStage, GetStage, GetPhaseResult with ID validation
- [ ] **Dependency resolution** - ResolveDependencies function with conflict handling
- [ ] **Base image metadata** - Mock implementation with Package map structure
- [ ] **Stack interface** - Detect and Plan methods
- [ ] **Block interface** - Detect, Dependencies, and Plan methods

### Python Stack Implementation
- [ ] **PythonStack** - Main orchestrator with block composition logic
- [ ] **BaseImageBlock** - Select build/runtime images based on resolved dependencies
- [ ] **PythonVersionBlock** - Emit Python version dependency from cog.yaml
- [ ] **AptBlock** - Install system packages from cog.yaml
- [ ] **UvBlock** - Handle uv-based Python dependency management
- [ ] **PipBlock** - Fallback Python dependency management
- [ ] **TorchBlock** - Install PyTorch with GPU/CPU variants
- [ ] **CudaBlock** - Handle CUDA dependencies and detection

### Build System Integration
- [ ] **Builder interface** - Abstract builder for plan execution
- [ ] **LLB Builder** - Convert plan to BuildKit LLB operations
- [ ] **Squash pattern implementation** - Use llb.Diff + llb.Copy for layer control
- [ ] **Platform handling** - Ensure linux/amd64 platform in all LLB operations

### Validation & Testing
- [ ] **Plan validation** - Check for cycles, missing inputs, duplicate IDs
- [ ] **Unit tests** - Individual block testing
- [ ] **Integration tests** - Full stack testing with real projects
- [ ] **Snapshot tests** - Verify plan generation consistency

### CLI Integration
- [ ] **Environment flag** - Enable cogpack behind feature flag
- [ ] **Plan command** - Generate and display plans without building
- [ ] **Build command** - Execute plans with LLB builder
- [ ] **Debug output** - JSON plan serialization for inspection

### Future Enhancements (Deferred)
- [ ] **Advanced dependency resolution** - Semantic versioning, conflict resolution
- [ ] **Multiple language stacks** - Node.js, Go, etc.
- [ ] **Remote caching** - Push/pull build layers
- [ ] **Secrets management** - Secure credential handling
- [ ] **Performance optimization** - Build time analysis and optimization
- [ ] **Comprehensive validation** - Advanced plan checking and error reporting

---

## Context for Future Sessions

### Key Files to Reference
- `CURSOR.md` - Overall project context and conventions
- `builder.cursor.md` - High-level roadmap and current focus
- `cogpack.deepdive.cursor.md` - Detailed design decisions and status
- `pkg/cogpack/` - Current implementation (rough scaffolding)
- `pkg/model/builder.go` - Reference LLB implementation patterns

### Critical Design Decisions Made
1. **Single stack per build** - First stack to detect wins, no multi-stack builds
2. **Explicit phase structure** - BuildPhases and ExportPhases as organized containers
3. **Block-managed stage IDs** - Blocks set unique IDs, plan validates uniqueness
4. **Squash pattern for layers** - Use llb.Diff + llb.Copy, not LayerID matching
5. **Dependency map pattern** - Consistent structure for plan deps and base image metadata
6. **Simplified package tracking** - Use string slices for Provides, expand later if needed

### Architecture Patterns to Follow
- **Plan as single source of truth** - All state flows through the plan object
- **Blocks stay decoupled** - No direct block-to-block communication
- **Stacks orchestrate intelligently** - Complex composition logic lives in stacks
- **Fail fast and clear** - Distinguish Cog faults from user faults
- **JSON serializable everywhere** - Support debugging and testing

### Testing Strategy
- **Unit test blocks individually** - Mock SourceInfo and Plan
- **Integration test stacks** - Real project fixtures
- **Snapshot test plans** - Ensure deterministic plan generation
- **End-to-end test builds** - Verify BuildKit LLB execution

### Common Pitfalls to Avoid
- **Don't couple blocks** - Each block should work independently
- **Don't hardcode stage names** - Use IDs and phase references
- **Don't skip validation** - Validate stage ID uniqueness and input resolution
- **Don't forget platform** - Include platform in all BuildKit operations
- **Don't over-optimize early** - Focus on correctness first, performance later

### Questions for Future Sessions
1. How should we handle complex dependency conflicts in ResolveDependencies?
2. What additional validation should we add to plan generation?
3. How should we structure the LLB builder to handle the squash pattern efficiently?
4. What base image metadata do we need beyond the current Package structure?
5. How should we handle build context and local file mounting in the builder?

---

## Summary

The cogpack system provides a clean, modular architecture for building ML model containers with precise layer control and intelligent dependency management. The design balances simplicity with extensibility, focusing on the Python stack initially while laying groundwork for future language support.

The key insight is that **Stacks orchestrate, Blocks implement, Plans specify, and Builders execute** - each component has a clear responsibility and clean interfaces. The phase-based structure provides logical organization while BuildKit handles the actual dependency resolution and optimization.

This design should support the complex requirements of ML model builds while remaining maintainable and extensible for future enhancements. 
