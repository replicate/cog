package cli

import (
	"github.com/spf13/cobra"
)

var (
	gpusFlag string
)

func addGpusFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&gpusFlag, "gpus", "", "GPU devices to add to the container, in the same format as `docker run --gpus`.")
}

func newRunCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run [image]",
		Short: "Run a prediction",
		Long: `Run a prediction.

If 'image' is passed, it will run the prediction on that Docker image.
It must be an image that has been built by Cog.

Otherwise, it will build the model in the current directory and run
the prediction on that.`,
		Example: `  # Run a prediction with named inputs
  cog run -i prompt="a photo of a cat"

  # Pass a file as input
  cog run -i image=@photo.jpg

  # Save output to a file
  cog run -i image=@input.jpg -o output.png

  # Pass multiple inputs
  cog run -i prompt="sunset" -i width=1024 -i height=768

  # Run against a pre-built image
  cog run r8.im/your-username/my-model -i prompt="hello"

  # Pass inputs as JSON
  echo '{"prompt": "a cat"}' | cog run --json @-`,
		RunE:       runRun,
		Args:       cobra.MaximumNArgs(1),
		SuggestFor: []string{"predict", "infer"},
	}

	addUseCudaBaseImageFlag(cmd)
	addUseCogBaseImageFlag(cmd)
	addBuildProgressOutputFlag(cmd)
	addDockerfileFlag(cmd)
	addGpusFlag(cmd)
	addSetupTimeoutFlag(cmd)
	addConfigFlag(cmd)

	cmd.Flags().StringArrayVarP(&inputFlags, "input", "i", []string{}, "Inputs, in the form name=value. if value is prefixed with @, then it is read from a file on disk. E.g. -i path=@image.jpg")
	cmd.Flags().StringVarP(&outPath, "output", "o", "", "Output path")
	cmd.Flags().StringArrayVarP(&envFlags, "env", "e", []string{}, "Environment variables, in the form name=value")
	cmd.Flags().BoolVar(&useReplicateAPIToken, "use-replicate-token", false, "Pass REPLICATE_API_TOKEN from local environment into the model context")
	cmd.Flags().StringVar(&inputJSON, "json", "", "Pass inputs as JSON object, read from file (@inputs.json) or via stdin (@-)")

	return cmd
}

func runRun(cmd *cobra.Command, args []string) error {
	// This is the same handler as cog predict
	return cmdPredict(cmd, args)
}
