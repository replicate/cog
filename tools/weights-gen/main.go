// tools/weights-gen/main.go
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/model"
)

func main() {
	var (
		destPrefix string
		outputPath string
		outputDir  string
		count      int
		minSize    string
		maxSize    string
		noLock     bool
	)

	cmd := &cobra.Command{
		Use:   "weights-gen",
		Short: "Generate random weight files and optionally a weights.lock file",
		Long: `This tool generates random weight files and optionally a weights.lock file for testing.

It creates random binary files of configurable size and computes their digests,
simulating what a future "cog weights" command would do with real weight files.

By default, both weight files and a weights.lock file are generated. Use --no-lock
to generate only the weight files without the lock file.

The lock file's dest paths default to /cache/ for container paths.
Use --dest-prefix to override this.

Examples:
  # Generate 3 random files (25-50MB each) with defaults (includes weights.lock)
  weights-gen

  # Generate 5 files between 12-50MB
  weights-gen --count 5 --min-size 12mb --max-size 50mb

  # Generate files to a specific output directory
  weights-gen --output-dir ./my-weights/

  # Generate only weight files without a lock file
  weights-gen --output-dir ./my-weights/ --no-lock

  # Generate files with custom destination prefix
  weights-gen --output-dir ./my-weights/ --dest-prefix /models/`,
		RunE: func(cmd *cobra.Command, args []string) error {
			minBytes, err := parseSize(minSize)
			if err != nil {
				return fmt.Errorf("invalid --min-size: %w", err)
			}
			maxBytes, err := parseSize(maxSize)
			if err != nil {
				return fmt.Errorf("invalid --max-size: %w", err)
			}
			if minBytes > maxBytes {
				return fmt.Errorf("--min-size (%s) cannot be greater than --max-size (%s)", minSize, maxSize)
			}
			if count < 1 {
				return fmt.Errorf("--count must be at least 1")
			}

			return generateWeights(outputDir, destPrefix, outputPath, count, minBytes, maxBytes, !noLock)
		},
	}

	cmd.Flags().StringVar(&destPrefix, "dest-prefix", "/cache/", "Prefix for destination paths in lock file (default: /cache/)")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "weights.lock", "Output path for weights.lock file")
	cmd.Flags().StringVar(&outputDir, "output-dir", "", "Directory to write generated weight files (default: temp dir)")
	cmd.Flags().IntVarP(&count, "count", "n", 3, "Number of random weight files to generate")
	cmd.Flags().StringVar(&minSize, "min-size", "25mb", "Minimum file size (e.g., 12mb, 25MB, 1gb)")
	cmd.Flags().StringVar(&maxSize, "max-size", "50mb", "Maximum file size (e.g., 50mb, 100MB, 1gb)")
	cmd.Flags().BoolVar(&noLock, "no-lock", false, "Skip generating the weights.lock file")

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// parseSize parses a size string like "25mb", "50MB", "1gb" into bytes.
func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, fmt.Errorf("empty size string")
	}

	var multiplier int64 = 1
	var numStr string

	switch {
	case strings.HasSuffix(s, "gb"):
		multiplier = 1024 * 1024 * 1024
		numStr = strings.TrimSuffix(s, "gb")
	case strings.HasSuffix(s, "mb"):
		multiplier = 1024 * 1024
		numStr = strings.TrimSuffix(s, "mb")
	case strings.HasSuffix(s, "kb"):
		multiplier = 1024
		numStr = strings.TrimSuffix(s, "kb")
	case strings.HasSuffix(s, "b"):
		numStr = strings.TrimSuffix(s, "b")
	default:
		// Assume bytes if no suffix
		numStr = s
	}

	num, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number: %s", numStr)
	}
	if num < 0 {
		return 0, fmt.Errorf("size cannot be negative")
	}

	return int64(num * float64(multiplier)), nil
}

func generateWeights(outputDir, destPrefix, outputPath string, count int, minSize, maxSize int64, generateLock bool) error {
	// Determine where to write files
	var filesDir string

	if outputDir != "" {
		// User specified an output directory
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
		filesDir = outputDir
	} else {
		// Use a temp directory (not cleaned up so user can access the files)
		tmpDir, err := os.MkdirTemp("", "weights-gen-")
		if err != nil {
			return fmt.Errorf("create temp directory: %w", err)
		}
		filesDir = tmpDir
	}

	// Seed random number generator
	// Using math/rand is fine for test data generation - we don't need crypto randomness
	rng := rand.New(rand.NewSource(time.Now().UnixNano())) //nolint:gosec

	// Generate random files
	fmt.Printf("Generating %d random weight files (%s - %s each)...\n",
		count, formatSize(minSize), formatSize(maxSize))

	var files []model.WeightFile
	for i := 1; i <= count; i++ {
		// Random size between min and max
		var size int64
		if minSize == maxSize {
			size = minSize
		} else {
			size = minSize + rng.Int63n(maxSize-minSize+1)
		}

		filename := fmt.Sprintf("weights-%03d.bin", i)
		filePath := filepath.Join(filesDir, filename)

		fmt.Printf("  Creating %s (%s)...\n", filename, formatSize(size))

		if err := generateRandomFile(filePath, size, rng); err != nil {
			return fmt.Errorf("generate %s: %w", filename, err)
		}

		if generateLock {
			wf, err := processFile(filePath, filesDir, destPrefix)
			if err != nil {
				return fmt.Errorf("process %s: %w", filename, err)
			}

			files = append(files, *wf)
			fmt.Printf("  Processed: %s -> %s\n", wf.Name, wf.Dest)
		} else {
			fmt.Printf("  Created: %s\n", filename)
		}
	}

	if generateLock {
		lock := &model.WeightsLock{
			Version: "1",
			Created: time.Now().UTC(),
			Files:   files,
		}

		if err := lock.Save(outputPath); err != nil {
			return err
		}

		fmt.Printf("\nGenerated %s with %d files\n", outputPath, len(files))
	} else {
		fmt.Printf("\nGenerated %d weight files (no lock file)\n", count)
	}

	fmt.Printf("Weight files written to: %s\n", filesDir)
	return nil
}

// generateRandomFile creates a file filled with random data of the specified size.
func generateRandomFile(path string, size int64, rng *rand.Rand) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	// Write in chunks to avoid allocating huge buffers
	const chunkSize = 1024 * 1024 // 1MB chunks
	chunk := make([]byte, chunkSize)
	remaining := size

	for remaining > 0 {
		toWrite := min(remaining, chunkSize)

		// Fill chunk with random data
		_, _ = rng.Read(chunk[:toWrite])

		if _, err := f.Write(chunk[:toWrite]); err != nil {
			return fmt.Errorf("write: %w", err)
		}
		remaining -= toWrite
	}

	return nil
}

// formatSize formats bytes into a human-readable string.
func formatSize(bytes int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)

	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1fGB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1fMB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1fKB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}

func processFile(path, baseDir, destPrefix string) (*model.WeightFile, error) {
	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	// Compute digest
	hash := sha256.Sum256(data)
	digest := "sha256:" + hex.EncodeToString(hash[:])

	// Compute relative path for dest
	relPath, err := filepath.Rel(baseDir, path)
	if err != nil {
		return nil, fmt.Errorf("rel path: %w", err)
	}
	dest := filepath.Join(destPrefix, relPath)
	// Normalize to forward slashes for container paths
	dest = strings.ReplaceAll(dest, "\\", "/")

	// Generate a simple identifier from the filename (without extension)
	baseName := filepath.Base(path)
	name := baseName[:len(baseName)-len(filepath.Ext(baseName))]

	size := int64(len(data))

	return &model.WeightFile{
		Name:             name,
		Dest:             dest,
		Digest:           digest,
		DigestOriginal:   digest,
		Size:             size,
		SizeUncompressed: size,
		MediaType:        model.MediaTypeWeightsLayer,
	}, nil
}
