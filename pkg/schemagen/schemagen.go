// Package schemagen invokes the cog-schema-gen binary to produce an OpenAPI
// schema from Python source files on disk, without booting a Docker container.
//
// The binary is resolved in this order:
//  1. COG_SCHEMA_GEN_BINARY env var (explicit override)
//  2. Embedded binary (extracted to cache on first use)
//  3. dist/cog-schema-gen relative to cwd (development builds)
//  4. dist/cog-schema-gen relative to the cog executable (goreleaser layout)
//  5. cog-schema-gen on PATH
package schemagen

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/util/console"
)

const (
	// BinaryName is the name of the schema generator binary.
	BinaryName = "cog-schema-gen"
	// EnvVar is the environment variable that overrides binary location.
	EnvVar = "COG_SCHEMA_GEN_BINARY"
)

//go:embed embedded/*
var embeddedFS embed.FS

// Generate runs cog-schema-gen against the given source directory and predictor
// reference, returning the parsed OpenAPI schema JSON.
//
// Parameters:
//   - ctx: context for cancellation/timeout
//   - sourceDir: directory containing the Python source files
//   - predictRef: predictor reference in "file.py:ClassName" format (from cog.yaml predict/train)
//   - mode: "predict" or "train"
func Generate(ctx context.Context, sourceDir string, predictRef string, mode string) (map[string]any, error) {
	binary, err := ResolveBinary()
	if err != nil {
		return nil, fmt.Errorf("cannot find %s binary: %w\n\nTo build it, run: mise run build:schema-gen", BinaryName, err)
	}

	console.Debugf("=== schemagen.Generate binary=%s src=%s ref=%s mode=%s", binary, sourceDir, predictRef, mode)

	args := []string{
		predictRef,
		"--mode", mode,
		"--src", sourceDir,
	}

	cmd := exec.CommandContext(ctx, binary, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s failed: %w\n\nstderr:\n%s", BinaryName, err, stderr.String())
	}

	var schema map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &schema); err != nil {
		return nil, fmt.Errorf("failed to parse %s output as JSON: %w\n\nstdout:\n%s\nstderr:\n%s",
			BinaryName, err, stdout.String(), stderr.String())
	}

	return schema, nil
}

// ResolveBinary finds the cog-schema-gen binary.
//
// Resolution order:
//  1. COG_SCHEMA_GEN_BINARY env var (explicit path)
//  2. Embedded binary (extracted to ~/.cache/cog/bin/cog-schema-gen-{version})
//  3. dist/cog-schema-gen relative to cwd (development builds)
//  4. dist/cog-schema-gen relative to the cog executable (goreleaser layout)
//  5. cog-schema-gen on PATH
func ResolveBinary() (string, error) {
	// 1. Explicit env var
	if envPath := os.Getenv(EnvVar); envPath != "" {
		if _, err := os.Stat(envPath); err != nil {
			return "", fmt.Errorf("%s=%s: %w", EnvVar, envPath, err)
		}
		console.Debugf("Using %s from %s=%s", BinaryName, EnvVar, envPath)
		return envPath, nil
	}

	// 2. Embedded binary
	if path, err := extractEmbedded(); err == nil {
		console.Debugf("Using embedded %s at %s", BinaryName, path)
		return path, nil
	}

	isDev := global.Version == "dev" || strings.Contains(global.Version, "-dev") || strings.Contains(global.Version, "+")

	// 3. Auto-detect from ./dist (dev builds only)
	if isDev {
		cwdDist := filepath.Join("dist", BinaryName)
		if _, err := os.Stat(cwdDist); err == nil {
			absPath, _ := filepath.Abs(cwdDist)
			if absPath != "" {
				console.Debugf("Using %s from %s", BinaryName, absPath)
				return absPath, nil
			}
			return cwdDist, nil
		}
	}

	// 4. Auto-detect relative to cog executable (goreleaser dist/go/<platform>/cog)
	if isDev {
		if exePath, err := os.Executable(); err == nil {
			if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
				distDir := filepath.Clean(filepath.Join(filepath.Dir(resolved), "..", ".."))
				candidate := filepath.Join(distDir, BinaryName)
				if _, err := os.Stat(candidate); err == nil {
					console.Debugf("Using %s from %s", BinaryName, candidate)
					return candidate, nil
				}
			}
		}
	}

	// 5. PATH lookup
	if path, err := exec.LookPath(BinaryName); err == nil {
		console.Debugf("Using %s from PATH: %s", BinaryName, path)
		return path, nil
	}

	return "", fmt.Errorf("%s not found (set %s, run mise run build:schema-gen, or install it on PATH)", BinaryName, EnvVar)
}

// extractEmbedded extracts the embedded binary to a versioned cache path and
// returns the path. Returns an error if no binary is embedded or extraction fails.
func extractEmbedded() (string, error) {
	data, err := embeddedFS.ReadFile("embedded/" + BinaryName)
	if err != nil {
		return "", fmt.Errorf("no embedded binary: %w", err)
	}

	cacheDir, err := cacheDirectory()
	if err != nil {
		return "", err
	}

	// Version the cache path so upgrades don't use stale binaries.
	cachedPath := filepath.Join(cacheDir, fmt.Sprintf("%s-%s-%s-%s", BinaryName, global.Version, runtime.GOOS, runtime.GOARCH))

	// If already extracted and matches size, reuse it.
	if info, err := os.Stat(cachedPath); err == nil && info.Size() == int64(len(data)) {
		return cachedPath, nil
	}

	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create cache directory %s: %w", cacheDir, err)
	}

	// Write atomically via temp file + rename.
	tmpPath := cachedPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o755); err != nil {
		return "", fmt.Errorf("failed to write %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, cachedPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("failed to rename %s to %s: %w", tmpPath, cachedPath, err)
	}

	return cachedPath, nil
}

// cacheDirectory returns the cache directory for cog binaries.
// Uses $XDG_CACHE_HOME/cog/bin or ~/.cache/cog/bin.
func cacheDirectory() (string, error) {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "cog", "bin"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}

	return filepath.Join(home, ".cache", "cog", "bin"), nil
}
