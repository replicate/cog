# Cogpack – LLM Assistant Context & Working Document

> **For LLM Assistants**: This file provides complete context for the cogpack build system project. Please continue to refine and update this document as we work together, keeping it current with implementation progress and design decisions.

> **Last Updated**: 2025-07-11  
> **Status**: Core Implementation Complete, Mount-Based Context System Implemented  

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
    Dependencies  map[string]*Dependency   `json:"dependencies"`  // resolved versions
    BaseImage     *BaseImage              `json:"base_image"`    // build/runtime images
    BuildPhases   []*Phase                `json:"build_phases"`  // organized build work
    ExportPhases  []*Phase                `json:"export_phases"` // runtime image assembly
    Export        *ExportConfig           `json:"export"`        // final image config
    Contexts      map[string]*BuildContext `json:"contexts"`      // build contexts for mounting
}
```

#### BuildContext Structure
```go
type BuildContext struct {
    Name        string            `json:"name"`         // context name for referencing
    SourceBlock string            `json:"source_block"` // which block created this context
    Description string            `json:"description"`  // human-readable description
    Metadata    map[string]string `json:"metadata"`     // debug annotations
    FS          fs.FS             `json:"-"`            // the actual filesystem (not serialized)
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

#### Input Structure
```go
type Input struct {
    Image string `json:"image,omitempty"` // external image reference
    Stage string `json:"stage,omitempty"` // reference to another stage
    Local string `json:"local,omitempty"` // build context name
    URL   string `json:"url,omitempty"`   // HTTP/HTTPS URL for files
}
```

#### Mount Structure
```go
type Mount struct {
    Source Input  `json:"source"` // mount source (supports Input types)
    Target string `json:"target"` // mount path in container
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
**Completed functional Python stack** with mount-based context system for enhanced build flexibility and cog wheel installation.

### Completed ✅
- ✅ System architecture design
- ✅ Data structure definitions (Plan, Phase, Stage, BuildContext)
- ✅ Workflow patterns established
- ✅ Core interfaces defined
- ✅ **Mount-based context system** - Generic fs.FS mounting with BuildKit integration
- ✅ **Python Stack orchestration** - Complete PythonStack with block composition
- ✅ **Core Block implementations** - UvBlock, CogWheelBlock with context support
- ✅ **BuildKit LLB Builder integration** - Full LLB translation with mount support
- ✅ **Context management** - Generic ContextFS for directory and fs.FS contexts
- ✅ **Plan validation** - Comprehensive validation including context references
- ✅ **Integration testing** - End-to-end BuildKit integration tests passing
- ✅ **Cog wheel installation** - Mount-based wheel installation using embedded fs

### In Progress 🔄
- Additional Block implementations (TorchBlock, AptBlock, etc.)
- Enhanced dependency resolution engine

### Planned 📋
- Complete block implementations for GPU/CUDA support
- Enhanced base image metadata system
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
| **Mount-based context system** | Generic fs.FS mounting instead of MkFile operations. Enables flexible file/wheel installation. |
| **BuildContext on Plan** | Contexts stored directly in Plan with fs.FS. Centralized context management and validation. |
| **Extended Input type** | Input supports Image, Stage, Local, and URL sources. Unified interface for all source types. |
| **Generic ContextFS** | Single ContextFS handles both directories and fs.FS. Flexible context creation from any source. |
| **Consolidated validation** | Single ValidatePlan function handles all validation. Comprehensive plan verification in one place. |

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
- [x] ✅ **Plan data structures** - Plan, Phase, Stage, BaseImage, BuildContext types
- [x] ✅ **Plan methods** - AddStage, GetStage, GetPhaseResult with ID validation
- [x] ✅ **Dependency resolution** - ResolveDependencies function with conflict handling
- [x] ✅ **Base image metadata** - Mock implementation with Package map structure
- [x] ✅ **Stack interface** - Detect and Plan methods
- [x] ✅ **Block interface** - Detect, Dependencies, and Plan methods
- [x] ✅ **Mount-based context system** - Generic fs.FS mounting with BuildKit integration
- [x] ✅ **Input type extensions** - Support for Image, Stage, Local, URL sources
- [x] ✅ **Context validation** - Comprehensive plan validation including context references

### Python Stack Implementation
- [x] ✅ **PythonStack** - Main orchestrator with block composition logic
- [x] ✅ **BaseImageBlock** - Select build/runtime images based on resolved dependencies
- [x] ✅ **PythonBlock** - Emit Python version dependency and installation
- [ ] **AptBlock** - Install system packages from cog.yaml
- [x] ✅ **UvBlock** - Handle uv-based Python dependency management
- [ ] **PipBlock** - Fallback Python dependency management
- [ ] **TorchBlock** - Install PyTorch with GPU/CPU variants
- [x] ✅ **CogWheelBlock** - Mount-based cog wheel installation with embedded fs
- [ ] **CudaBlock** - Handle CUDA dependencies and detection

### Build System Integration
- [x] ✅ **Builder interface** - Abstract builder for plan execution
- [x] ✅ **LLB Builder** - Convert plan to BuildKit LLB operations with mount support
- [x] ✅ **Context processing** - Generic context conversion from fs.FS to fsutil.FS
- [x] ✅ **Mount translation** - LLB mount creation from plan mount specifications
- [x] ✅ **Platform handling** - Ensure linux/amd64 platform in all LLB operations
- [x] ✅ **Image export** - Proper "moby" exporter for local Docker daemon

### Validation & Testing
- [x] ✅ **Plan validation** - Check for cycles, missing inputs, duplicate IDs, context references
- [x] ✅ **Unit tests** - Individual block testing with context support
- [x] ✅ **Integration tests** - Full stack testing with BuildKit integration
- [x] ✅ **Context tests** - ContextFS and mount system testing
- [x] ✅ **End-to-end tests** - Complete build pipeline validation

### CLI Integration
- [x] ✅ **Environment flag** - Enable cogpack behind COGPACK=1 feature flag
- [x] ✅ **Build command** - Execute plans with LLB builder (via BuildWithDocker)
- [x] ✅ **Debug output** - JSON plan serialization for inspection
- [ ] **Plan command** - Generate and display plans without building

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
        From: Input{Stage: "torch-install"},
        Src:  []string{"/usr/local/lib/python3.11/site-packages/torch*"},
        Dest: "/usr/local/lib/python3.11/site-packages/",
    })
    
    return nil
}
```

### Mount-Based Context Pattern
```go
func (b *CogWheelBlock) Plan(ctx context.Context, src *SourceInfo, plan *Plan) error {
    // Initialize contexts map if needed
    if plan.Contexts == nil {
        plan.Contexts = make(map[string]*BuildContext)
    }

    // Add context to plan with embedded filesystem
    plan.Contexts["wheel-context"] = &BuildContext{
        Name:        "wheel-context",
        SourceBlock: "cog-wheel",
        Description: "Cog wheel file for installation",
        Metadata: map[string]string{
            "type": "embedded-wheel",
        },
        FS: dockerfile.CogEmbed, // fs.FS implementation
    }

    stage, err := plan.AddStage(PhaseAppDeps, "cog-wheel", "cog-wheel")
    if err != nil {
        return err
    }

    // Use mount to access wheel files
    stage.Operations = append(stage.Operations, Exec{
        Command: "/uv/uv pip install --python /venv/bin/python /mnt/wheel/embed/*.whl 'pydantic>=1.9,<3'",
        Mounts: []Mount{
            {
                Source: Input{Local: "wheel-context"},
                Target: "/mnt/wheel",
            },
        },
    })

    return nil
}
```

### Key Patterns to Follow
- **Plan as single source of truth** - All state flows through the plan object
- **Blocks stay decoupled** - No direct block-to-block communication
- **Stacks orchestrate intelligently** - Complex composition logic lives in stacks
- **Mount-based file access** - Use contexts and mounts instead of MkFile operations
- **Contexts on Plan** - Store BuildContext directly in Plan.Contexts map
- **Extended Input types** - Use Input struct for all source specifications (Stage, Image, Local, URL)
- **Generic context creation** - Use ContextFS for both directories and fs.FS interfaces
- **Consolidated validation** - Single ValidatePlan function for all validation needs
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
5. ✅ ~~How should we handle build context and local file mounting in the builder?~~ **SOLVED**: Mount-based context system implemented
6. How should we optimize the fs.FS to fsutil.FS conversion to avoid temp directory creation?
7. What additional context types (beyond directory and fs.FS) might we need in the future?
8. How should we handle context cleanup and lifecycle management in long-running builds?

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
