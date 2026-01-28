package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/predict"
	"github.com/replicate/cog/pkg/registry"
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

	imageName := ""
	volumes := []command.Volume{}
	gpus := gpusFlag

	resolver := model.NewResolver(dockerClient, registry.NewRegistryClient())

	if len(args) == 0 {
		// Build image
		src, err := model.NewSource(configFilename)
		if err != nil {
			return err
		}

		if src.Config.Build != nil && src.Config.Build.Fast {
			buildFast = true
		}

		m, err := resolver.BuildBase(ctx, src, buildBaseOptionsFromFlags(cmd))
		if err != nil {
			return err
		}
		imageName = m.ImageRef()

		// Base image doesn't have /src in it, so mount as volume
		volumes = append(volumes, command.Volume{
			Source:      src.ProjectDir,
			Destination: "/src",
		})

		if gpus == "" && m.HasGPU() {
			gpus = "all"
		}
	} else {
		// Use existing image
		imageName = args[0]

		// Pull the image (if needed) and validate it's a Cog model
		ref, err := model.ParseRef(imageName)
		if err != nil {
			return err
		}
		m, err := resolver.Pull(ctx, ref)
		if err != nil {
			return err
		}

		if gpus == "" && m.HasGPU() {
			gpus = "all"
		}
		if m.IsFast() {
			buildFast = true
		}
	}

	console.Info("")
	console.Infof("Starting Docker image %s...", imageName)

	predictor, err := predict.NewPredictor(ctx, command.RunOptions{
		GPUs:    gpus,
		Image:   imageName,
		Volumes: volumes,
		Env:     trainEnvFlags,
		Args:    []string{"python", "-m", "cog.server.http", "--x-mode", "train"},
	}, true, buildFast, dockerClient)
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
