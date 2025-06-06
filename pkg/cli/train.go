package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/model/factory"
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
	addFastFlag(cmd)
	addConfigFlag(cmd)

	cmd.Flags().StringArrayVarP(&trainInputFlags, "input", "i", []string{}, "Inputs, in the form name=value. if value is prefixed with @, then it is read from a file on disk. E.g. -i path=@image.jpg")
	cmd.Flags().StringArrayVarP(&trainEnvFlags, "env", "e", []string{}, "Environment variables, in the form name=value")
	cmd.Flags().StringVarP(&trainOutPath, "output", "o", "weights", "Output path")

	return cmd
}

func cmdTrain(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		return err
	}

	volumes := []command.Volume{}
	gpus := gpusFlag

	cfg, projectDir, err := config.GetConfig(configFilename)
	if err != nil {
		return err
	}

	var cogModel *model.Model

	if len(args) == 0 {
		// Build image

		settings := buildSettings(cmd, cfg, true, projectDir)

		modelFactory, err := factory.New(dockerClient)
		if err != nil {
			return err
		}
		builtModel, buildInfo, err := modelFactory.Build(ctx, settings)
		if err != nil {
			return err
		}
		cogModel = builtModel

		// dockerfile images for predict don't have /src in it, so mount as volume
		if buildInfo.BaseImageOnly {
			volumes = append(volumes, command.Volume{
				Source:      projectDir,
				Destination: "/src",
			})
		}
	} else {
		// Use existing image
		imageName := args[0]

		_, err := dockerClient.Pull(ctx, imageName, false)
		if err != nil {
			return fmt.Errorf("Failed to pull image %q: %w", imageName, err)
		}

		cogModel, err = model.Resolve(ctx, imageName, model.WithProvider(dockerClient), model.WithResolveMode(docker.ResolveModeLocal))
		if err != nil {
			return fmt.Errorf("Failed to resolve model %q: %w", imageName, err)
		}
	}

	if gpus == "" && cogModel.Config.Build.GPU {
		gpus = "all"
	}

	console.Info("")
	console.Infof("Starting Docker image %s...", cogModel.Name())

	predictor, err := predict.NewPredictor(ctx, command.RunOptions{
		GPUs:    gpus,
		Image:   cogModel.ImageRef(),
		Volumes: volumes,
		Env:     trainEnvFlags,
		Args:    []string{"python", "-m", "cog.server.http", "--x-mode", "train"},
	}, true, dockerClient)
	if err != nil {
		return err
	}

	go func() {
		captureSignal := make(chan os.Signal, 1)
		signal.Notify(captureSignal, syscall.SIGINT)

		<-captureSignal

		console.Info("Stopping container...")
		if err := predictor.Stop(ctx); err != nil {
			console.Warnf("Failed to stop container: %s", err)
		}
	}()

	if err := predictor.Start(ctx, os.Stderr, time.Duration(setupTimeout)*time.Second); err != nil {
		return err
	}

	// FIXME: will not run on signal
	defer func() {
		console.Debugf("Stopping container...")
		// use background context to ensure stop signal is still sent after root context is canceled
		if err := predictor.Stop(context.Background()); err != nil {
			console.Warnf("Failed to stop container: %s", err)
		}
	}()

	return predictIndividualInputs(*predictor, trainInputFlags, trainOutPath, true)
}
