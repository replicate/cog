# weights-gen

A tool for generating random weight files and optionally a `weights.lock` file for testing.

## Installation

```bash
go install github.com/replicate/cog/tools/weights-gen@latest
```

## Usage

```bash
# If installed via go install
weights-gen [flags]

# Or run directly from the repository
go run ./tools/weights-gen [flags]
```

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--count` | `-n` | `3` | Number of random weight files to generate |
| `--min-size` | | `25mb` | Minimum file size (e.g., `12mb`, `25MB`, `1gb`) |
| `--max-size` | | `50mb` | Maximum file size (e.g., `50mb`, `100MB`, `1gb`) |
| `--output-dir` | | temp dir | Directory to write generated weight files |
| `--output` | `-o` | `weights.lock` | Output path for weights.lock file |
| `--dest-prefix` | | `/cache/` | Prefix for destination paths in lock file |
| `--no-lock` | | `false` | Skip generating the weights.lock file |

## Examples

```bash
# Generate 3 random files (25-50MB each) with a weights.lock file
go run ./tools/weights-gen

# Generate 5 files between 12-50MB
go run ./tools/weights-gen --count 5 --min-size 12mb --max-size 50mb

# Generate files to a specific output directory
go run ./tools/weights-gen --output-dir ./my-weights/

# Generate only weight files without a lock file
go run ./tools/weights-gen --output-dir ./my-weights/ --no-lock

# Generate files with custom destination prefix
go run ./tools/weights-gen --output-dir ./my-weights/ --dest-prefix /models/
```

## Output

The tool generates:
- Random binary weight files named `weights-001.bin`, `weights-002.bin`, etc.
- A `weights.lock` file (unless `--no-lock` is specified) containing metadata about each file including SHA256 digests for both original and gzip-compressed content.

The path to the generated files is always printed to stdout.

## How the lock file works

The `weights.lock` file contains a `dest` field for each weight file. By default, `dest` paths use the `/cache/` prefix, which is the standard location for weights in Cog containers.

Use `--dest-prefix` to override this behavior if you need different paths in the lock file (e.g., `/models/` or local paths for testing).
