// tools/weights-gen generates random weight directories and (optionally) a
// weights.lock file for testing. It packs each directory through the same
// packer used by `cog weights build`, so the lockfile matches what a real
// build would produce.
package main

import (
	"context"
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
		outputPath string
		outputDir  string
		count      int
		minSize    string
		maxSize    string
		filesPerWeight int
		noLock     bool
		destPrefix string
	)

	cmd := &cobra.Command{
		Use:   "weights-gen",
		Short: "Generate random weight directories and optionally a weights.lock file",
		Long: `Generates random weight directories (each containing several files) and optionally
a weights.lock file for testing. Each directory is packed via the real packer to
produce layer descriptors that match what 'cog weights build' would write.

Examples:
  # Default: 3 directories of ~3 files each, 25-50 MB per file, with weights.lock
  weights-gen

  # Put output in a specific directory
  weights-gen --output-dir ./my-weights/

  # Skip the lockfile (directories only)
  weights-gen --output-dir ./my-weights/ --no-lock
`,
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
			if filesPerWeight < 1 {
				return fmt.Errorf("--files-per-weight must be at least 1")
			}

			return generateWeights(cmd.Context(),
				outputDir, destPrefix, outputPath,
				count, filesPerWeight, minBytes, maxBytes, !noLock)
		},
	}

	cmd.Flags().StringVar(&destPrefix, "dest-prefix", "/src/weights/", "Prefix for target paths in lock file")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "weights.lock", "Output path for weights.lock file")
	cmd.Flags().StringVar(&outputDir, "output-dir", "", "Directory to write generated weight directories (default: temp dir)")
	cmd.Flags().IntVarP(&count, "count", "n", 3, "Number of weight directories to generate")
	cmd.Flags().IntVar(&filesPerWeight, "files-per-weight", 3, "Number of files per weight directory")
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

func generateWeights(ctx context.Context,
	outputDir, destPrefix, outputPath string,
	count, filesPerWeight int,
	minSize, maxSize int64,
	generateLock bool,
) error {
	filesDir, err := resolveOutputDir(outputDir)
	if err != nil {
		return err
	}

	// math/rand is fine for test data; we don't need crypto randomness.
	rng := rand.New(rand.NewSource(time.Now().UnixNano())) //nolint:gosec

	fmt.Printf("Generating %d weight directories (%d file(s) each, %s - %s per file)...\n",
		count, filesPerWeight, formatSize(minSize), formatSize(maxSize))

	var cacheDir string
	if generateLock {
		cacheDir, err = os.MkdirTemp("", "weights-gen-cache-")
		if err != nil {
			return fmt.Errorf("create pack cache dir: %w", err)
		}
		defer os.RemoveAll(cacheDir) //nolint:errcheck // best-effort cleanup
	}

	var entries []model.WeightLockEntry
	for i := 1; i <= count; i++ {
		name := fmt.Sprintf("weights-%03d", i)
		weightDir := filepath.Join(filesDir, name)
		if err := os.MkdirAll(weightDir, 0o755); err != nil {
			return fmt.Errorf("create weight dir %s: %w", weightDir, err)
		}

		for j := 1; j <= filesPerWeight; j++ {
			size := minSize
			if maxSize > minSize {
				size = minSize + rng.Int63n(maxSize-minSize+1)
			}
			filename := fmt.Sprintf("file-%03d.bin", j)
			filePath := filepath.Join(weightDir, filename)
			fmt.Printf("  %s/%s (%s)\n", name, filename, formatSize(size))
			if err := generateRandomFile(filePath, size, rng); err != nil {
				return fmt.Errorf("generate %s: %w", filePath, err)
			}
		}

		if generateLock {
			target := filepath.ToSlash(filepath.Join(destPrefix, name))
			entry, err := packDirectoryToEntry(ctx, name, target, weightDir, cacheDir)
			if err != nil {
				return fmt.Errorf("pack %s: %w", name, err)
			}
			entries = append(entries, entry)
		}
	}

	if generateLock {
		lock := &model.WeightsLock{
			Version: model.WeightsLockVersion,
			Created: time.Now().UTC(),
			Weights: entries,
		}
		if err := lock.Save(outputPath); err != nil {
			return err
		}
		fmt.Printf("\nGenerated %s with %d weight(s)\n", outputPath, len(entries))
	} else {
		fmt.Printf("\nGenerated %d weight directories (no lock file)\n", count)
	}

	fmt.Printf("Weight directories written to: %s\n", filesDir)
	return nil
}

// resolveOutputDir picks an output directory, creating it if needed.
// An empty string falls back to a temp directory (not cleaned up, so the
// user can inspect the output afterwards).
func resolveOutputDir(outputDir string) (string, error) {
	if outputDir != "" {
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			return "", fmt.Errorf("create output directory: %w", err)
		}
		return outputDir, nil
	}
	tmp, err := os.MkdirTemp("", "weights-gen-")
	if err != nil {
		return "", fmt.Errorf("create temp directory: %w", err)
	}
	return tmp, nil
}

// packDirectoryToEntry packs a directory into tar layers (via the real
// packer), assembles the v1 OCI manifest, and builds a lockfile entry that
// matches what `cog weights build` would produce. cacheDir holds the tar
// files produced by the packer; the caller owns its lifetime.
func packDirectoryToEntry(ctx context.Context, name, target, sourceDir, cacheDir string) (model.WeightLockEntry, error) {
	layers, err := model.Pack(ctx, sourceDir, &model.PackOptions{TempDir: cacheDir})
	if err != nil {
		return model.WeightLockEntry{}, fmt.Errorf("pack: %w", err)
	}

	img, err := model.BuildWeightManifestV1(layers, model.WeightManifestV1Metadata{
		Name:   name,
		Target: target,
	})
	if err != nil {
		return model.WeightLockEntry{}, fmt.Errorf("build manifest: %w", err)
	}

	digest, err := img.Digest()
	if err != nil {
		return model.WeightLockEntry{}, fmt.Errorf("manifest digest: %w", err)
	}

	return model.NewWeightLockEntry(name, target, digest.String(), layers), nil
}

// generateRandomFile creates a file filled with random bytes of the given size.
func generateRandomFile(path string, size int64, rng *rand.Rand) error {
	f, err := os.Create(path) //nolint:gosec // path is under a caller-chosen output dir
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close() //nolint:errcheck

	const chunkSize = 1024 * 1024
	chunk := make([]byte, chunkSize)
	remaining := size
	for remaining > 0 {
		toWrite := min(remaining, chunkSize)
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



