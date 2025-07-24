# UV Block Simplification and Critical Environment Variable Fix

**Date**: 2025-07-18  
**Author**: Claude (with @md)  
**Status**: Completed

## Summary

This session completed a major refactoring of the UV block to implement a UV-native approach and fixed a critical environment variable inheritance bug in BuildKit LLB translation. The work involved both simplifying the UV block architecture and solving a fundamental issue with environment variable preservation across build stages.

## Problem Context

### UV Block Complexity
The UV block had become overengineered with ML/AI framework-specific logic:
- Two-phase installation (framework vs user packages)
- Package classification logic (torch/tensorflow detection)
- PyTorch index URL handling
- Complex environment variable optimizations (CFLAGS, UV_TORCH_BACKEND)
- Requirements.txt generation on-the-fly

### Critical Environment Variable Bug
A critical bug was discovered where environment variables were being lost between build stages:
- `cd: executable file not found in $PATH` errors during UV sync
- PATH environment variable from base images not preserved
- Root cause: `llb.Diff(base, modified)` operations only capture filesystem changes, not environment variables

## Solution Implementation

### 1. UV Block Simplification

**UV-Native Architecture**: Redesigned the UV block to treat all Python projects as UV projects at build time.

**Key Changes**:
- **Removed ML/AI Logic**: Eliminated `separateFrameworkPackages()`, PyTorch URL logic, and two-phase installation
- **UV Project Detection**: Added `isUvProject()` function checking for `uv.lock` or `pyproject.toml`
- **Legacy Project Conversion**: Non-UV projects are converted to UV format via generated `pyproject.toml`
- **Unified Installation**: Single `uv sync` command for all dependency installation
- **Cog Wheel Integration**: Added cog wheel as `file://` dependency in generated pyproject.toml

**Benefits**:
- Reduced complexity from ~220 lines to ~180 lines
- Unified workflow for all Python projects
- Better dev UX (cog package visible to LSPs)
- Leverages UV's deterministic lockfile approach

### 2. Critical Environment Variable Fix

**Root Cause**: In `translate.go:120-121`, the LLB translation process was:
1. Creating a `modified` state with accumulated environment variables
2. Using `llb.Diff(base, modified)` to capture only filesystem changes
3. Applying changes back to `base` state, losing environment variables

**Solution**: Extract all environment variables from the `modified` state and apply them to the `final` state:

```go
// Extract ALL environment variables from the modified state
envList, err := modified.Env(ctx)
if err != nil {
    return llb.State{}, nil, fmt.Errorf("failed to extract environment variables from modified state in stage %s: %w", st.ID, err)
}

// Apply all environment variables to the final state
envArray := envList.ToArray()
for _, env := range envArray {
    if eq := strings.Index(env, "="); eq != -1 {
        final = final.AddEnv(env[:eq], env[eq+1:])
    }
}
```

### 3. Additional Fixes

**Shell Command Issue**: Replaced `cd /app && uv sync` with working directory specification:
```go
syncStage.Dir = "/app"
syncStage.AddOperation(plan.Exec{
    Command: "uv sync --python /venv/bin/python --no-install-project",
})
```

**Reason**: `cd` is a shell builtin, not an executable, causing PATH resolution failures.

## Testing Strategy Implementation

Built a comprehensive testing framework to catch similar issues early:

### Enhanced Plan->LLB Translation Tests
- **Environment Variable Preservation**: Tests that verify env vars are preserved across stage chains
- **Mount Resolution**: Validates that mount operations work correctly
- **Complex Stage Chains**: Tests multi-stage dependency resolution
- **Error Handling**: Comprehensive error scenario testing
- **Performance Benchmarks**: Baseline performance measurements

### Test Coverage
- Environment variable preservation across stages ✅
- Mount handling and validation ✅
- Stage dependency resolution ✅
- Error scenarios and edge cases ✅
- LLB state property inspection ✅

## Key Architecture Decisions

### Environment Variable Preservation Strategy
**Decision**: Extract environment variables from modified LLB state and apply to final state  
**Rationale**: `llb.Diff()` only captures filesystem changes, not environment context  
**Trade-offs**: Slightly more complex translation logic, but ensures runtime dependencies work

### UV-Native Approach
**Decision**: Treat all Python projects as UV projects at build time  
**Rationale**: Unifies dependency management and leverages UV's deterministic lockfile approach  
**Trade-offs**: Requires pyproject.toml generation for legacy projects, but simplifies overall architecture

### Working Directory vs Shell Commands
**Decision**: Use LLB working directory specification instead of shell cd commands  
**Rationale**: Shell builtins like `cd` are not available as executables in minimal containers  
**Trade-offs**: None - this is strictly better

## Results

### Immediate Success
- ✅ UV block builds complete successfully
- ✅ Environment variables preserved across stages
- ✅ `cd: executable file not found in $PATH` errors eliminated
- ✅ All tests passing, including new comprehensive test suite

### Code Quality Improvements
- 🔧 Reduced UV block complexity (~220 → ~180 lines)
- 🔧 Cleaner, more maintainable code
- 🔧 Better separation of concerns
- 🔧 Comprehensive test coverage

### Architecture Benefits
- 🏗️ Unified UV workflow for all Python projects
- 🏗️ Better dev UX with cog package visibility
- 🏗️ Leverages UV's deterministic dependency resolution
- 🏗️ Robust environment variable handling

## Verification

### Manual Testing
```bash
cd test-integration/test_integration/fixtures/string-project
COGPACK=1 go run ../../../../cmd/cog build
# ✅ Build completes successfully
# ✅ No environment variable errors
# ✅ UV sync works correctly
```

### Automated Testing
```bash
go test ./pkg/cogpack/builder/... -run TestTranslatePlan_EnvironmentVariablePreservation -v
# ✅ PASS: Environment variables preserved across stages
# ✅ All enhanced translation tests pass
```

## Next Steps

### Immediate Follow-ups
1. **Complete UV wheel path fix** - Fix remaining wheel mount path issue
2. **Integration testing** - Add BuildKit integration tests with Docker
3. **Stack-specific tests** - Python stack-specific functionality tests

### Future Enhancements
1. **Base image metadata** - Define structure for base image dependency resolution
2. **Framework blocks** - Implement simplified torch/tensorflow blocks
3. **Property-based testing** - Add fuzz-like testing for complex scenarios

## Lessons Learned

### Critical Insight
**Environment variables are lost in LLB diff operations** - This is a fundamental issue that affects any BuildKit-based build system using multi-stage builds. The fix is to explicitly extract and preserve environment variables from the modified state.

### Testing Philosophy
**Test LLB state properties directly** - Instead of complex mocking, test the actual LLB state properties like environment variables, which catches real issues.

### Architecture Principle
**Simplify by unifying workflows** - The UV-native approach dramatically simplified the codebase by treating all projects the same way at build time.

## Files Modified

### Core Implementation
- `pkg/cogpack/stacks/python/uv.go` - UV block simplification
- `pkg/cogpack/stacks/python/uv_test.go` - Updated tests
- `pkg/cogpack/builder/translate.go` - Environment variable fix

### Testing Framework
- `pkg/cogpack/builder/translate_enhanced_test.go` - New comprehensive tests
- `pkg/cogpack/builder/translate.go` - Added TranslatePlan export for testing

### Test Fixtures
- `test-integration/test_integration/fixtures/uv-project/` - New UV project test fixture

This work establishes a solid foundation for Python dependency management in cogpack with proper environment variable handling and comprehensive testing coverage.