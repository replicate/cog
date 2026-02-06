// pkg/model/weights_lock_generator.go
package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/replicate/cog/pkg/config"
)

// WeightsLockGeneratorOptions configures the WeightsLockGenerator.
type WeightsLockGeneratorOptions struct {
	// DestPrefix is the prefix for destination paths in the container (e.g., "/weights").
	DestPrefix string
}

// WeightsLockGenerator generates weights.lock files from weight sources.
type WeightsLockGenerator struct {
	opts WeightsLockGeneratorOptions
}

// NewWeightsLockGenerator creates a new WeightsLockGenerator with the given options.
func NewWeightsLockGenerator(opts WeightsLockGeneratorOptions) *WeightsLockGenerator {
	return &WeightsLockGenerator{opts: opts}
}

// Generate processes weight sources and returns a WeightsLock.
func (g *WeightsLockGenerator) Generate(ctx context.Context, projectDir string, sources []config.WeightSource) (*WeightsLock, error) {
	lock, _, err := g.GenerateWithFilePaths(ctx, projectDir, sources)
	return lock, err
}

// GenerateWithFilePaths processes weight sources and returns a WeightsLock along with
// a map of weight names to their absolute file paths.
func (g *WeightsLockGenerator) GenerateWithFilePaths(ctx context.Context, projectDir string, sources []config.WeightSource) (*WeightsLock, map[string]string, error) {
	var files []WeightFile
	filePaths := make(map[string]string)

	for _, src := range sources {
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		default:
		}

		sourcePath := filepath.Join(projectDir, src.Source)

		// TODO: should we validate that sourcePath is within projectDir to avoid accidental file access outside the project?
		info, err := os.Stat(sourcePath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil, fmt.Errorf("weight source not found: %s", src.Source)
			}
			return nil, nil, fmt.Errorf("stat weight source %s: %w", src.Source, err)
		}

		if info.IsDir() {
			// Process directory recursively
			err := filepath.Walk(sourcePath, func(path string, fi os.FileInfo, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if fi.IsDir() {
					return nil
				}

				relPath, err := filepath.Rel(projectDir, path)
				if err != nil {
					return fmt.Errorf("compute relative path: %w", err)
				}

				wf, err := g.processFile(ctx, path, relPath, "", "")
				if err != nil {
					return err
				}

				files = append(files, *wf)
				filePaths[wf.Name] = path
				return nil
			})
			if err != nil {
				return nil, nil, fmt.Errorf("walk weight directory %s: %w", src.Source, err)
			}
		} else {
			// Process single file
			wf, err := g.processFile(ctx, sourcePath, src.Source, src.Target, src.Name)
			if err != nil {
				return nil, nil, err
			}
			files = append(files, *wf)
			filePaths[wf.Name] = sourcePath
		}
	}

	lock := &WeightsLock{
		Version: "1.0",
		Created: time.Now().UTC(),
		Files:   files,
	}

	return lock, filePaths, nil
}

// processFile creates a WeightFile entry for a single file.
// If configName is non-empty, it is used as the weight name; otherwise the name
// is derived from the filename (basename without extension).
func (g *WeightsLockGenerator) processFile(ctx context.Context, absPath, relPath, customTarget, configName string) (*WeightFile, error) {
	// TODO: it would be better if we could cancel during a copy op
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Open and read file for hashing
	f, err := os.Open(absPath)
	if err != nil {
		return nil, fmt.Errorf("open weight file %s: %w", relPath, err)
	}
	defer f.Close()

	// Compute SHA256 digest
	hash := sha256.New()
	size, err := io.Copy(hash, f)
	if err != nil {
		return nil, fmt.Errorf("hash weight file %s: %w", relPath, err)
	}
	digest := "sha256:" + hex.EncodeToString(hash.Sum(nil))

	// Use config name if provided, otherwise derive from filename
	name := configName
	if name == "" {
		baseName := filepath.Base(relPath)
		name = strings.TrimSuffix(baseName, filepath.Ext(baseName))
	}

	// Compute dest path
	var dest = customTarget

	if dest == "" {
		dest = filepath.Join(g.opts.DestPrefix, relPath)
		// Ensure forward slashes for container paths
		dest = filepath.ToSlash(dest)
	}

	return &WeightFile{
		Name:             name,
		Dest:             dest,
		Digest:           digest,
		DigestOriginal:   digest,
		Size:             size,
		SizeUncompressed: size,
		MediaType:        MediaTypeWeightLayer,
	}, nil
}
