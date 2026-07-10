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
	"github.com/replicate/cog/pkg/weights"
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
		RunE:       cmdTrain,
		Args:       cobra.MaximumNArgs(1),
		Hidden:     true,
		Deprecated: "the train command will be removed in a future version of Cog",
	}

	addBuildProgressOutputFlag(cmd)
	addDockerfileFlag(cmd)
	addUseCudaBaseImageFlag(cmd)
	addGpusFlag(cmd)
	addUseCogBaseImageFlag(cmd)
	addConfigFlag(cmd)

	cmd.Flags().StringArrayVarP(&trainInputFlags, "input", "i", []string{}, "Inputs, in the form name=value. if value is prefixed with @, then it is read from a file on disk. E.g. -i path=@image.jpg")
	cmd.Flags().StringArrayVarP(&trainEnvFlags, "env", "e", []string{}, "Environment variables, in the form name=value")
	cmd.Flags().StringVarP(&trainOutPath, "output", "o", "weights", "Output path")

	return cmd
}

func cmdTrain(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var (
		imageName      string
		volumes        = []command.Volume{}
		gpus           = gpusFlag
		preparedInputs predict.Inputs
		inputsPrepared = false
		dockerClient   command.Command
		err            error
		m              *model.Model
	)

	// Managed-weight mounts only apply when we have cog.yaml in scope.
	var wm *weights.Manager

	if len(args) == 0 {
		// Build image
		src, err := model.NewSource(configFilename)
		if err != nil {
			return err
		}
		defer src.Close()

		openAPISchemaJSON, openAPISchema, err := generateLocalOpenAPISchema(src)
		if err != nil {
			return err
		}
		preparedInputs, err = prepareIndividualInputs(trainInputFlags, openAPISchema, true)
		if err != nil {
			return err
		}
		inputsPrepared = true

		if err := weights.CheckDrift(src.ProjectDir, src.Config.Weights); err != nil {
			return err
		}

		dockerClient, err = docker.NewClient(ctx)
		if err != nil {
			return err
		}
		resolver := model.NewResolver(dockerClient, registry.NewRegistryClient())

		console.Info("Building Docker image from environment in cog.yaml...")
		console.Info("")
		buildOpts := serveBuildOptions(cmd)
		buildOpts.OpenAPISchema = openAPISchemaJSON
		m, err = resolver.Build(ctx, src, buildOpts)
		if err != nil {
			return err
		}
		imageName = m.ImageRef()

		// ExcludeSource build doesn't have /src in it, so mount as volume
		volumes = append(volumes, command.Volume{
			Source:      src.ProjectDir,
			Destination: "/src",
		})

		if gpus == "" && m.HasGPU() {
			gpus = "all"
		}

		wm, err = newWeightManager(src)
		if err != nil {
			return err
		}
	} else {
		dockerClient, err = docker.NewClient(ctx)
		if err != nil {
			return err
		}
		resolver := model.NewResolver(dockerClient, registry.NewRegistryClient())

		// Use existing image
		imageName = args[0]

		// Pull the image (if needed) and validate it's a Cog model
		ref, err := model.ParseRef(imageName)
		if err != nil {
			return err
		}
		m, err = resolver.Pull(ctx, ref)
		if err != nil {
			return err
		}

		// Preflight-validate inputs when the image has a usable input component;
		// otherwise fall back to the runtime schema after the container starts.
		if m.Schema != nil && predict.HasInputComponent(m.Schema, true) {
			preparedInputs, err = prepareIndividualInputs(trainInputFlags, m.Schema, true)
			if err != nil {
				return err
			}
			inputsPrepared = true
		}

		if gpus == "" && m.HasGPU() {
			gpus = "all"
		}
	}

	console.Info("")
	console.Info("Starting Docker image and running setup()...")

	predictor, err := predict.NewPredictor(ctx, predict.PredictorOptions{
		RunOptions: command.RunOptions{
			GPUs:    gpus,
			Image:   imageName,
			Volumes: volumes,
			Env:     trainEnvFlags,
			Args:    []string{"python", "-m", "cog.server.http", "--x-mode", "train"},
		},
		IsTrain:       true,
		Docker:        dockerClient,
		WeightManager: wm,
	})
	if err != nil {
		return err
	}

	if err := predictor.Start(ctx, os.Stderr, time.Duration(setupTimeout)*time.Second); err != nil {
		return err
	}

	// Use background context to ensure stop signal is still sent after root context is canceled by signal
	defer func() {
		console.Debugf("Stopping container...")
		if err := predictor.Stop(context.Background()); err != nil {
			console.Warnf("Failed to stop container: %s", err)
		}
	}()

	// Validate against the runtime schema when the image label was unusable.
	if !inputsPrepared {
		schema, err := predictor.GetSchema()
		if err != nil {
			return err
		}
		preparedInputs, err = prepareIndividualInputs(trainInputFlags, schema, true)
		if err != nil {
			return err
		}
	}

	return runPrediction(*predictor, preparedInputs, trainOutPath, true, false)
}
