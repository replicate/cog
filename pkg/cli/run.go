package cli

import (
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/model"
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
	addConfigFlag(cmd)

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

	src, err := model.NewSource(configFilename)
	if err != nil {
		return err
	}

	resolver := model.NewResolver(dockerClient, registry.NewRegistryClient())

	m, err := resolver.BuildBase(ctx, src, buildBaseOptionsFromFlags(cmd))
	if err != nil {
		return err
	}

	gpus := ""
	if gpusFlag != "" {
		gpus = gpusFlag
	} else if m.HasGPU() {
		gpus = "all"
	}

	// Automatically propagate RUST_LOG for Rust coglet debugging
	env := envFlags
	if rustLog := os.Getenv("RUST_LOG"); rustLog != "" {
		env = append(env, "RUST_LOG="+rustLog)
	}

	runOptions := command.RunOptions{
		Args:    args,
		Env:     env,
		GPUs:    gpus,
		Image:   m.ImageRef(),
		Volumes: []command.Volume{{Source: src.ProjectDir, Destination: "/src"}},
		Workdir: "/src",
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
	// Only retry if we're using a GPU but the user didn't explicitly select a GPU with --gpus
	// If the user specified the wrong GPU, they are explicitly selecting a GPU and they'll want to hear about it
	if runOptions.GPUs == "all" && err == docker.ErrMissingDeviceDriver {
		console.Info("Missing device driver, re-trying without GPU")

		runOptions.GPUs = ""
		err = docker.Run(ctx, dockerClient, runOptions)
	}

	return err
}
