package cli

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/image"
	"github.com/replicate/cog/pkg/predict"
	"github.com/replicate/cog/pkg/util/console"
)

var (
	trainEnvFlags   []string
	trainInputFlags []string
	trainOutPath    string
)

func newTrainCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "train [image]",
		Short: "Run a training",
		Long: `Run a training.

If 'image' is passed, it will run the training on that Docker image.
It must be an image that has been built by Cog.

Otherwise, it will build the model in the current directory and train it.`,
		RunE:   cmdTrain,
		Args:   cobra.MaximumNArgs(1),
		Hidden: true,
	}

	addBuildProgressOutputFlag(cmd)
	addDockerfileFlag(cmd)
	addUseCudaBaseImageFlag(cmd)
	addGpusFlag(cmd)
	addUseCogBaseImageFlag(cmd)

	cmd.Flags().StringArrayVarP(&trainInputFlags, "input", "i", []string{}, "Inputs, in the form name=value. if value is prefixed with @, then it is read from a file on disk. E.g. -i path=@image.jpg")
	cmd.Flags().StringArrayVarP(&trainEnvFlags, "env", "e", []string{}, "Environment variables, in the form name=value")
	cmd.Flags().StringVarP(&trainOutPath, "output", "o", "weights", "Output path")

	return cmd
}

func cmdTrain(cmd *cobra.Command, args []string) error {
	imageName := ""
	volumes := []docker.Volume{}
	gpus := gpusFlag

	if len(args) == 0 {
		// Build image

		cfg, projectDir, err := config.GetConfig(projectDirFlag)
		if err != nil {
			return err
		}

		if imageName, err = image.BuildBase(cfg, projectDir, buildUseCudaBaseImage, buildUseCogBaseImage, buildProgressOutput); err != nil {
			return err
		}

		// Base image doesn't have /src in it, so mount as volume
		volumes = append(volumes, docker.Volume{
			Source:      projectDir,
			Destination: "/src",
		})

		if gpus == "" && cfg.Build.GPU {
			gpus = "all"
		}
	} else {
		// Use existing image
		imageName = args[0]

		exists, err := docker.ImageExists(imageName)
		if err != nil {
			return fmt.Errorf("Failed to determine if %s exists: %w", imageName, err)
		}
		if !exists {
			console.Infof("Pulling image: %s", imageName)
			if err := docker.Pull(imageName); err != nil {
				return fmt.Errorf("Failed to pull %s: %w", imageName, err)
			}
		}
		conf, err := image.GetConfig(imageName)
		if err != nil {
			return err
		}
		if gpus == "" && conf.Build.GPU {
			gpus = "all"
		}
	}

	console.Info("")
	console.Infof("Starting Docker image %s...", imageName)

	predictor := predict.NewPredictor(docker.RunOptions{
		GPUs:    gpus,
		Image:   imageName,
		Volumes: volumes,
		Env:     trainEnvFlags,
		Args:    []string{"python", "-m", "cog.server.http", "--x-mode", "train"},
	})

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

	return predictIndividualInputs(predictor, trainInputFlags, trainOutPath)
}
