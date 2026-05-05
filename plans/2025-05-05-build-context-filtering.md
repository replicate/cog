# Build Context Filtering + .cog Directory Cleanup

## Problem

Every `cog build` sends the entire `.cog/` directory to BuildKit as context.
This includes weight blobs, mount directories, stale tmp dirs from crashed
builds, and build caches -- none of which the Dockerfile references. For a
project with 10GB of cached weights, that's 10GB of useless context on every
build.

Additionally, the build staging directory uses a timestamp-based name
(`.cog/tmp/build<timestamp>/`), so the `COPY` paths in the generated
Dockerfile change every run. This busts Docker layer cache even when the file
contents haven't changed.

There's also a guard (`checkCompatibleDockerIgnore`) that rejects builds if
the user's `.dockerignore` excludes `.cog`, because build staging files live
there. This forces users to include the entire `.cog/` tree in their context.

Finally, `.dockerignore` manipulation for separate-weights builds
(backup/write/restore) is fragile -- it mutates a user file on disk and can
leave it in a broken state if the build crashes.

## Design

### Two-mount architecture

Instead of one `LocalDirs["context"]` pointing at the project root (which
includes everything), use two `LocalMounts`:

| Mount name   | Local path          | Purpose                                    |
|------------- |---------------------|--------------------------------------------|
| `context`    | `<projectDir>`      | User source code. Excludes `.cog` entirely |
| `cog_build`  | `<projectDir>/.cog/build` | Build staging (wheels, requirements.txt, CA certs, schemas, weights manifest) |

The `context` mount uses `fsutil.NewFilterFS` with
`ExcludePatterns: []string{".cog"}`. Everything under `.cog/` is invisible to
BuildKit through this mount.

The `cog_build` mount is a plain `fsutil.NewFS` pointing at `.cog/build/`.
No filtering needed -- everything in this directory is build-relevant by
construction.

The generated Dockerfile uses `COPY --from=cog_build` for any file that
comes from the staging directory:

```dockerfile
# Before (today):
COPY .cog/tmp/build20240620123456.000000/requirements.txt /tmp/requirements.txt

# After:
COPY --from=cog_build requirements.txt /tmp/requirements.txt
```

### Stable build directory

Replace `.cog/tmp/build<timestamp>/` with a single `.cog/build/` directory.
No timestamp, no partitioning. The directory is created at the start of each
build and cleaned up at the end (same as today's `Cleanup()`).

Concurrent builds in the same project directory are serialized by an
advisory lock at `.cog/build.lock`. The second build blocks until the first
finishes.

### Bundled files move into .cog/build/

`openapi_schema.json` and `weights.json` are currently written to `.cog/`
root. They move into `.cog/build/` so they're accessible through the
`cog_build` mount.

The bundle step (second docker build that COPYs schema + weights manifest
into the image) uses `cog_build` as its context instead of the full project
directory.

### .dockerignore guard removed

`checkCompatibleDockerIgnore` is deleted. Since build staging files come
through the `cog_build` mount (not the `context` mount), the user's
`.dockerignore` cannot interfere with them. Users are free to ignore `.cog`
in their `.dockerignore` -- it's redundant with our fsutil filter but
harmless.

### Separate-weights .dockerignore mutation removed

The backup/write/restore dance for `.dockerignore` during separate-weights
builds is replaced by passing appropriate `ExcludePatterns` through the
fsutil filter. No user files are mutated on disk.

## Changes by File

### `pkg/docker/command/command.go`

Add `ExcludePatterns []string` field to `ImageBuildOptions`. This lets
callers declare fsutil exclude patterns for the context mount without the
docker package knowing about `.cog` conventions.

### `pkg/docker/buildkit.go` — `solveOptFromImageOptions`

**Switch from `LocalDirs` to `LocalMounts`.**

1. Create `fsutil.FS` for the context directory
2. If `opts.ExcludePatterns` is non-empty, wrap with `fsutil.NewFilterFS`
3. Create `fsutil.FS` for the dockerfile directory
4. For each entry in `opts.BuildContexts`, create `fsutil.FS`
5. Set `solveOpts.LocalMounts` (not `LocalDirs`)

Pseudocode:

```go
contextFS, err := fsutil.NewFS(contextDir)
if len(opts.ExcludePatterns) > 0 {
    contextFS, err = fsutil.NewFilterFS(contextFS, &fsutil.FilterOpt{
        ExcludePatterns: opts.ExcludePatterns,
    })
}

dockerfileFS, err := fsutil.NewFS(filepath.Dir(dockerfilePath))

localMounts := map[string]fsutil.FS{
    "context":    contextFS,
    "dockerfile": dockerfileFS,
}

for name, dir := range opts.BuildContexts {
    if name == "dockerfile" || name == "context" {
        console.Warnf("build context name collision: %q", name)
        continue
    }
    fs, err := fsutil.NewFS(dir)
    localMounts[name] = fs
    frontendAttrs["context:"+name] = "local:" + name
}

solveOpts.LocalMounts = localMounts
// Remove: solveOpts.LocalDirs
```

### `pkg/dockercontext/build_tempdir.go`

**Drop the timestamp from `BuildTempDir`.**

```go
// Before:
func BuildTempDir(dir string) (string, error) {
    now := time.Now().Format("20060102150405.000000")
    return BuildCogTempDir(dir, "build"+now)
}

// After:
func BuildTempDir(dir string) (string, error) {
    return BuildCogTempDir(dir, "build")
}
```

This produces `.cog/build/` instead of `.cog/tmp/build<timestamp>/`.

`CogTempDir` and `BuildCogTempDir` remain unchanged -- they're used by other
callers. The `tmp` level disappears from the build path because
`BuildCogTempDir` joins `dir + ".cog" + "tmp" + subDir` -- wait, that would
produce `.cog/tmp/build`. We want `.cog/build`.

So actually, change `BuildTempDir` to bypass `CogTempDir`/`BuildCogTempDir`
and construct the path directly:

```go
func BuildTempDir(dir string) (string, error) {
    buildDir := filepath.Join(dir, global.CogBuildArtifactsFolder, "build")
    if err := os.MkdirAll(buildDir, 0o755); err != nil {
        return "", err
    }
    return buildDir, nil
}
```

### `pkg/dockerfile/standard_generator.go`

**1. `NewStandardGenerator`**: No change to constructor signature. The
`tmpDir` and `relativeTmpDir` fields now point to `.cog/build/` (stable
path). The `relativeTmpDir` becomes `.cog/build` relative to the project
dir.

**2. `writeTemp`**: Change the COPY line to use `--from=cog_build` with a
path relative to `.cog/build/` (not relative to the project root).

```go
// Before:
return []string{fmt.Sprintf("COPY %s /tmp/%s",
    filepath.Join(g.relativeTmpDir, filename), filename)},
    "/tmp/" + filename, nil

// After:
return []string{fmt.Sprintf("COPY --from=cog_build %s /tmp/%s",
    filename, filename)},
    "/tmp/" + filename, nil
```

The `relativeTmpDir` field can be removed since it's no longer used in COPY
paths. The `tmpDir` (absolute path) is still needed for `os.WriteFile`.

**3. `BuildContexts`**: Return the `cog_build` context.

```go
func (g *StandardGenerator) BuildContexts() (map[string]string, error) {
    return map[string]string{
        "cog_build": g.tmpDir,
    }, nil
}
```

**4. `Cleanup`**: Unchanged -- still does `os.RemoveAll(g.tmpDir)`.

### `pkg/image/build.go`

**1. Remove `checkCompatibleDockerIgnore`** and its call at line 74.

**2. Move bundled file paths into `.cog/build/`**:

```go
// Before:
const bundledSchemaFile = ".cog/openapi_schema.json"
const bundledWeightsFile = ".cog/weights.json"

// After:
const bundledSchemaFile = ".cog/build/openapi_schema.json"
const bundledWeightsFile = ".cog/build/weights.json"
```

**3. Pass `ExcludePatterns` on every `ImageBuildOptions`** that sets a
`ContextDir`:

```go
ExcludePatterns: []string{".cog"},
```

This applies to:
- Main build (line ~258)
- Custom Dockerfile build (line ~168)
- Separate-weights builds (lines ~230, ~241, ~648, ~668)

**4. Update `bundleDockerfile`** to use `--from=cog_build`:

```go
func bundleDockerfile(baseImage string, files []string) string {
    var b strings.Builder
    fmt.Fprintf(&b, "FROM %s\n", baseImage)
    for _, f := range files {
        // f is e.g. ".cog/build/openapi_schema.json"
        // we want just "openapi_schema.json" relative to cog_build mount
        rel := filepath.Base(f)
        fmt.Fprintf(&b, "COPY --from=cog_build %s .cog/\n", rel)
    }
    return b.String()
}
```

The bundle build also needs `cog_build` in its `BuildContexts`:

```go
buildOpts := command.ImageBuildOptions{
    DockerfileContents: bundleDockerfile(tmpImageId, files),
    ImageName:          tmpImageId,
    ProgressOutput:     progressOutput,
    BuildContexts:      map[string]string{
        "cog_build": filepath.Join(dir, global.CogBuildArtifactsFolder, "build"),
    },
}
```

**5. Remove `.dockerignore` manipulation functions.** The
`backupDockerignore`, `writeDockerignore`, `restoreDockerignore`, and
`makeDockerignoreForWeightsImage` functions are deleted. The
`buildRunnerImage` function drops the backup/restore calls. The separate-
weights builds pass their exclude patterns via `ExcludePatterns` instead.

The `DockerignoreHeader` patterns in `standard_generator.go` that
separate-weights builds used to write to `.dockerignore` need to move into
the `ExcludePatterns` list for those build calls. These patterns
(`__pycache__`, `*.pyc`, `.git`, etc.) should be passed alongside `.cog`.

**6. Add build advisory lock.** At the top of `Build()`, acquire a lockfile
at `.cog/build.lock` (using the same `lockfile.WithLock` pattern as
`weights.lock.guard`). Release on function exit. This serializes concurrent
builds in the same project directory.

### `pkg/cli/init-templates/base/.dockerignore`

Add `.cog` to the template:

```
# Exclude cog build artifacts and caches
.cog
```

### `pkg/dockerignore/` package

This package is only used by `checkCompatibleDockerIgnore`. Once that
function is removed, check if the package has other callers. If not, delete
it.

### `examples/managed-weights/.dockerignore`

Update the comment and patterns. The explanation about not excluding `.cog/`
is no longer relevant.

## Build Advisory Lock

Use the same file-locking mechanism as `weights.lock.guard`. Create
`.cog/build.lock` (or reuse an existing lock helper) to ensure only one
`cog build` runs in a project directory at a time.

```go
// In Build(), early:
lockPath := filepath.Join(dir, global.CogBuildArtifactsFolder, "build.lock")
if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
    return "", err
}
unlock, err := acquireBuildLock(ctx, lockPath)
if err != nil {
    return "", fmt.Errorf("another build is running in this directory: %w", err)
}
defer unlock()
```

## Migration

No migration needed. This is a build-time change:
- The `.cog/tmp/` directory tree becomes orphaned (harmless, cleaned by next
  build or manual `rm -rf .cog/tmp`)
- Users who had `.cog` in `.dockerignore` (previously rejected) can now
  build successfully
- `cog init` on new projects produces the updated `.dockerignore`

## Testing

- `mise run test:go` -- unit tests for generator, buildkit opts, build
- Verify `COPY --from=cog_build` in generated Dockerfiles (generator tests)
- Verify `ExcludePatterns` are set on `SolveOpt.LocalMounts` (buildkit tests)
- Verify consecutive builds with unchanged code hit Docker layer cache
- Verify `.cog/weights/` (once relocated in the next phase) doesn't appear
  in build context
- `mise run test:integration` -- end-to-end build tests

## What This Unblocks

After this lands, the weights store can move from `~/.cache/cog/weights/` to
`.cog/weights/` without affecting build context size. That's the next phase:
introduce `dotcog.CacheRoot`, relocate the store, add `cog weights prune`.
