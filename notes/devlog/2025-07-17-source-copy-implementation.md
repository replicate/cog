# Development Session: Source Copy Implementation for Python Models

**Date**: July 17, 2025  
**Duration**: ~2 hours  
**Status**: ✅ Completed  
**Commit**: `55cf3ce` - Implement source copy functionality for Python models in cogpack

## Session Overview

This session focused on implementing source copy functionality for Python models in cogpack. The initial problem was that Python models were building successfully but failing to run because the application source code wasn't being copied to the runtime image.

## Problem Statement

Python models built with cogpack were failing with the error:
```
Python models are building, but they fail to run because the python model code isn't being copied to the runtime image.
```

The SourceCopyBlock existed but was commented out in the Python stack, and there were several issues preventing it from working correctly.

## Root Cause Analysis

Through systematic debugging, we identified three main issues:

### 1. SourceCopyBlock Not Enabled
- The `&commonblocks.SourceCopyBlock{}` was commented out in the Python stack
- **Location**: `pkg/cogpack/stacks/python/stack.go:60`

### 2. Incorrect Dependencies Method Signature
- SourceCopyBlock's Dependencies method returned `[]plan.Dependency` instead of `[]*plan.Dependency`
- **Location**: `pkg/cogpack/stacks/commonblocks/source_copy.go:24`

### 3. Nested Virtual Environment Issue (Critical)
- **Root Cause**: BuildKit's Copy operation semantics
- When copying `/venv` to `/venv` where destination already exists, BuildKit copies the source directory INTO the destination, creating `/venv/venv`
- This caused cog module to be installed at `/venv/venv/lib/python3.13/site-packages/cog` instead of `/venv/lib/python3.13/site-packages/cog`
- **Location**: `pkg/cogpack/stacks/python/uv.go` copy operation

## Technical Investigation Process

### Phase 1: Enable SourceCopyBlock
1. Uncommented SourceCopyBlock in Python stack
2. Fixed Dependencies method signature
3. Removed conflicting export config from SourceCopyBlock

### Phase 2: Debug Nested Venv Issue
1. Added extensive debugging to track venv creation and package installation
2. Used `docker run --entrypoint="" cog-string-project:latest find /venv -name "*cog*"` to trace package locations
3. Discovered packages ending up in `/venv/venv/` instead of `/venv/`
4. Used dive tool analysis to identify the issue in the `copy-venv` layer

### Phase 3: Understanding BuildKit Copy Semantics
1. Researched BuildKit LLB Copy operation documentation
2. Learned that Copy operations place directories INTO existing destinations rather than replacing them
3. Tried various approaches: `/venv/.`, `/venv/*`, individual file copying
4. All failed due to same underlying copy semantics

### Phase 4: Solution Implementation
1. **Key Insight**: Remove existing destination before copying
2. Added `rm -rf /venv` before `copy /venv /venv` operation
3. This ensures BuildKit creates `/venv` at the correct location instead of nesting

## Final Solution

The complete fix involved:

### Code Changes
1. **Enable SourceCopyBlock**: Uncommented in Python stack
2. **Fix method signature**: Changed Dependencies return type to `[]*plan.Dependency`
3. **Remove existing venv**: Added `rm -rf /venv` before copy operation
4. **Add conditional venv setup**: Used `--allow-existing` flag for base image compatibility

### Key Technical Insight
```go
// Problem: This creates /venv/venv when /venv already exists
exportStage.AddOperation(plan.Copy{
    From: plan.Input{Phase: plan.PhaseBuildComplete},
    Src:  []string{"/venv"},
    Dest: "/venv",
})

// Solution: Remove existing directory first
exportStage.AddOperation(plan.Exec{
    Command: "rm -rf /venv",
})
exportStage.AddOperation(plan.Copy{
    From: plan.Input{Phase: plan.PhaseBuildComplete},
    Src:  []string{"/venv"},
    Dest: "/venv",
})
```

## Testing & Validation

### Test Command
```bash
cd ./test-integration/test_integration/fixtures/string-project
COGPACK=1 go run ../../../../cmd/cog predict --input s=hello
```

### Results
- **Before**: `ModuleNotFoundError: No module named 'cog'`
- **After**: `hello hello` (successful prediction)

### Verification Steps
1. Confirmed cog package at correct location: `/venv/lib/python3.13/site-packages/cog`
2. Verified source code copied to `/src`
3. Tested end-to-end model execution
4. Removed debugging code for production

## Files Modified

| File | Purpose | Changes |
|------|---------|---------|
| `pkg/cogpack/stacks/python/stack.go` | Python stack orchestration | Enabled SourceCopyBlock |
| `pkg/cogpack/stacks/commonblocks/source_copy.go` | Source copy implementation | Fixed Dependencies signature, removed conflicting export config |
| `pkg/cogpack/stacks/python/uv.go` | Virtual environment handling | Added conditional venv setup, fixed copy semantics with directory removal |
| `CLAUDE.project.md` | Project documentation | Updated status, added design decision, documented completion |

## Design Decisions Documented

Added new design decision to project documentation:

**Source Copy with Directory Removal**: Use `rm -rf /venv` before copying `/venv` from build to runtime to prevent BuildKit Copy operation from creating nested `/venv/venv` structure. BuildKit copies directories INTO existing directories rather than replacing them.

## Lessons Learned

### BuildKit Copy Semantics
- BuildKit Copy operations have specific directory handling behavior
- When destination exists, source is copied INTO destination, not replacing it
- This is different from standard `cp` command behavior
- Solution: Remove destination before copying when replacement is intended

### Base Image Compatibility
- Need to handle both base images with existing venv and bare base images
- `--allow-existing` flag provides compatibility for existing venvs
- Conditional logic may be needed until base image metadata is available

### Debugging Methodology
- Use dive tool for layer-by-layer analysis
- Test with docker run commands to inspect final image state
- Add temporary debugging output to understand build process
- Remove debugging code before production

## Next Steps

With source copy functionality complete, cogpack now supports basic Python models end-to-end. Priority items for future work:

1. Additional blocks (Apt, Torch, CUDA)
2. Base image metadata structure definition
3. Advanced Cog build features
4. GPU/CUDA support
5. Framework-specific blocks (PyTorch, TensorFlow)

## Session Metrics

- **Problem Resolution**: ✅ Complete
- **Build Success**: ✅ Python models build and run
- **Test Coverage**: ✅ End-to-end validation
- **Documentation**: ✅ Updated with decisions and patterns
- **Code Quality**: ✅ Production-ready (debugging removed)

## Command Reference

```bash
# Test current functionality
COGPACK=1 go run ../../../../cmd/cog predict --input s=hello

# View build plan
COGPACK=1 go run ../../../../cmd/cog plan

# Debug final image
docker run --rm --entrypoint="" cog-string-project:latest find /venv -name "*cog*"
```

---

*This session represents a significant milestone in cogpack development, completing the basic Python model support pipeline and establishing patterns for future block development.*