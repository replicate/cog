package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/util/console"
)

var weightsDestPrefix string

func newWeightsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "weights",
		Short: "Manage model weights",
		Long:  "Commands for managing model weight files.",
	}

	cmd.AddCommand(newWeightsBuildCommand())
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

	cmd.Flags().StringVar(&weightsDestPrefix, "dest-prefix", "/cache/", "Container path prefix for weights")
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
		DestPrefix: weightsDestPrefix,
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
