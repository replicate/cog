package cli

import (
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
	port = 8393
)

func newServeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run a prediction HTTP server",
		Long: `Run a prediction HTTP server.

Generate and run an HTTP server based on the declared model inputs and outputs.`,
		RunE:       cmdServe,
		Args:       cobra.MaximumNArgs(0),
		SuggestFor: []string{"http"},
	}

	addBuildProgressOutputFlag(cmd)
	addUseCudaBaseImageFlag(cmd)
	addUseCogBaseImageFlag(cmd)
	addGpusFlag(cmd)
	addFastFlag(cmd)
	addConfigFlag(cmd)

	cmd.Flags().IntVarP(&port, "port", "p", port, "Port on which to listen")

	return cmd
}

func cmdServe(cmd *cobra.Command, arg []string) error {
	ctx := cmd.Context()

	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		return err
	}

	cfg, projectDir, err := config.GetConfig(configFilename)
	if err != nil {
		return err
	}

	client := registry.NewRegistryClient()
	imageName, err := image.BuildBase(ctx, dockerClient, cfg, projectDir, buildUseCudaBaseImage, DetermineUseCogBaseImage(cmd), buildProgressOutput, client, true)
	if err != nil {
		return err
	}

	if buildFast {
		console.Info("Fast serve enabled.")
	}

	gpus := ""
	if gpusFlag != "" {
		gpus = gpusFlag
	} else if cfg.Build.GPU {
		gpus = "all"
	}

	args := []string{
		"python",
		"--check-hash-based-pycs", "never",
		"-m", "cog.server.http",
		"--await-explicit-shutdown", "true",
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

	runOptions.Ports = append(runOptions.Ports, command.Port{HostPort: port, ContainerPort: 5000})

	console.Info("")
	console.Infof("Running '%[1]s' in Docker with the current directory mounted as a volume...", strings.Join(args, " "))
	console.Info("")
	console.Infof("Serving at http://127.0.0.1:%[1]v", port)
	console.Info("")

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
