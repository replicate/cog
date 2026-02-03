// pkg/model/weights_lock_generator.go
package model

import (
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
func (g *WeightsLockGenerator) Generate(projectDir string, sources []config.WeightSource) (*WeightsLock, error) {
	lock, _, err := g.GenerateWithFilePaths(projectDir, sources)
	return lock, err
}

// GenerateWithFilePaths processes weight sources and returns a WeightsLock along with
// a map of weight names to their absolute file paths.
func (g *WeightsLockGenerator) GenerateWithFilePaths(projectDir string, sources []config.WeightSource) (*WeightsLock, map[string]string, error) {
	var files []WeightFile
	filePaths := make(map[string]string)

	for _, src := range sources {
		sourcePath := filepath.Join(projectDir, src.Source)

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

				wf, err := g.processFile(path, relPath, "")
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
			wf, err := g.processFile(sourcePath, src.Source, src.Target)
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
func (g *WeightsLockGenerator) processFile(absPath, relPath, customTarget string) (*WeightFile, error) {
	// Open and read file for hashing
	f, err := os.Open(absPath)
	if err != nil {
		return nil, fmt.Errorf("open weight file %s: %w", relPath, err)
	}
	defer f.Close()

	// Get file info for size
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat weight file %s: %w", relPath, err)
	}
	size := info.Size()

	// Compute SHA256 digest
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return nil, fmt.Errorf("hash weight file %s: %w", relPath, err)
	}
	digest := "sha256:" + hex.EncodeToString(hash.Sum(nil))

	// Compute name (filename without extension)
	baseName := filepath.Base(relPath)
	name := strings.TrimSuffix(baseName, filepath.Ext(baseName))

	// Compute dest path
	var dest string
	if customTarget != "" {
		dest = customTarget
	} else {
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
		MediaType:        MediaTypeWeightsLayer,
	}, nil
}
