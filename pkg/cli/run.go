package cli

import (
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/image"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
)

var (
	runPorts []string
	gpusFlag string
)

func addGpusFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&gpusFlag, "gpus", "", "GPU devices to add to the container, in the same format as `docker run --gpus`.")
}

func newRunCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "run <command> [arg...]",
		Short:   "Run a command inside a Docker environment",
		RunE:    run,
		PreRunE: checkMutuallyExclusiveFlags,
		Args:    cobra.MinimumNArgs(1),
	}
	addBuildProgressOutputFlag(cmd)
	addDockerfileFlag(cmd)
	addUseCudaBaseImageFlag(cmd)
	addUseCogBaseImageFlag(cmd)
	addGpusFlag(cmd)
	addFastFlag(cmd)
	addLocalImage(cmd)
	addConfigFlag(cmd)
	addPipelineImage(cmd)

	flags := cmd.Flags()
	// Flags after first argument are considered args and passed to command

	// This is called `publish` for consistency with `docker run`
	cmd.Flags().StringArrayVarP(&runPorts, "publish", "p", []string{}, "Publish a container's port to the host, e.g. -p 8000")
	cmd.Flags().StringArrayVarP(&envFlags, "env", "e", []string{}, "Environment variables, in the form name=value")

	flags.SetInterspersed(false)

	return cmd
}

func run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		return err
	}
	client := registry.NewRegistryClient()

	cfg, projectDir, err := config.GetConfig(configFilename)
	if err != nil {
		return err
	}

	var imageName string
	if cfg.Build.Fast || buildFast || pipelinesImage {
		imageName = config.DockerImageName(projectDir)
		err = image.Build(
			ctx,
			cfg,
			projectDir,
			imageName,
			buildSecrets,
			buildNoCache,
			buildSeparateWeights,
			buildUseCudaBaseImage,
			buildProgressOutput,
			buildSchemaFile,
			buildDockerfileFile,
			DetermineUseCogBaseImage(cmd),
			buildStrip,
			buildPrecompile,
			cfg.Build.Fast || buildFast,
			nil,
			buildLocalImage,
			dockerClient,
			client,
			pipelinesImage)
		if err != nil {
			return err
		}
	} else {
		imageName, err = image.BuildBase(ctx, dockerClient, cfg, projectDir, buildUseCudaBaseImage, DetermineUseCogBaseImage(cmd), buildProgressOutput, client, true)
		if err != nil {
			return err
		}
	}

	gpus := ""
	if gpusFlag != "" {
		gpus = gpusFlag
	} else if cfg.Build.GPU {
		gpus = "all"
	}

	runOptions := command.RunOptions{
		Args:    args,
		Env:     envFlags,
		GPUs:    gpus,
		Image:   imageName,
		Volumes: []command.Volume{{Source: projectDir, Destination: "/src"}},
		Workdir: "/src",
	}
	runOptions, err = docker.FillInWeightsManifestVolumes(ctx, dockerClient, runOptions)
	if err != nil {
		return err
	}

	for _, portString := range runPorts {
		port, err := strconv.Atoi(portString)
		if err != nil {
			return err
		}

		runOptions.Ports = append(runOptions.Ports, command.Port{HostPort: port, ContainerPort: port})
	}

	console.Info("")
	console.Infof("Running '%s' in Docker with the current directory mounted as a volume...", strings.Join(args, " "))

	err = docker.Run(ctx, dockerClient, runOptions)
	// Only retry if we're using a GPU but but the user didn't explicitly select a GPU with --gpus
	// If the user specified the wrong GPU, they are explicitly selecting a GPU and they'll want to hear about it
	if runOptions.GPUs == "all" && err == docker.ErrMissingDeviceDriver {
		console.Info("Missing device driver, re-trying without GPU")

		runOptions.GPUs = ""
		err = docker.Run(ctx, dockerClient, runOptions)
	}

	return err
}
