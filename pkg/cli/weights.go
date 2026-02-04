package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
)

var weightsDest string

func newWeightsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "weights",
		Short: "Manage model weights",
		Long:  "Commands for managing model weight files.",
	}

	cmd.AddCommand(newWeightsBuildCommand())
	cmd.AddCommand(newWeightsPushCommand())
	return cmd
}

func newWeightsBuildCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Generate weights.lock from weight sources in cog.yaml",
		Long: `Reads the weights section from cog.yaml, processes each weight source,
and generates a weights.lock file containing metadata (digests, sizes) for each file.`,
		Args: cobra.NoArgs,
		RunE: weightsBuildCommand,
	}

	cmd.Flags().StringVar(&weightsDest, "dest", "/cache/", "Container path prefix for weights")
	addConfigFlag(cmd)
	return cmd
}

func weightsBuildCommand(cmd *cobra.Command, args []string) error {
	src, err := model.NewSource(configFilename)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	cfg := src.Config
	projectDir := src.ProjectDir

	if len(cfg.Weights) == 0 {
		return fmt.Errorf("no weights defined in %s", configFilename)
	}

	console.Infof("Processing %d weight source(s)...", len(cfg.Weights))

	gen := model.NewWeightsLockGenerator(model.WeightsLockGeneratorOptions{
		DestPrefix: weightsDest,
	})

	lock, err := gen.Generate(projectDir, cfg.Weights)
	if err != nil {
		return fmt.Errorf("failed to generate weights lock: %w", err)
	}

	lockPath := filepath.Join(projectDir, model.WeightsLockFilename)
	if err := lock.Save(lockPath); err != nil {
		return fmt.Errorf("failed to save weights.lock: %w", err)
	}

	// Print summary
	var totalSize int64
	for _, f := range lock.Files {
		totalSize += f.Size
		console.Infof("  %s -> %s (%s)", f.Name, f.Dest, formatSize(f.Size))
	}

	console.Infof("\nGenerated %s with %d file(s) (%s total)",
		model.WeightsLockFilename, len(lock.Files), formatSize(totalSize))

	return nil
}

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

func newWeightsPushCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "push [IMAGE]",
		Short: "Push weights to a registry",
		Long: `Reads weights.lock and pushes weight files as an OCI artifact to a registry.

The registry is determined from the image name, which can be:
- Specified as an argument: cog weights push registry.example.com/user/model
- Set in cog.yaml as the 'image' field`,
		Args: cobra.MaximumNArgs(1),
		RunE: weightsPushCommand,
	}

	addConfigFlag(cmd)
	return cmd
}

func weightsPushCommand(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	src, err := model.NewSource(configFilename)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	cfg := src.Config
	projectDir := src.ProjectDir

	// Determine image name
	imageName := cfg.Image
	if len(args) > 0 {
		imageName = args[0]
	}
	if imageName == "" {
		return fmt.Errorf("no image specified; provide an argument or set 'image' in cog.yaml")
	}

	// Parse repository from image name (strip tag if present)
	ref, err := name.ParseReference(imageName, name.Insecure)
	if err != nil {
		return fmt.Errorf("invalid image reference %q: %w", imageName, err)
	}
	repo := ref.Context().Name()

	// Check weights.lock exists
	lockPath := filepath.Join(projectDir, model.WeightsLockFilename)
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		return fmt.Errorf("weights.lock not found; run 'cog weights build' first")
	}

	// Load weights.lock
	lock, err := model.LoadWeightsLock(lockPath)
	if err != nil {
		return fmt.Errorf("failed to load weights.lock: %w", err)
	}

	if len(lock.Files) == 0 {
		return fmt.Errorf("weights.lock contains no files")
	}

	// Generate filePaths map from weights config
	if len(cfg.Weights) == 0 {
		return fmt.Errorf("no weights defined in %s", configFilename)
	}

	gen := model.NewWeightsLockGenerator(model.WeightsLockGeneratorOptions{
		DestPrefix: weightsDest,
	})

	_, filePaths, err := gen.GenerateWithFilePaths(projectDir, cfg.Weights)
	if err != nil {
		return fmt.Errorf("failed to resolve weight files: %w", err)
	}

	// Push weight files as layers concurrently
	console.Infof("Pushing %d weight file(s) to %s...", len(lock.Files), repo)

	regClient := registry.NewRegistryClient()

	type pushResult struct {
		name   string
		digest string
		size   int64
		err    error
	}

	results := make(chan pushResult, len(lock.Files))
	var wg sync.WaitGroup

	for _, wf := range lock.Files {
		wg.Add(1)
		go func() {
			defer wg.Done()

			filePath, ok := filePaths[wf.Name]
			if !ok {
				results <- pushResult{name: wf.Name, err: fmt.Errorf("file path not found for weight %q", wf.Name)}
				return
			}

			// Create layer from file
			layer, err := tarball.LayerFromFile(filePath, tarball.WithMediaType(model.MediaTypeWeightsLayer))
			if err != nil {
				results <- pushResult{name: wf.Name, err: fmt.Errorf("create layer for %s: %w", wf.Name, err)}
				return
			}

			// Push layer to registry
			if err := regClient.WriteLayer(ctx, repo, layer); err != nil {
				results <- pushResult{name: wf.Name, err: fmt.Errorf("push layer %s: %w", wf.Name, err)}
				return
			}

			digest, _ := layer.Digest()
			size, _ := layer.Size()
			results <- pushResult{name: wf.Name, digest: digest.String(), size: size}
		}()
	}

	// Wait for all goroutines and close results channel
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results and report progress
	var totalSize int64
	var pushed int
	var errors []error

	for result := range results {
		pushed++
		if result.err != nil {
			errors = append(errors, result.err)
			console.Warnf("  [%d/%d] Failed: %s", pushed, len(lock.Files), result.err)
		} else {
			totalSize += result.size
			console.Infof("  [%d/%d] %s (%s)", pushed, len(lock.Files), result.name, formatSize(result.size))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to push %d/%d weight files", len(errors), len(lock.Files))
	}

	console.Infof("\nPushed %d weight blobs to %s", len(lock.Files), repo)
	console.Infof("Total: %s", formatSize(totalSize))

	return nil
}
