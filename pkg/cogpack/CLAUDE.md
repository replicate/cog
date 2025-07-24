# Cogpack Development Context

> **For LLM Assistants**: This is cogpack-specific development context. For general Cog development, see [../../CLAUDE.md](../../CLAUDE.md). For organized cogpack context, see [../../notes/](../../notes/).

> **Last Updated**: 2025-07-23  
> **Status**: Active Development

## Quick Start

### What is Cogpack?
Cogpack is an internal build system for packaging Cog models into OCI images using a modular **Stack + Blocks + Composer + Plan + Builder** architecture.

### Enable & Test
```bash
# Enable cogpack (feature flag)
export COGPACK=1

# Build and run a model
cog predict --input key=value

# View generated build plan
cog plan --json > plan.json

# Test cogpack functionality
cd test-integration/test_integration/fixtures/string-project
COGPACK=1 go run ../../../../cmd/cog predict --input s=hello
```

## Architecture

### Core Components
| Component | Purpose | Key File |
|-----------|---------|----------|
| **Stack** | Detects project type & orchestrates blocks | [`stacks/python/stack.go`](stacks/python/stack.go) |
| **Block** | Self-contained build operations | [`plan/block.go`](plan/block.go) |
| **Composer** | Plan assembly API with phases | [`plan/composer.go`](plan/composer.go) |
| **Plan** | Normalized build instructions | [`plan/plan.go`](plan/plan.go) |
| **Builder** | Executes plans via BuildKit | [`builder/buildkit.go`](builder/buildkit.go) |

### Build Flow
1. **Stack Detection** → Find matching stack (e.g., Python)
2. **Block Detection** → Stack identifies needed blocks  
3. **Dependency Collection** → Blocks declare requirements
4. **Dependency Resolution** → Resolve conflicts
5. **Plan Composition** → Blocks add stages via Composer
6. **Plan Execution** → BuildKit translates Plan → LLB → Image

*For detailed architecture, see [../../notes/DESIGN_DECISIONS.md](../../notes/DESIGN_DECISIONS.md)*

## Development

### Creating a New Block
```go
type Block interface {
    Name() string
    Detect(ctx context.Context, src *project.SourceInfo) (bool, error)
    Dependencies(ctx context.Context, src *project.SourceInfo) ([]*Dependency, error)
    Plan(ctx context.Context, src *project.SourceInfo, composer *Composer) error
}
```
**Reference**: [`stacks/python/uv.go`](stacks/python/uv.go)

### Key Patterns
- **Mount-based contexts**: Use `composer.AddContext()` with fs.FS mounts
- **Phase references**: Use `PhaseBuildComplete` for final build output
- **Provider checking**: Use `composer.HasProvider()` before installing packages

*For code patterns, see [../../notes/](../../notes/)*

### Testing
```bash
# Unit tests
go test ./plan/       # Composer & plan logic
go test ./stacks/python/  # Python stack components

# Integration tests (requires Docker)
COGPACK_INTEGRATION=1 go test ./builder/integration_test.go

# Specific test
go test ./stacks/python/ -run TestUVBlock_BasicDetection
```

*For testing strategy, see [../../notes/TESTING_STRATEGY.md](../../notes/TESTING_STRATEGY.md)*

### Phases
**Build**: `PhaseSystemDeps` → `PhaseRuntime` → `PhaseFrameworkDeps` → `PhaseAppDeps` → `PhaseAppSource` → `PhaseBuildComplete`

**Export**: `ExportPhaseBase` → `ExportPhaseRuntime` → `ExportPhaseApp` → `ExportPhaseConfig`

## Debugging

### Common Issues
1. **"stage ID already exists"**: Ensure unique IDs per block (prefix with block name)
2. **Input resolution failures**: Check stage/phase references exist in plan
3. **BuildKit mount errors**: Verify context added to composer before use

### Debug Commands
```bash
BUILDKIT_PROGRESS=plain cog build  # Verbose BuildKit output
COGPACK_DEBUG=1 cog build          # Future: detailed plan output
```

## Context References

- **Architecture & Design**: [../../notes/DESIGN_DECISIONS.md](../../notes/DESIGN_DECISIONS.md)
- **Completed Work**: [../../notes/COMPLETED_WORK.md](../../notes/COMPLETED_WORK.md)
- **Testing Strategy**: [../../notes/TESTING_STRATEGY.md](../../notes/TESTING_STRATEGY.md)
- **Roadmap**: [../../notes/ROADMAP.md](../../notes/ROADMAP.md)
- **General Cog Development**: [../../CLAUDE.md](../../CLAUDE.md)
- **Task Workflow**: [../../notes/WORKFLOW.md](../../notes/WORKFLOW.md)

## Current Status

### 🚧 Active Work
- Additional blocks (Apt, PyTorch, CUDA)
- Define base image metadata structure for dependency resolution

### 🎯 Next Priorities  
- GPU/CUDA Support: Handle CUDA version matrix and driver compatibility
- Schema.json Generation: Generate during build, embed in image labels
- Model Struct: Central model metadata instead of image tags

*For detailed roadmap, see [../../notes/ROADMAP.md](../../notes/ROADMAP.md)*

---

*For new work, use the task workflow: `/task name` → edit → `/work` → `/done`*