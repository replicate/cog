package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/tools/test-harness/internal/manifest"
)

func newListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List models in manifest",
		Long:  `Print a table of all models in the manifest with their requirements.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList()
		},
	}
}

func runList() error {
	// Load manifest
	mf, manifestPath, err := manifest.Load(manifestPath)
	if err != nil {
		return fmt.Errorf("loading manifest: %w", err)
	}
	fmt.Printf("Loaded manifest: %s\n\n", manifestPath)

	// Print models
	for _, m := range mf.Models {
		gpuTag := ""
		if m.GPU {
			gpuTag = " [GPU]"
		}

		envTag := ""
		if len(m.RequiresEnv) > 0 {
			envTag = fmt.Sprintf(" (requires: %s)", strings.Join(m.RequiresEnv, ", "))
		}

		fmt.Printf("  %-25s %s/%s%s%s\n", m.Name, m.Repo, m.Path, gpuTag, envTag)
	}

	fmt.Printf("\n%d model(s) total\n", len(mf.Models))
	return nil
}
