package cli

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/model"
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

	args := []string{
		"python",
		"--check-hash-based-pycs", "never",
		"-m", "cog.server.http",
		"--await-explicit-shutdown", "true",
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

	runOptions.Ports = append(runOptions.Ports, command.Port{HostPort: port, ContainerPort: 5000})

	console.Info("")
	console.Infof("Running '%[1]s' in Docker with the current directory mounted as a volume...", strings.Join(args, " "))
	console.Info("")
	console.Infof("Serving at http://127.0.0.1:%[1]v", port)
	console.Info("")

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
