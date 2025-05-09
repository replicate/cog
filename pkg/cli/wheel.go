package cli

import (
	"fmt"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"

	"github.com/replicate/cog/pkg/dockerfile"
)

var (
	wheelOutPath string
)

func newWheelCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wheel",
		Short: "Export the cog wheel",
		Long: `Export the cog wheel embedded in the cog binary.

If a path is provided, the wheel will be written to that path.
Otherwise, the wheel will be written to the current directory with a default name.

This is useful for testing Cog models locally, outside of a Docker container.`,
		RunE:   cmdWheel,
		Hidden: true,
		Args:   cobra.MaximumNArgs(0),
	}

	cmd.Flags().StringVarP(&wheelOutPath, "output", "o", "", "Path to write the wheel to")
	return cmd
}

func cmdWheel(*cobra.Command, []string) error {
	filename, err := dockerfile.WheelFilename()
	if err != nil {
		return err
	}
	fullPath := filepath.Join(wheelOutPath, filename)
	data, _, err := dockerfile.ReadWheelFile()
	if err != nil {
		return err
	}

	if err = os.WriteFile(fullPath, data, 0644); err != nil {
		return err
	} else {
		fmt.Print(fullPath)
	}
	return nil
}
