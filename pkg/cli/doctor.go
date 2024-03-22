package cli

import (
	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/doctor"
	"github.com/replicate/cog/pkg/util/console"
)

var folder string
var bucket string

func newDoctorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check for common issues with your project",
		Args:  cobra.NoArgs,
		RunE:  doctorCommand,
	}

	addBucketFlag(cmd)
	addFolderFlag(cmd)

	return cmd
}

func doctorCommand(cmd *cobra.Command, args []string) error {

	console.Info("🩺 Checking for issues with your project.\n")

	err := doctor.CheckFiles(bucket, folder)
	if err != nil {
		return err
	}

	return nil
}

func addBucketFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&bucket, "bucket", "replicate-weights", "The target GCS bucket.")
}

func addFolderFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&folder, "folder", "", "The target cache folder in your GCS bucket.")
}
