// Package schemagen invokes the cog-schema-gen binary to produce an OpenAPI
// schema from Python source files on disk, without booting a Docker container.
//
// The binary is resolved in this order:
//  1. COG_SCHEMA_GEN_TOOL env var (local path or URL)
//  2. Embedded binary (extracted to cache on first use)
//  3. dist/cog-schema-gen relative to cwd (development builds)
//  4. dist/cog-schema-gen relative to the cog executable (goreleaser layout)
//  5. cog-schema-gen on PATH
package schemagen

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	// EnvVar is the environment variable that overrides binary resolution.
	// It accepts either a local file path or an https:// URL. When a URL is
	// provided, the binary is downloaded once and cached under ~/.cache/cog/bin/.
	EnvVar = "COG_SCHEMA_GEN_TOOL"
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

// GenerateCombined generates OpenAPI schemas for both predict and train modes
// (when both are configured) and merges them into a single schema. If only one
// mode is configured, it behaves identically to Generate.
func GenerateCombined(ctx context.Context, sourceDir string, predictRef string, trainRef string) (map[string]any, error) {
	if predictRef == "" && trainRef == "" {
		return nil, fmt.Errorf("no predict or train reference provided")
	}

	// Single-mode: just generate the one schema
	if predictRef == "" {
		return Generate(ctx, sourceDir, trainRef, "train")
	}
	if trainRef == "" {
		return Generate(ctx, sourceDir, predictRef, "predict")
	}

	// Both modes: generate each and merge
	predictSchema, err := Generate(ctx, sourceDir, predictRef, "predict")
	if err != nil {
		return nil, fmt.Errorf("predict schema: %w", err)
	}
	trainSchema, err := Generate(ctx, sourceDir, trainRef, "train")
	if err != nil {
		return nil, fmt.Errorf("train schema: %w", err)
	}

	return MergeSchemas(predictSchema, trainSchema), nil
}

// MergeSchemas merges a predict-mode and train-mode OpenAPI schema into a single
// combined schema. The predict schema is used as the base; paths and component
// schemas from the train schema are added to it.
func MergeSchemas(predict, train map[string]any) map[string]any {
	// Merge paths: add train paths to predict paths
	predictPaths, _ := predict["paths"].(map[string]any)
	trainPaths, _ := train["paths"].(map[string]any)
	if predictPaths != nil && trainPaths != nil {
		for k, v := range trainPaths {
			if _, exists := predictPaths[k]; !exists {
				predictPaths[k] = v
			}
		}
	}

	// Merge component schemas: add train components to predict components
	predictComponents, _ := predict["components"].(map[string]any)
	trainComponents, _ := train["components"].(map[string]any)
	if predictComponents != nil && trainComponents != nil {
		predictSchemas, _ := predictComponents["schemas"].(map[string]any)
		trainSchemas, _ := trainComponents["schemas"].(map[string]any)
		if predictSchemas != nil && trainSchemas != nil {
			for k, v := range trainSchemas {
				if _, exists := predictSchemas[k]; !exists {
					predictSchemas[k] = v
				}
			}
		}
	}

	return predict
}

// ResolveBinary finds the cog-schema-gen binary.
//
// Resolution order:
//  1. COG_SCHEMA_GEN_TOOL env var (local path or URL)
//  2. Embedded binary (extracted to ~/.cache/cog/bin/cog-schema-gen-{version})
//  3. dist/cog-schema-gen relative to cwd (development builds)
//  4. dist/cog-schema-gen relative to the cog executable (goreleaser layout)
//  5. cog-schema-gen on PATH
func ResolveBinary() (string, error) {
	// 1. Explicit env var (local path or URL)
	if envVal := os.Getenv(EnvVar); envVal != "" {
		if strings.HasPrefix(envVal, "https://") || strings.HasPrefix(envVal, "http://") {
			path, err := downloadAndCache(envVal)
			if err != nil {
				return "", fmt.Errorf("%s=%s: %w", EnvVar, envVal, err)
			}
			console.Debugf("Using %s from %s (downloaded from %s)", BinaryName, path, envVal)
			return path, nil
		}
		if _, err := os.Stat(envVal); err != nil {
			return "", fmt.Errorf("%s=%s: %w", EnvVar, envVal, err)
		}
		console.Debugf("Using %s from %s=%s", BinaryName, EnvVar, envVal)
		return envVal, nil
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

	// 5. PATH lookup (dev builds only — production should always use the embedded binary)
	if isDev {
		if path, err := exec.LookPath(BinaryName); err == nil {
			console.Debugf("Using %s from PATH: %s", BinaryName, path)
			return path, nil
		}
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

	// Write atomically via unique temp file + rename.
	// Each process gets its own temp file to avoid ETXTBSY races when
	// multiple parallel builds extract the same embedded binary.
	tmp, err := os.CreateTemp(cacheDir, BinaryName+".tmp.*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file in %s: %w", cacheDir, err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("failed to write %s: %w", tmpPath, err)
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("failed to chmod %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("failed to close %s: %w", tmpPath, err)
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

// downloadAndCache downloads a binary from the given URL and caches it under
// ~/.cache/cog/bin/ keyed by a SHA-256 hash of the URL. Subsequent calls with
// the same URL return the cached path without re-downloading.
func downloadAndCache(url string) (string, error) {
	cacheDir, err := cacheDirectory()
	if err != nil {
		return "", err
	}

	// Derive a stable cache key from the URL.
	h := sha256.Sum256([]byte(url))
	cacheKey := hex.EncodeToString(h[:12]) // 24 hex chars — plenty unique
	cachedPath := filepath.Join(cacheDir, fmt.Sprintf("%s-%s", BinaryName, cacheKey))

	// If already downloaded, reuse it.
	if info, err := os.Stat(cachedPath); err == nil && info.Size() > 0 {
		console.Debugf("Using cached %s from %s", BinaryName, cachedPath)
		return cachedPath, nil
	}

	console.Infof("Downloading %s from %s...", BinaryName, url)

	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create cache directory %s: %w", cacheDir, err)
	}

	resp, err := http.Get(url) //nolint:gosec // URL comes from user-set env var
	if err != nil {
		return "", fmt.Errorf("failed to download %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download %s: HTTP %d", url, resp.StatusCode)
	}

	// Write atomically via temp file + rename.
	tmpPath := cachedPath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file %s: %w", tmpPath, err)
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("failed to write %s: %w", tmpPath, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("failed to close %s: %w", tmpPath, err)
	}

	if err := os.Rename(tmpPath, cachedPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("failed to rename %s to %s: %w", tmpPath, cachedPath, err)
	}

	return cachedPath, nil
}
