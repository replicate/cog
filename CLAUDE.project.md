# Cogpack Project Context

> **For LLM Assistants**: This is the primary context document for the cogpack build system. Please update this document as the project evolves, documenting decisions, patterns, and architectural changes.

The current task is written up in the `CURRENT TASK` section below. Please read it to understand the immediate task.

> **Last Updated**: 2025-07-17  
> **Primary Maintainer**: @md  
> **Status**: Active Development - Source Copy Functionality Completed

## Quick Start

### What is Cogpack?
Cogpack is an internal build system for packaging Cog models into OCI images using a modular **Stack + Blocks + Composer + Plan + Builder** architecture. It provides precise control over layers, reproducibility, and build ergonomics.

### Key Concepts in 30 Seconds
- **Stack**: Detects project type (e.g., Python) and orchestrates blocks
- **Block**: Self-contained build component (e.g., install PyTorch, copy source)
- **Composer**: API for building plans during assembly with phase organization
- **Plan**: Normalized build instructions for execution
- **Builder**: Executes plans (currently BuildKit)
- **Cogpack-Images**: Supporting project for optimizing build & runtime cache layers. If no base image is available cogpack will install required dependencies on demand

### Most Common Tasks
```bash
# Enable cogpack (feature flag)
export COGPACK=1

# Run a model, building it if necessary
cog predict --input key=value

# View generated plan along with composer state and metadata
cog plan --json > plan.json
```

## Architecture Overview

### Current Flow
```
┌─────────────────────────────────────────────────────────────┐
│                    Build Orchestration                      │
├─────────────────────────────────────────────────────────────┤
│ 1. Stack Detection    │ Find matching stack (e.g., Python)  │
│ 2. Block Detection    │ Stack detects which blocks needed  │
│ 3. Dependency Collection │ Blocks emit requirements        │
│ 4. Dependency Resolution │ Central resolver               │
│ 5. Plan Composition   │ Blocks add stages via Composer     │
│ 6. Plan Normalization │ Composer.Compose() → Plan         │
│ 7. Plan Execution     │ BuildKit translates Plan → LLB    │
└─────────────────────────────────────────────────────────────┘
```

### Core Components Reference

| Component | Purpose | Key Files |
|-----------|---------|-----------|
| **Stack** | Project type detection & orchestration | [`pkg/cogpack/stacks/python/stack.go`](pkg/cogpack/stacks/python/stack.go) |
| **Block** | Self-contained build operations | [`pkg/cogpack/plan/block.go`](pkg/cogpack/plan/block.go) |
| **Composer** | Plan assembly API with phases | [`pkg/cogpack/plan/composer.go`](pkg/cogpack/plan/composer.go) |
| **Plan** | Normalized build instructions | [`pkg/cogpack/plan/plan.go`](pkg/cogpack/plan/plan.go) |
| **Builder** | Plan execution (BuildKit) | [`pkg/cogpack/builder/buildkit.go`](pkg/cogpack/builder/buildkit.go) |

## Implementation Guide

### Creating a New Block

1. **Implement the Block interface**:
```go
type Block interface {
    Name() string
    Detect(ctx context.Context, src *project.SourceInfo) (bool, error)
    Dependencies(ctx context.Context, src *project.SourceInfo) ([]*Dependency, error)
    Plan(ctx context.Context, src *project.SourceInfo, composer *Composer) error
}
```

2. **Reference implementation**: [`pkg/cogpack/stacks/python/uv.go`](pkg/cogpack/stacks/python/uv.go)

3. **Key patterns**:
   - Use `composer.AddStage()` to add build operations
   - Check `composer.HasProvider()` before installing packages
   - Use mount-based contexts for file access (not MkFile)
   - Set `Provides` on stages to indicate what they install

### Working with Composer API

The Composer provides a clean API for building plans:

```go
// Add a stage with automatic input resolution
stage, err := composer.AddStage(plan.PhaseSystemDeps, "apt-install",
    plan.WithName("Install system packages"))

// Configure the stage
stage.AddOperation(plan.Exec{
    Command: "apt-get update && apt-get install -y curl",
})

// Access resolved dependencies
pythonDep, _ := composer.GetDependency("python")
```

See full API: [`pkg/cogpack/plan/composer.go`](pkg/cogpack/plan/composer.go:180-477)

### Phase Organization

Phases provide logical grouping of build operations, allowing blocks to insert stages at various points of the build without knowing the current plan structure. The phases are defined in `pkg/cogpack/plan/phases.go` and are currently a work in progress. Add or remove as needed.

#### Phase Resolution Behavior

When a stage references a phase as input (e.g., `plan.Input{Phase: plan.PhaseBuildComplete}`), the resolution works by looking through all **preceding phases** to find the latest stage. This means:

- `PhaseBuildComplete` (build.99-complete) is intentionally always empty
- When referenced, it resolves to the final stage from any preceding build phase
- This provides a stable reference to "the final build output" regardless of which phases actually contain stages
- Blocks can safely reference `PhaseBuildComplete` without knowing the specific phase structure

Example: If stages exist in `PhaseAppDeps` but not in `PhaseBuildComplete`, referencing `PhaseBuildComplete` will resolve to the last stage from `PhaseAppDeps`.

#### Build Phases
Build phases provide logical organization:
- `PhaseSystemDeps`: System packages (apt, yum)
- `PhaseRuntime`: Language runtime (Python, Node)
- `PhaseFrameworkDeps`: Heavy dependencies (PyTorch, TensorFlow)
- `PhaseAppDeps`: Application dependencies (requirements.txt)
- `PhaseAppSource`: Copy source code
- `PhaseBuildComplete`: Always empty, used as reference to final build output

#### Export Phases
Export phases build the runtime image:
- `ExportPhaseBase`: Runtime base setup
- `ExportPhaseRuntime`: Copy runtime dependencies
- `ExportPhaseApp`: Copy application
- `ExportPhaseConfig`: Final configuration

## Design Decisions

### Active Decisions

| Decision | Rationale | Date |
|----------|-----------|------|
| **Environment Variable Inheritance via LLB State** | Use `finalState.Env(ctx)` to extract accumulated environment variables from BuildKit LLB state for proper metadata flow from base images to final export config. Ensures runtime dependencies like Python are properly available. | 2025-07-16 |
| **Image Config Inspection for Base Images** | Implement image config inspection during Plan → LLB translation to extract environment variables from base images and apply them to image-based states. Required because BuildKit's llb.Diff() operations lose environment context from base images. | 2025-07-16 |
| **Explicit Wheel Filename Resolution** | Replace wildcard patterns (`*.whl`) with exact filename resolution using `dockerfile.WheelFilename()` for UV pip install commands. UV requires explicit wheel filenames and cannot resolve wildcards. | 2025-07-16 |
| **Operation Input Resolution** | Enables blocks to reference final build outputs using phase references in Copy operations. Allows UV block to copy from PhaseBuildComplete without knowing specific stage IDs. | 2025-07-16 |
| **Phase Pre-registration** | All phases are registered at composer creation to enable robust phase resolution. Prevents "phase does not exist" errors when referencing empty phases. | 2025-07-16 |
| **Separate Phase Resolution Methods** | `resolvePhaseInputStage()` and `resolvePhaseOutputStage()` provide clear semantics for different use cases. Input resolution gets last stage before a phase, output resolution gets accumulated result through a phase. | 2025-07-16 |
| **Composer Pattern** | Separates plan assembly (mutable) from execution (immutable). Provides clean API for blocks. | 2025-07-15 |
| **Mount-based Contexts** | Use fs.FS mounts instead of MkFile for flexibility. Enables embedded files, remote URLs. | 2025-07-14 |
| **Phase Organization** | Logical grouping of build operations. Enables auto input resolution within/across phases. | 2024-07-11 |
| **Single Stack per Build** | First matching stack wins. Simplifies orchestration, avoids conflicts. | 2024-07-11 |
| **Source Copy with Directory Removal** | Use `rm -rf /venv` before copying `/venv` from build to runtime to prevent BuildKit Copy operation from creating nested `/venv/venv` structure. BuildKit copies directories INTO existing directories rather than replacing them. | 2025-07-17 |

### Pending Decisions

| Topic | Options | Considerations |
|-------|---------|----------------|
| **Block Ordering** | Hard-coded vs dependency graph | Currently hard-coded in Python stack, may need DAG |
| **Remote Caching** | BuildKit cache vs custom | Deferred for MVP |
| **Multi-architecture** | Native BuildKit vs emulation | Linux/amd64 only for now |

## Current Work Item

This section tracks work that is currently in progress. Once a work item is completed, clear this and summarize in the "Current Status" section.

## Major Work Items

*These are significant areas of work that need exploration and implementation. Unlike "Pending Decisions" which have concrete options to evaluate, these items need investigation to understand the problem space first.*

### 🔍 Under Investigation

**Single Phase List Architecture**
- Current state: Separate buildPhases and exportPhases slices in Composer
- Questions: Should we merge these into a single ordered list of phases? This would simplify phase traversal and eliminate the need for separate build/export phase handling in previousPhase()
- Considerations: Would enable more natural phase ordering, but requires careful design to maintain build vs export semantics
- Next steps: Evaluate impact on existing code, design unified phase traversal API

**Cogpack Base Images**
- Current state: Hardcoded base images for a few python<>cuda combinations
- Questions: What metadata structure do we need for resolving requirements into base images? Once this is defined we can update the WIP image generation repo to export the required data. **Note**: Base image metadata should include whether a virtual environment exists at `/venv` to avoid conditional venv creation logic in UV block.
- Next steps: Define metadata structure, implement base image selection logic, include venv availability in metadata

**UV Projects**
- Current state: Old dockerfile builds use pyenv & `pip install` for dependencies. We want to move towards UV: this means projects that already use UV will work out of the box, and projects that use requirements.txt or requirements defined in the cog.yaml file will be converted to UV projects on the fly.
- Questions: How to convert existing requirements.txt to UV? How to handle UV project metadata?
- Next steps: Implement requirements conversion logic, define UV project metadata structure

**schema.json**
- Current state: Cog generates a schema.json file from the pydantic model. This is used to validate inputs and outputs.
- Questions: How do we generate this during the build? How do we make it available to the model in source code? How do we make it available as a label in the output image? How do we make it available to output of the build code?

**model struct**
- Current state: Models are built with a specific image tag, and the image tag is passed throughout cog to run, inspect, etc. Instead I want to create a `model.Model` struct that captures the model metadata with a central place to resolve models from image tags or image IDs. The rest of cog will then work with models instead of image tags, vastly simplifying the code and creating opportunities to improve the dev ux for end users.
- Questions: What do we need in the model struct? How do we resolve models from image tags or IDs? How do we make this available to the rest of cog?
- Next steps: Define model struct, implement resolution logic, update cog to use model struct

**Dependency Resolution Engine**
- Current state: Simple version matching and some hard-coded versions, no conflict resolution
- Questions: What metadata do we need to identify dependencies in base images? How do we handle version conflicts? How do we handle dependencies added by blocks that are consumed by other blocks? How do we handle differrent package management systems used in a single build (apt, pip)
- Next steps: Prototype with real-world requirements.txt files

**Layer Optimization**
- Current state: One layer per stage, no deduplication
- Questions: How to identify common layers? When to squash vs preserve?
- Next steps: Analyze typical model builds for optimization opportunities

**GPU/CUDA Support**
- Current state: Basic CUDA block exists but untested, probably doesn't work
- Questions: How to handle CUDA version matrix? Driver compatibility? Multi-GPU? This should be available from the cogpack base image repo that we download at build time, but we need to define the structure of the metadata so that it's designed for easy compatibility and use with the cogpack build system.
- Next steps: Audit existing Cog CUDA handling, design compatibility layer

**Torch/Tensorflow Block**
- Current state: Pytorch block exist as a placeholder, not implemented
- Questions: How to handle different PyTorch/Tensorflow versions? How to integrate with existing Python stack? How do we ensure that pytorch and tensorflow uses the correct CUDA libraries from the base image and DO NOT include thier own CUDA libraries? How do we ensure pytorch/tensorflow are installed with UV but isolated so that other models can reuse the same layer? How do we handle CPU vs GPU accelerators?
- Next steps: Implement PyTorch/TensorFlow blocks, define versioning strategy, define required metadata for base images and dependency resolution

**Implement Remaining Cog Build Behavior**
- Current state: Basic Python models now build and run successfully. Most advanced features from the old cog build system are not yet implemented.
- Questions: What advanced features are missing? How do we ensure compatibility with complex models? 
- Next Steps: Look over the old model building code and documentation to identify gaps for advanced features like custom dependencies, complex builds, etc.

### 🎯 Future Focus Areas

**Remote Build Contexts**
- Support for git URLs, HTTP archives as build contexts
- Streaming large model files during build
- Authentication for private repositories

**Non-python Stack**
- Implement a basic stack in Javascript to validate the design can support future direction

### 📝 Technical Debt

**Context Conversion Efficiency**
- Current: fs.FS → temp dir → fsutil.FS conversion is inefficient
- Impact: Slower builds, disk usage
- Fix: Direct fs.FS to fsutil.FS adapter

**Test Coverage Gaps**
- Integration tests for GPU builds
- Error path testing in Builder
- Multi-stage build scenarios


## Code Patterns

### Pattern: Block with Mount Context

```go
func (b *CogWheelBlock) Plan(ctx context.Context, src *project.SourceInfo, composer *plan.Composer) error {
    // Add context to composer
    composer.AddContext("wheel-context", &plan.BuildContext{
        Name:        "wheel-context",
        SourceBlock: "cog-wheel",
        Description: "Cog wheel file for installation",
        FS:          dockerfile.CogEmbed, // fs.FS implementation
    })

    // Use mount in operations
    stage, _ := composer.AddStage(plan.PhaseAppDeps, "cog-wheel")
    stage.AddOperation(plan.Exec{
        Command: "pip install /mnt/wheel/*.whl",
        Mounts: []plan.Mount{{
            Source: plan.Input{Local: "wheel-context"},
            Target: "/mnt/wheel",
        }},
    })
    return nil
}
```

### Pattern: Cross-Phase File Copy

```go
func (b *UvBlock) Plan(ctx context.Context, src *project.SourceInfo, composer *plan.Composer) error {
    // Build stage creates venv
    buildStage, _ := composer.AddStage(plan.PhaseAppDeps, "uv-venv")
    buildStage.AddOperation(plan.Exec{
        Command: "uv venv /venv --python 3.11",
    })

    // Export stage copies venv from final build output
    exportStage, _ := composer.AddStage(plan.ExportPhaseRuntime, "copy-venv")
    exportStage.AddOperation(plan.Copy{
        From: plan.Input{Phase: plan.PhaseBuildComplete}, // Resolves to final build stage
        Src:  []string{"/venv"},
        Dest: "/venv",
    })
    return nil
}
```

### Pattern: Dependency Resolution

```go
func (s *PythonStack) Plan(ctx context.Context, src *project.SourceInfo, composer *plan.Composer) error {
    // 1. Detect active blocks
    blocks := plan.DetectBlocks(ctx, src, availableBlocks)
    
    // 2. Collect dependencies
    var allDeps []*plan.Dependency
    for _, block := range blocks {
        deps, _ := block.Dependencies(ctx, src)
        allDeps = append(allDeps, deps...)
    }
    
    // 3. Resolve conflicts
    resolved, _ := plan.ResolveDependencies(ctx, allDeps)
    composer.SetDependencies(resolved)
    
    // 4. Let blocks build
    for _, block := range blocks {
        block.Plan(ctx, src, composer)
    }
}
```

## Testing Strategy

- Create unit tests for individual components that verify behavior in isolation using deterministic inputs and outputs.
- Use integration tests to verify end-to-end behavior with external dependencies like Docker and BuildKit.
- Use testify/assert and testify/require for assertions.
- Prefer table-driven tests for clarity and coverage.
- Prefer grouping related tests into subtests for better organization.
- Use helpers in modern `testing` packages such as `t.Context()`, `t.TempDir()`, `t.Cleanup()`, and `testing/synctest`.

### Unit Tests
- Stacks, Blocks, Composer, and Plan are all in memory representations of a build. They are deterministic and idempotent. Verify behavior through unit tests.
- Each component should have its own tests that can be unit tested independently witout external dependencies.


- Input for tests can be either fixtures loaded in a temp directory with SourceInfo or in-memory
- Verify Composer API behavior
- Example: [`pkg/cogpack/plan/composer_test.go`](pkg/cogpack/plan/composer_test.go)

### Integration Tests
- Integration tests are any test requiring external dependencies like Docker and BuildKit or that take more than a few seconds to run. These should be opt-in with the `INTEGRATION` environment variable.
- Integration tests for cogpack should verify source code input and output.
- Example: [`pkg/cogpack/builder/integration_test.go`](pkg/cogpack/builder/integration_test.go)

### Test Fixtures
- Location: `pkg/cogpack/testdata/`
- Example projects for specific scenarios
- For the simplest projects, use in-memory `SourceInfo` with `project.NewSourceInfo()`
- For more complex projects, use fixtures to load from `testdata/` directory
- Fixtures should be realistic examples of projects that would be built with cogpack
- Use `t.TempDir()` to create temporary directories for tests that need file system access. DO NOT run tests directly on fixture source code.

## Debugging & Troubleshooting

### Common Issues

1. **"stage ID already exists"**
   - Cause: Duplicate stage IDs across blocks
   - Fix: Ensure unique IDs, consider prefixing with block name

2. **Input resolution failures**
   - Cause: Referencing stages/phases that don't exist
   - Debug: Check Composer.Compose() error, validate stage ordering

3. **BuildKit mount errors**
   - Cause: Missing context or incorrect context name
   - Debug: Verify context added to composer before use

### Debug Commands
```bash
# View generated plan (when implemented)
COGPACK_DEBUG=1 cog build

# BuildKit debug output
BUILDKIT_PROGRESS=plain cog build

# Run build & run predict on a real model. This is a simple echo model that just returns the input string.
cd ./test-integration/test_integration/fixtures/string-project && COGPACK=1 go run ../../../../cmd/cog predict --input s=hello`
```

## Current Status

### CURRENT TASK

No current task - ready for next feature work.

### ✅ Completed
- Composer API with phase organization
- Python stack with basic blocks (Uv, CogWheel, Python)
- Mount-based context system
- BuildKit integration with LLB translation
- Input resolution (auto, phase, stage references)
- CLI integration: MVP `cog plan` command (outputs plan metadata and normalized plan as JSON)
- Operation input resolution: Copy, Add, and Mount operations now support phase references that resolve to concrete stages during plan composition
- Phase pre-registration: All standard phases are registered at composer creation to enable robust phase resolution
- UV block cross-phase references: UV block can now copy venv from build to export phases using `PhaseBuildComplete` reference
- **Environment Variable Inheritance**: Implemented complete solution for preserving ENV/PATH from base images through BuildKit LLB translation to final export image config. Fixes runtime dependency resolution (e.g., `python` executable not found)
- **Wheel Installation Fix**: Resolved UV pip install wildcard issue by using explicit wheel filename resolution instead of `*.whl` patterns
- **Image Config Metadata Flow**: Added image config inspection and proper metadata accumulation during Plan → LLB translation to ensure base image environment variables are inherited by build stages
- **Source Copy Functionality**: Completed implementation of SourceCopyBlock to copy application source code to runtime image. Fixed nested venv issue with proper BuildKit Copy operation semantics using `rm -rf /venv` before copy to prevent directory nesting. Python models now build and run successfully end-to-end.

### 🚧 In Progress
- Additional blocks (Apt, Torch, CUDA)
- Define base image metadata structure and metadata needed for dependency resolution

### 📋 Planned
- Implement remaining blocks (TensorFlow, CUDA)
- ensure phases can't create cycles in the plan

## Maintenance Instructions

### For LLM Assistants

When working on cogpack:

1. **Update this document** when:
   - Adding new architectural patterns`
   - Making design decisions
   - Discovering important implementation details
   - Finding common pitfalls

2. **Document decisions** using this format:
   ```markdown
   ### [Decision Title]
   **Date**: YYYY-MM-DD
   **Context**: What problem needed solving?
   **Decision**: What was chosen?
   **Rationale**: Why this approach?
   **Trade-offs**: What are the downsides?
   ```

3. **Keep references current**:
   - Link to actual code files, not inline examples
   - Update file paths if code moves
   - Remove references to deleted code

4. **Maintain clarity**:
   - Assume reader is new to project
   - Explain "why" not just "what"
   - Include examples from real code

5 **Document completed work**:
   - Append a new line to the `### ✅ Completed` section of the Current Status section with a description of the work that was completed.
   - Remove references to the work item from other sections, including `### 🚧 In Progress`, `### 📋 Planned`, `### 🔍 Under Investigation`, and `### 🎯 Future Focus Areas`.
   - Clear the `CURRENT TASK` section
   - Look over the document to ensure that it's up to date, accurate, and no longer referencing work that has been completed.

### Review Checklist

Before major changes:
- [ ] Updated relevant sections of this document
- [ ] Added/updated test coverage
- [ ] Verified no duplicate stage IDs
- [ ] Checked BuildKit integration still works
- [ ] Updated status section if needed

## Questions & Contact

- **Technical questions**: Reference code or create TODO in relevant file
- **Design questions**: Add to "Pending Decisions" section
- **Bugs/Issues**: Document in code with clear TODO markers

---

*This document is the source of truth for cogpack architecture. Keep it current.*
