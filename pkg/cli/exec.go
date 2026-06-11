package cli

import (
	"context"
	"errors"
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
	execPorts []string
	gpusFlag  string
)

func addGpusFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&gpusFlag, "gpus", "", "GPU devices to add to the container, in the same format as `docker run --gpus`.")
}

func newExecCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exec <command> [arg...]",
		Short: "Execute a command inside a Docker environment",
		Long: `Execute a command inside a Docker environment defined by cog.yaml.

Cog builds a temporary image from your cog.yaml configuration and runs the
given command inside it. This is useful for debugging, running scripts, or
exploring the environment your model will run in.`,
		Example: `  # Open a Python interpreter inside the model environment
  cog exec python

  # Run a script
  cog exec python train.py

  # Run with environment variables
  cog exec -e HUGGING_FACE_HUB_TOKEN=abc123 python download.py

  # Expose a port (e.g. for Jupyter)
  cog exec -p 8888 jupyter notebook`,
		RunE:    execCmd,
		PreRunE: checkMutuallyExclusiveFlags,
		Args:    cobra.MinimumNArgs(1),
	}
	addBuildProgressOutputFlag(cmd)
	addUseCudaBaseImageFlag(cmd)
	addUseCogBaseImageFlag(cmd)
	addGpusFlag(cmd)
	addConfigFlag(cmd)

	flags := cmd.Flags()
	// Flags after first argument are considered args and passed to command

	// This is called `publish` for consistency with `docker run`
	cmd.Flags().StringArrayVarP(&execPorts, "publish", "p", []string{}, "Publish a container's port to the host, e.g. -p 8000")
	cmd.Flags().StringArrayVarP(&envFlags, "env", "e", []string{}, "Environment variables, in the form name=value")

	flags.SetInterspersed(false)

	return cmd
}

func execCmd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		return err
	}

	return RunExec(ctx, dockerClient, registry.NewRegistryClient(), ExecCommandOptions{
		RuntimeBuildOptions: RuntimeBuildOptions{
			ConfigFilename:   configFilename,
			ProgressOutput:   buildProgressOutput,
			UseCudaBaseImage: buildUseCudaBaseImage,
			UseCogBaseImage:  DetermineUseCogBaseImage(cmd),
			GPUs:             gpusFlag,
			Env:              envFlags,
		},
		Args:  args,
		Ports: execPorts,
	})
}

// ExecCommandOptions holds everything RunExec needs that is independent of the
// argument parser.
type ExecCommandOptions struct {
	RuntimeBuildOptions
	Args  []string
	Ports []string
}

// RunExec builds a local image and executes an arbitrary command inside it with
// the project directory volume-mounted. It is shared by both the Cobra and Kong
// exec commands.
func RunExec(ctx context.Context, dockerClient command.Command, regClient registry.Client, opts ExecCommandOptions) error {
	src, err := model.NewSource(opts.ConfigFilename)
	if err != nil {
		return err
	}
	defer src.Close()

	resolver := model.NewResolver(dockerClient, regClient)

	console.Info("Building Docker image from environment in cog.yaml...")
	console.Info("")
	buildOpts := opts.ServeBuildOptions()
	buildOpts.SkipSchemaValidation = true
	m, err := resolver.Build(ctx, src, buildOpts)
	if err != nil {
		return err
	}

	runOptions := command.RunOptions{
		Args:    opts.Args,
		Env:     runtimeEnv(opts.Env),
		GPUs:    runtimeGPUs(opts.GPUs, m),
		Image:   m.ImageRef(),
		Volumes: []command.Volume{{Source: src.ProjectDir, Destination: "/src"}},
		Workdir: "/src",
	}

	for _, portString := range opts.Ports {
		port, err := strconv.Atoi(portString)
		if err != nil {
			return err
		}

		runOptions.Ports = append(runOptions.Ports, command.Port{HostPort: port, ContainerPort: port})
	}

	console.Info("")
	console.Infof("Running %s in Docker with the current directory mounted as a volume...", console.Bold(strings.Join(opts.Args, " ")))
	console.Info("")

	err = docker.Run(ctx, dockerClient, runOptions)
	// Only retry if we're using a GPU but the user didn't explicitly select a GPU with --gpus
	// If the user specified the wrong GPU, they are explicitly selecting a GPU and they'll want to hear about it
	if runOptions.GPUs == "all" && errors.Is(err, docker.ErrMissingDeviceDriver) {
		console.Info("Missing device driver, re-trying without GPU")

		runOptions.GPUs = ""
		err = docker.Run(ctx, dockerClient, runOptions)
	}

	return err
}
