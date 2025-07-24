l# Cogpack Build System

> **Status**: Active Development | **Maintainer**: @md | **Updated**: 2025-01-23

Cogpack is an internal build system for packaging Cog models into OCI images using a modular **Stack + Blocks + Composer + Plan + Builder** architecture. It provides precise control over layers, reproducibility, and improved build ergonomics.

## Quick Start

```bash
# Enable cogpack (feature flag)
export COGPACK=1

# Build and run a model
cog predict --input key=value

# View generated build plan
cog plan --json > plan.json
```

## Architecture

### Core Components

| Component | Purpose | Key File |
|-----------|---------|----------|
| **Stack** | Detects project type & orchestrates blocks | [`stacks/python/stack.go`](pkg/cogpack/stacks/python/stack.go) |
| **Block** | Self-contained build operations | [`plan/block.go`](pkg/cogpack/plan/block.go) |
| **Composer** | Plan assembly API with phases | [`plan/composer.go`](pkg/cogpack/plan/composer.go) |
| **Plan** | Normalized build instructions | [`plan/plan.go`](pkg/cogpack/plan/plan.go) |
| **Builder** | Executes plans via BuildKit | [`builder/buildkit.go`](pkg/cogpack/builder/buildkit.go) |

### Build Flow
1. **Stack Detection** → Find matching stack (e.g., Python)
2. **Block Detection** → Stack identifies needed blocks  
3. **Dependency Collection** → Blocks declare requirements
4. **Dependency Resolution** → Resolve conflicts
5. **Plan Composition** → Blocks add stages via Composer
6. **Plan Execution** → BuildKit translates Plan → LLB → Image

## Development Guide

### Creating a New Block

Implement the Block interface:
```go
type Block interface {
    Name() string
    Detect(ctx context.Context, src *project.SourceInfo) (bool, error)
    Dependencies(ctx context.Context, src *project.SourceInfo) ([]*Dependency, error)
    Plan(ctx context.Context, src *project.SourceInfo, composer *Composer) error
}
```

**Reference implementation**: [`stacks/python/uv.go`](pkg/cogpack/stacks/python/uv.go)

### Phases

**Build phases**: `PhaseSystemDeps` → `PhaseRuntime` → `PhaseFrameworkDeps` → `PhaseAppDeps` → `PhaseAppSource` → `PhaseBuildComplete`

**Export phases**: `ExportPhaseBase` → `ExportPhaseRuntime` → `ExportPhaseApp` → `ExportPhaseConfig`

### Key Patterns
- **Mount-based contexts**: Use `composer.AddContext()` with fs.FS mounts
- **Phase references**: Use `PhaseBuildComplete` for final build output
- **Provider checking**: Use `composer.HasProvider()` before installing packages

*For detailed patterns, see [notes/PATTERNS.md](notes/PATTERNS.md)*

## Testing

Our three-layer testing strategy ensures framework reliability and enables confident changes:

### 1. Unit Tests (Fast, Many)
Test framework components in isolation - Composer API, phase resolution, block detection.
```bash
go test ./pkg/cogpack/plan       # Test composer & plan logic
go test ./pkg/cogpack/stacks/python  # Test Python stack components
```

### 2. Integration Tests - Source → Plan (Medium, Some)  
Verify source analysis produces correct build plans without Docker complexity.
```bash
go test ./pkg/cogpack/stacks/python -run TestPythonStack_PlanGeneration
```

### 3. Integration Tests - Plan → Image (Slow, Few)
Test that build plans actually produce correct images using real Docker/BuildKit.
```bash
COGPACK_INTEGRATION=1 go test ./pkg/cogpack/builder/integration_test.go
```

**Philosophy**: If individual build plan pieces produce correct images, we can trust any build plan can be built. This frees up complexity and reduces risk when changing business logic.

**Best examples**: 
- Unit tests: [`pkg/cogpack/plan/composer_test.go`](pkg/cogpack/plan/composer_test.go)
- Integration tests: [`pkg/cogpack/builder/integration_test.go`](pkg/cogpack/builder/integration_test.go)

*For detailed testing strategy, see [notes/TESTING_STRATEGY.md](notes/TESTING_STRATEGY.md)*

## Current Development

### 🚧 Active Work
- Additional blocks (Apt, PyTorch, CUDA)
- Define base image metadata structure for dependency resolution

### 🎯 Next Priorities
1. **GPU/CUDA Support**: Handle CUDA version matrix and driver compatibility
2. **Schema.json Generation**: Generate during build, embed in image labels
3. **Model Struct**: Central model metadata instead of image tags
4. **Dependency Resolution Engine**: Advanced conflict resolution across package managers

*For detailed roadmap, see [notes/ROADMAP.md](notes/ROADMAP.md)*

## Troubleshooting

### Common Issues
1. **"stage ID already exists"**: Ensure unique IDs per block (prefix with block name)
2. **Input resolution failures**: Check stage/phase references exist in plan
3. **BuildKit mount errors**: Verify context added to composer before use

### Debug Commands
```bash
BUILDKIT_PROGRESS=plain cog build  # Verbose BuildKit output
COGPACK_DEBUG=1 cog build          # Future: detailed plan output
```

## Contributing

### When Making Changes
1. **Update this doc** for architectural changes
2. **Add tests** for new functionality
3. **Document decisions** in `notes/devlog/YYYY-MM-DD-feature-name.md`
4. **Mark TODOs** in code, not documentation

### For Context
- **Design decisions**: [notes/DESIGN_DECISIONS.md](notes/DESIGN_DECISIONS.md)
- **Completed work**: [notes/COMPLETED_WORK.md](notes/COMPLETED_WORK.md)
- **Session logs**: [notes/devlog/](notes/devlog/)
- **Main Cog project**: [CLAUDE.md](CLAUDE.md)
- **Cogpack development**: [pkg/cogpack/CLAUDE.md](pkg/cogpack/CLAUDE.md)

---

*Last significant update: 2025-01-23 - Documentation restructure*
