# Cogpack – LLM Assistant Context & Working Document

> **For LLM Assistants**: This file provides complete context for the cogpack build system project. Please continue to refine and update this document as we work together, keeping it current with implementation progress and design decisions.

> **Last Updated**: 2025-01-27  
> **Status**: Design Complete, Implementation In Progress  

## Table of Contents
1. [Mission & Objectives](#mission--objectives)
2. [System Overview](#system-overview)
3. [Core Architecture](#core-architecture)
4. [Implementation Status](#implementation-status)
5. [Design Decisions](#design-decisions)
6. [Current Working Checklist](#current-working-checklist)
7. [Code Patterns & Conventions](#code-patterns--conventions)
8. [Context for LLM Assistants](#context-for-llm-assistants)

---

## Mission & Objectives

### Primary Mission
Package Cog models into OCI images using a **stack + blocks + plan + builder** architecture that gives us precise control over layers, reproducibility, and ergonomics. The system is internal-only for the foreseeable future but must be solid enough to replace the existing Cog build path.

### Success Criteria
Produce a *fully functional Python stack* covering:
- ✅ CPU-only "hello-world" model (string-project fixture)
- ✅ GPU + PyTorch
- ✅ GPU + TensorFlow  
- ✅ CPU + PyTorch
- ✅ CPU + TensorFlow

Success = images build & run via `cog predict`, under env-var flag.

### Guiding Principles
1. 📉 **Ruthless scope** – do what we need *now*, defer everything else with TODOs
2. 🧩 **Modular** – Stacks & Blocks are loosely coupled; Plan vs. Builder decoupled
3. 🛠 **Ease of hacking** – Internal engineers should grok & extend quickly
4. 🧪 **Tests from day 1** – Unit per Block, snapshot plans, end-to-end builds
5. 📜 **Docs live with code** – keep this file & package README up-to-date

---

## System Overview

### Architecture Flow
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

### Core Components
| Term | Definition |
|------|------------|
| **Stack** | Detects if it can handle the project and orchestrates an ordered list of Blocks. *Only one Stack wins per build.* |
| **Block** | A self-contained "lego brick" that may: Detect, emit dependency constraints, append build/export stages, etc. |
| **Plan** | The result of Stack + Blocks: a set of `Stage`s (≈ Dockerfile stages) with `Op`s (`Exec`, `Copy`, …) plus resolved dependencies. |
| **Builder** | Executes a Plan (target: BuildKit LLB). |
| **Cogpack Image** | The base image selected/resolved for the build (formerly "base image"). |

---

## Core Architecture

### Data Structures

#### Plan Structure
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

#### Phase Structure
```go
type Phase struct {
    Name   StagePhase `json:"name"`   // PhaseSystemDeps, PhaseFrameworkDeps, etc.
    Stages []Stage    `json:"stages"` // all stages within this phase
}
```

#### Stage Structure
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

### Workflow Pattern

#### Main Orchestration
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

#### Stack Orchestration (Python Example)
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

---

## Implementation Status

### Current Focus
**Produce a functional Python stack** that can build CPU and GPU models with PyTorch/TensorFlow support.

### Completed ✅
- System architecture design
- Data structure definitions
- Workflow patterns established
- Core interfaces defined

### In Progress 🔄
- Core Plan data structures implementation
- Python Stack orchestration logic
- Basic Block implementations (UvBlock, AptBlock, etc.)
- BuildKit LLB Builder integration

### Planned 📋
- Dependency resolution engine
- Base image metadata system
- Complete block implementations
- Integration testing
- CLI integration behind feature flag

---

## Design Decisions

### Key Architectural Decisions ✅

| Decision | Rationale |
|----------|-----------|
| **Single stack per build** | First stack to detect wins, no multi-stack builds. Simplifies orchestration. |
| **Explicit phase structure** | BuildPhases and ExportPhases as organized containers. Provides logical build progression. |
| **Block-managed stage IDs** | Blocks set unique IDs, plan validates uniqueness. Enables precise stage referencing. |
| **Squash pattern for layers** | Use llb.Diff + llb.Copy, not LayerID matching. Guarantees one layer per logical unit. |
| **Dependency map pattern** | Consistent structure for plan deps and base image metadata. Flexible and extensible. |

### Current Assumptions 🟡

| Topic | Assumption | Status |
|-------|------------|--------|
| Block ordering | Python stack hard-codes ordered slice of Blocks | May evolve to dependency-based ordering |
| Dependency resolution | Central multi-pass solver with semver constraints | Simple implementation first |
| Base image selection | From `pkg/base_images` with resolved dependencies | May need compatibility matrix |
| Error handling | Fail fast and clear, distinguish Cog vs user faults | Basic implementation, expand later |

### Deferred Decisions ⚠️

| Topic | Deferred Because |
|-------|------------------|
| Block mutability vs. append-only | Need implementation experience |
| Ownership of LayerID & artifact naming | Will be resolved during builder work |
| Secrets API surface | Basic implementation sufficient initially |
| Plan schema versioning | Internal use only for now |
| Remote caching | Out of scope for MVP |

---

## Current Working Checklist

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

---

## Code Patterns & Conventions

### Block Implementation Pattern
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

### Key Patterns to Follow
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

---

## Context for LLM Assistants

### This Document's Purpose
This file serves as the primary context for LLM assistants working on the cogpack system. It should be:
- **Continuously updated** as implementation progresses
- **Refined** based on new insights and decisions
- **Expanded** with new architectural patterns and conventions
- **Maintained** to reflect current implementation status

### Key Files to Reference
- `CURSOR.md` - Overall Cog project context and conventions
- `pkg/cogpack/` - Current implementation (rough scaffolding)
- `pkg/model/builder.go` - Reference LLB implementation patterns
- `test-integration/test_integration/fixtures/` - Test project examples

### Critical Implementation Notes
1. **Start with core data structures** - Plan, Phase, Stage types are foundational
2. **Implement Python stack first** - Focus on one complete stack before expanding
3. **Use BuildKit LLB backend** - Target precise layer control through squash pattern
4. **Validate early and often** - Stage ID uniqueness, input resolution, dependency cycles
5. **Follow existing Cog patterns** - Use similar error handling, logging, and testing approaches

### Common Pitfalls to Avoid
- **Don't couple blocks** - Each block should work independently
- **Don't hardcode stage names** - Use IDs and phase references
- **Don't skip validation** - Validate stage ID uniqueness and input resolution
- **Don't forget platform** - Include platform in all BuildKit operations
- **Don't over-optimize early** - Focus on correctness first, performance later

### Questions for Future Development
1. How should we handle complex dependency conflicts in ResolveDependencies?
2. What additional validation should we add to plan generation?
3. How should we structure the LLB builder to handle the squash pattern efficiently?
4. What base image metadata do we need beyond the current Package structure?
5. How should we handle build context and local file mounting in the builder?

### Update Guidelines for LLM Assistants
When working on cogpack:
1. **Update the checklist** - Mark items complete (✅) as implemented
2. **Record design decisions** - Add new decisions to the design decisions section
3. **Update implementation status** - Move items between Completed/In Progress/Planned
4. **Add new patterns** - Document new code patterns and conventions discovered
5. **Note blockers** - Add any implementation blockers or questions to the questions section
6. **Refine architecture** - Update data structures and workflows based on implementation learnings

---

**Remember**: This system replaces the existing Cog build path, so it must be solid, maintainable, and extensible while remaining focused on the Python stack initially.
