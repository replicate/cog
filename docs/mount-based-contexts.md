# Mount-Based Context System

> **Status**: Implemented ✅  
> **Last Updated**: 2025-07-11

## Overview

The mount-based context system is a key architectural component of cogpack that enables flexible file and resource mounting during build operations. Instead of using `MkFile` operations to write files directly to the build context, the system uses BuildKit mount operations to provide access to filesystems during specific build steps.

## Key Components

### BuildContext Structure
```go
type BuildContext struct {
    Name        string            `json:"name"`         // context name for referencing
    SourceBlock string            `json:"source_block"` // which block created this context
    Description string            `json:"description"`  // human-readable description
    Metadata    map[string]string `json:"metadata"`     // debug annotations
    FS          fs.FS             `json:"-"`            // the actual filesystem (not serialized)
}
```

### Extended Input Types
```go
type Input struct {
    Image string `json:"image,omitempty"` // external image reference
    Stage string `json:"stage,omitempty"` // reference to another stage
    Local string `json:"local,omitempty"` // build context name
    URL   string `json:"url,omitempty"`   // HTTP/HTTPS URL for files
}
```

### Mount Specification
```go
type Mount struct {
    Source Input  `json:"source"` // mount source (supports all Input types)
    Target string `json:"target"` // mount path in container
}
```

## Implementation Architecture

### Plan-Level Context Storage
Contexts are stored directly in the `Plan` struct:

```go
type Plan struct {
    // ... other fields
    Contexts map[string]*BuildContext `json:"contexts"` // build contexts for mounting
}
```

### Generic Context Creation
The system provides a generic `ContextFS` that can handle both directory paths and `fs.FS` interfaces:

```go
// Create from directory
ctx, err := NewContextFromDirectory("my-context", "/path/to/dir")

// Create from fs.FS (creates temp directory)
ctx, err := NewContextFromFS("my-context", myFilesystem)
```

### BuildKit Integration
During plan execution, the BuildKit builder:

1. **Processes contexts generically** - Iterates over `plan.Contexts`
2. **Converts fs.FS to fsutil.FS** - Uses `convertToFsutilFS()` for BuildKit compatibility
3. **Creates local mounts** - Maps context names to `fsutil.FS` instances
4. **Translates mount operations** - Converts plan `Mount` specs to BuildKit LLB mount options

## Use Cases

### 1. Embedded Wheel Installation (Cog)
```go
// Add embedded wheel filesystem to plan
plan.Contexts["wheel-context"] = &BuildContext{
    Name:        "wheel-context",
    SourceBlock: "cog-wheel",
    Description: "Cog wheel file for installation",
    Metadata:    map[string]string{"type": "embedded-wheel"},
    FS:          dockerfile.CogEmbed, // embed.FS with wheel files
}

// Use in exec operation
stage.Operations = append(stage.Operations, Exec{
    Command: "/uv/uv pip install /mnt/wheel/embed/*.whl",
    Mounts: []Mount{
        {
            Source: Input{Local: "wheel-context"},
            Target: "/mnt/wheel",
        },
    },
})
```

### 2. Build Context Mounting
```go
// Standard build context (source code)
stage.Operations = append(stage.Operations, Copy{
    From: Input{Local: "context"}, // refers to build context
    Src:  []string{"."},
    Dest: "/src",
})
```

### 3. HTTP Resource Access
```go
// Download and use external resources
stage.Operations = append(stage.Operations, Add{
    From: Input{URL: "https://example.com/file.tar.gz"},
    Src:  []string{"file.tar.gz"},
    Dest: "/tmp/downloads/",
})
```

## Key Benefits

### 1. **Flexibility**
- Supports multiple source types (directories, fs.FS, URLs, other stages)
- Generic interface for all file access patterns
- Easy to extend for new source types

### 2. **Performance** 
- Mount operations are more efficient than copying files
- BuildKit can optimize mount operations
- Temporary files are only created when needed

### 3. **Maintainability**
- Single interface for all file access
- Clear separation between context creation and usage
- Comprehensive validation of context references

### 4. **Debugging**
- Rich metadata and descriptions for contexts
- Clear source tracking (which block created what context)
- JSON-serializable for inspection

## Validation

The system includes comprehensive validation:

### Context Reference Validation
```go
// Validates that all referenced contexts exist
func validateContextReferences(p *Plan) error {
    // Collect all Local references from operations
    // Verify they exist in p.Contexts
}
```

### Mount Input Validation
```go
// Validates mount source specifications
func validateMountInput(input Input, stageStates map[string]State) error {
    // Ensure exactly one input type is specified
    // Validate stage references exist
    // Validate input is not empty
}
```

## BuildKit Translation

### LLB Operation Translation
The builder translates plan operations to BuildKit LLB:

```go
// Mount translation
func applyMounts(mounts []Mount, stageStates map[string]State, platform Platform) ([]llb.RunOption, error) {
    var opts []llb.RunOption
    for _, mount := range mounts {
        source, err := resolveMountInput(mount.Source, stageStates, platform)
        if err != nil {
            return nil, err
        }
        opts = append(opts, llb.AddMount(mount.Target, source))
    }
    return opts, nil
}
```

### Context Processing
```go
// Convert contexts to BuildKit local mounts
for name, buildCtx := range plan.Contexts {
    fsutilFS, err := convertToFsutilFS(buildCtx.FS)
    if err != nil {
        return fmt.Errorf("convert context %s: %w", name, err)
    }
    localMounts[name] = fsutilFS
}
```

## Migration from MkFile

The mount-based system replaces the previous `MkFile` approach:

### Before (MkFile)
```go
// Old approach - write files directly
stage.Operations = append(stage.Operations, MkFile{
    Dest: "/app/wheel.whl",
    Data: wheelData,
    Mode: 0644,
})
```

### After (Mount-Based)
```go
// New approach - mount filesystem
plan.Contexts["wheel-context"] = &BuildContext{...}
stage.Operations = append(stage.Operations, Exec{
    Command: "install /mnt/wheel/*.whl",
    Mounts: []Mount{{
        Source: Input{Local: "wheel-context"},
        Target: "/mnt/wheel",
    }},
})
```

## Testing

The system includes comprehensive test coverage:

### Unit Tests
- Context creation from directories and fs.FS
- Mount input validation
- Context reference validation

### Integration Tests
- BuildKit integration with real contexts
- End-to-end mount-based builds
- Plan validation with contexts

### Example Test
```go
func TestMountBasedWheelInstallation(t *testing.T) {
    // Create plan with wheel context
    plan.Contexts["wheel-context"] = &BuildContext{
        FS: embedded.WheelFS,
    }
    
    // Verify mount is created correctly
    _, stageStates, err := translatePlan(ctx, plan)
    require.NoError(t, err)
    
    // Verify context is processed
    assert.Contains(t, stageStates, "cog-wheel")
}
```

## Future Considerations

### Performance Optimization
- Direct fs.FS to fsutil.FS conversion without temp directories
- Context caching and reuse across builds
- Lazy context materialization

### Extended Context Types
- Remote filesystem support (S3, HTTP, etc.)
- Encrypted context support
- Dynamic context generation

### Advanced Mount Options
- Read-only mount specifications
- Mount-time transformations
- Conditional mounting based on build parameters

## Conclusion

The mount-based context system provides a flexible, performant, and maintainable approach to file and resource access during builds. It replaces the previous MkFile-based approach with a more generic system that can handle diverse source types while maintaining clear separation of concerns and comprehensive validation.