package cli

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/image"
	"github.com/replicate/cog/pkg/predict"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/spf13/cobra"
)

var (
	trainInputFlags []string
)

func newTrainCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "train",
		Short: "Run a training",
		Long: `Run a training.

It will build the model in the current directory and train it.`,
		RunE:   cmdTrain,
		Args:   cobra.MaximumNArgs(1),
		Hidden: true,
	}
	addBuildProgressOutputFlag(cmd)
	cmd.Flags().StringArrayVarP(&trainInputFlags, "input", "i", []string{}, "Inputs, in the form name=value. if value is prefixed with @, then it is read from a file on disk. E.g. -i path=@image.jpg")

	return cmd
}

func cmdTrain(cmd *cobra.Command, args []string) error {
	imageName := ""
	volumes := []docker.Volume{}
	gpus := ""
	weightsPath := "weights"

	// Build image

	cfg, projectDir, err := config.GetConfig(projectDirFlag)
	if err != nil {
		return err
	}

	if imageName, err = image.BuildBase(cfg, projectDir, buildProgressOutput); err != nil {
		return err
	}

	// Base image doesn't have /src in it, so mount as volume
	volumes = append(volumes, docker.Volume{
		Source:      projectDir,
		Destination: "/src",
	})

	if cfg.Build.GPU {
		gpus = "all"
	}

	console.Info("")
	console.Infof("Starting Docker image %s...", imageName)

	predictor := predict.NewPredictor(docker.RunOptions{
		GPUs:    gpus,
		Image:   imageName,
		Volumes: volumes,
		Args:    []string{"python", "-m", "cog.server.http", "--x-mode", "train"},
	}, "trainings")

	go func() {
		captureSignal := make(chan os.Signal, 1)
		signal.Notify(captureSignal, syscall.SIGINT)

		<-captureSignal

		console.Info("Stopping container...")
		if err := predictor.Stop(); err != nil {
			console.Warnf("Failed to stop container: %s", err)
		}
	}()

	if err := predictor.Start(os.Stderr); err != nil {
		return err
	}

	// FIXME: will not run on signal
	defer func() {
		console.Debugf("Stopping container...")
		if err := predictor.Stop(); err != nil {
			console.Warnf("Failed to stop container: %s", err)
		}
	}()

	return predictIndividualInputs(predictor, trainInputFlags, weightsPath)
}
