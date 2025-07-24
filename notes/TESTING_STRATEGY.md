# Testing Strategy

## Philosophy

The cogpack build process is complex, so we break it down into testable units to ensure reliability and enable confident refactoring. We are not religious about TDD, but use it where practical.

## Three-Layer Testing Approach

### 1. Unit Tests
**Purpose**: Test specific pieces of code or functionality in isolation  
**Target**: Individual components with deterministic inputs/outputs  
**Examples**: 
- Composer API behavior ([`pkg/cogpack/plan/composer_test.go`](../pkg/cogpack/plan/composer_test.go))
- Phase resolution logic
- Input/output transformations
- Block detection logic

```bash
# Run all unit tests
go test ./pkg/cogpack/...

# Run specific test
go test ./pkg/cogpack/plan -run TestComposer_Phases
```

### 2. Integration Tests - Source → Build Plan
**Purpose**: Process real input and verify the output plan structure  
**Target**: Source analysis → Plan generation pipeline  
**Benefits**: Tests the "business logic" without Docker complexity

**Examples**:
- Python project detection generates correct UV blocks
- Requirements.txt parsing produces expected dependency stages
- Phase organization is correct for different project types

```bash
# These should NOT require Docker/BuildKit
go test ./pkg/cogpack/stacks/python -run TestPythonStack_PlanGeneration
```

### 3. Integration Tests - Build Plan → Image
**Purpose**: Verify build plans actually produce correct images using real Docker/BuildKit  
**Target**: Plan execution via BuildKit → final image  
**Infrastructure**: Uses [`pkg/docker/testenv/`](../pkg/docker/testenv/) utilities

**Best example**: [`pkg/cogpack/builder/integration_test.go`](../pkg/cogpack/builder/integration_test.go)

```bash
# Requires Docker/BuildKit
COGPACK_INTEGRATION=1 go test ./pkg/cogpack/builder/integration_test.go
```

## Strategic Benefits

### Foundation Trust
- **If integration tests show individual build plan pieces → correct image**, we can trust any build plan can be built
- **This frees up complexity** from other parts because correct input → build plan conversion can be trusted to become the correct image
- **Reduces risk** when making changes to Python model logic or other "business logic"

### Testing Pyramid
```
┌─────────────────────────────────────┐
│     E2E Tests (Few, Slow)           │  ← Full cogpack pipeline
├─────────────────────────────────────┤
│  Integration Tests (Some, Medium)   │  ← Plan→Image & Source→Plan  
├─────────────────────────────────────┤
│    Unit Tests (Many, Fast)          │  ← Framework components
└─────────────────────────────────────┘
```

## Implementation Guidelines

### Unit Tests
- **Deterministic & Isolated**: No external dependencies (Docker, filesystem, network)
- **Framework focused**: Test Composer, Plan, Phase resolution, Block interfaces
- **Fast execution**: Should run in milliseconds
- **Use in-memory fixtures**: `project.NewSourceInfo()` for simple cases
- **Table-driven tests**: For multiple scenarios

```go
func TestBlockDetection(t *testing.T) {
    src := project.NewSourceInfo()
    src.SetFile("requirements.txt", "torch==1.9.0")
    
    block := &UvBlock{}
    detected, err := block.Detect(context.Background(), src)
    
    assert.NoError(t, err)
    assert.True(t, detected)
}
```

### Integration Tests - Source → Plan
- **Focus on plan structure**: Verify correct stages, phases, dependencies
- **Use realistic fixtures**: Load from `pkg/cogpack/testdata/`
- **Test edge cases**: Empty projects, malformed configs, complex dependencies
- **No Docker required**: Pure Go testing

```go
func TestPythonStack_GeneratesCorrectPlan(t *testing.T) {
    src := loadFixture(t, "python-with-requirements")
    
    planResult, err := cogpack.GeneratePlan(context.Background(), src)
    require.NoError(t, err)
    
    assert.Len(t, planResult.Plan.Stages, 4) // base, python, deps, source
    assert.Equal(t, "uv-install", planResult.Plan.Stages[2].ID)
}
```

### Integration Tests - Plan → Image
- **Use testcontainers**: Via [`pkg/docker/testenv/`](../pkg/docker/testenv/)
- **Gate with environment**: `COGPACK_INTEGRATION=1`
- **Test metadata flow**: ENV vars, WORKDIR, platform preservation
- **Verify image functionality**: Can the built image actually run?

```go
func TestPlanExecution_EnvironmentVariables(t *testing.T) {
    testhelpers.RequireIntegrationSuite(t)
    
    env := testenv.New(t)
    builder := NewBuildKitBuilder(env.DockerClient())
    
    plan := &plan.Plan{
        Stages: []*plan.Stage{{
            ID: "base",
            Source: plan.Input{Image: "python:3.11"},
            Env: []string{"CUSTOM_VAR=test"},
        }},
    }
    
    _, imageConfig, err := builder.Build(t.Context(), plan, buildConfig)
    require.NoError(t, err)
    
    assert.Contains(t, imageConfig.Config.Env, "CUSTOM_VAR=test")
}
```

## Test Organization

### File Structure
```
pkg/cogpack/
├── plan/
│   ├── composer_test.go          # Unit: Composer API
│   ├── plan_test.go              # Unit: Plan structure
│   └── validation_test.go        # Unit: Validation logic
├── builder/
│   ├── translate_test.go         # Unit: Plan→LLB translation
│   └── integration_test.go       # Integration: Plan→Image
├── stacks/python/
│   ├── uv_test.go               # Unit: UV block logic
│   ├── stack_test.go            # Integration: Source→Plan
│   └── python_integration_test.go # Integration: Full Python pipeline
└── testdata/
    ├── minimal-source/           # Simple Python project
    ├── requirements-project/     # Project with requirements.txt
    └── uv-project/              # Native UV project
```

### Test Helpers
- **`testhelpers.RequireIntegrationSuite(t)`**: Gates integration tests
- **`testenv.New(t)`**: Docker environment with cleanup
- **`loadFixture(t, name)`**: Load project fixtures
- **`buildImageFromFixture(t, fixture)`**: Build image from test project

## Coverage Goals

### High Priority (>90% coverage)
- **Plan composition**: Core framework logic
- **Input resolution**: Phase/stage references
- **BuildKit translation**: Plan → LLB conversion

### Medium Priority (>70% coverage)  
- **Block implementations**: UV, CogWheel, SourceCopy
- **Stack orchestration**: Python stack logic
- **Validation**: Plan validation rules

### Lower Priority (>50% coverage)
- **Integration paths**: Full pipeline tests
- **Error handling**: Edge cases and failures

## Future Improvements

### Missing Test Areas
- **GPU/CUDA builds**: Integration tests with CUDA base images
- **Multi-stage builds**: Complex dependency graphs
- **Error path testing**: Builder failure scenarios
- **Performance testing**: Large project build times

### Test Infrastructure
- **Parallel test execution**: Safe Docker isolation
- **Test data management**: Realistic model fixtures
- **CI integration**: Automated test suite execution

---

**Key Principle**: Trust in the foundation (framework components) enables confidence in the application (Python model logic). Invest heavily in framework testing to reduce risk in business logic changes.