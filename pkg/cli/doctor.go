package cli

import (
	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/doctor"
	"github.com/replicate/cog/pkg/util/console"
)

func newDoctorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check for common issues with your project",
		Args:  cobra.NoArgs,
		RunE:  doctorCommand,
	}

	return cmd
}

func doctorCommand(cmd *cobra.Command, args []string) error {

	console.Info("[🩺] Checking for issues with your project.")

	// Check for weights
	doctor.CheckFiles()

	return nil
}

/*
func addUseCudaBaseImageFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&buildUseCudaBaseImage, "use-cuda-base-image", "auto", "Use Nvidia CUDA base image, 'true' (default) or 'false' (use python base image). False results in a smaller image but may cause problems for non-torch projects")
}
*/
