// tools/weights-lock-gen/main.go
package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/model"
)

func main() {
	var (
		filesDir   string
		destPrefix string
		outputPath string
	)

	cmd := &cobra.Command{
		Use:   "weights-lock-gen",
		Short: "Generate a placeholder weights.lock file from local files",
		Long: `This tool generates a weights.lock file by reading local files,
computing their digests, and compressing them with gzip.

This is a placeholder tool for testing the OCI bundle format.
It will be replaced by proper weight resolution in the declarative weights implementation.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return generateWeightsLock(filesDir, destPrefix, outputPath)
		},
	}

	cmd.Flags().StringVar(&filesDir, "files", "", "Directory containing weight files (required)")
	cmd.Flags().StringVar(&destPrefix, "dest-prefix", "/cache/", "Prefix for destination paths")
	cmd.Flags().StringVar(&outputPath, "output", "weights.lock", "Output path for weights.lock")
	_ = cmd.MarkFlagRequired("files")

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func generateWeightsLock(filesDir, destPrefix, outputPath string) error {
	var files []model.WeightFile

	err := filepath.WalkDir(filesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		wf, err := processFile(path, filesDir, destPrefix)
		if err != nil {
			return fmt.Errorf("process %s: %w", path, err)
		}

		files = append(files, *wf)
		fmt.Printf("Processed: %s -> %s (%.2f MB compressed)\n",
			wf.Name, wf.Dest, float64(wf.Size)/1024/1024)

		return nil
	})
	if err != nil {
		return fmt.Errorf("walk files: %w", err)
	}

	if len(files) == 0 {
		return fmt.Errorf("no files found in %s", filesDir)
	}

	lock := &model.WeightsLock{
		Version: "1",
		Created: time.Now().UTC(),
		Files:   files,
	}

	if err := lock.Save(outputPath); err != nil {
		return err
	}

	fmt.Printf("\nGenerated %s with %d files\n", outputPath, len(files))
	return nil
}

func processFile(path, baseDir, destPrefix string) (*model.WeightFile, error) {
	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	// Compute original digest
	originalHash := sha256.Sum256(data)
	digestOriginal := "sha256:" + hex.EncodeToString(originalHash[:])

	// Compress with gzip
	var compressed bytes.Buffer
	gw := gzip.NewWriter(&compressed)
	if _, err := io.Copy(gw, bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("compress: %w", err)
	}
	if err := gw.Close(); err != nil {
		return nil, fmt.Errorf("close gzip: %w", err)
	}

	// Compute compressed digest
	compressedHash := sha256.Sum256(compressed.Bytes())
	digest := "sha256:" + hex.EncodeToString(compressedHash[:])

	// Compute relative path for dest
	relPath, err := filepath.Rel(baseDir, path)
	if err != nil {
		return nil, fmt.Errorf("rel path: %w", err)
	}
	dest := filepath.Join(destPrefix, relPath)
	// Normalize to forward slashes for container paths
	dest = strings.ReplaceAll(dest, "\\", "/")

	return &model.WeightFile{
		Name:             filepath.Base(path),
		Dest:             dest,
		Source:           "file://" + path,
		DigestOriginal:   digestOriginal,
		Digest:           digest,
		Size:             int64(compressed.Len()),
		SizeUncompressed: int64(len(data)),
		MediaType:        model.MediaTypeWeightsLayerGzip,
		ContentType:      "application/octet-stream", // Placeholder
	}, nil
}
