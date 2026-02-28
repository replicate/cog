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
	port      = 8393
	uploadURL = ""
)

func newServeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run a prediction HTTP server",
		Long: `Run a prediction HTTP server.

Builds the model and starts an HTTP server that exposes the model's inputs
and outputs as a REST API. Compatible with the Cog HTTP protocol.`,
		Example: `  # Start the server on the default port (8393)
  cog serve

  # Start on a custom port
  cog serve -p 5000

  # Test the server
  curl http://localhost:8393/predictions \
    -X POST \
    -H 'Content-Type: application/json' \
    -d '{"input": {"prompt": "a cat"}}'`,
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
	cmd.Flags().StringVar(&uploadURL, "upload-url", "", "Upload URL for file outputs (e.g. https://example.com/upload/)")

	return cmd
}

// serveBuildOptions creates BuildOptions for cog serve.
// Same build path as cog build, but with ExcludeSource so COPY . /src is
// skipped — source is volume-mounted at runtime instead. All other layers
// (wheels, apt, etc.) share Docker layer cache with cog build.
func serveBuildOptions(cmd *cobra.Command) model.BuildOptions {
	return model.BuildOptions{
		UseCudaBaseImage: buildUseCudaBaseImage,
		UseCogBaseImage:  DetermineUseCogBaseImage(cmd),
		ProgressOutput:   buildProgressOutput,
		ExcludeSource:    true,
		SkipLabels:       true,
	}
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
	m, err := resolver.Build(ctx, src, serveBuildOptions(cmd))
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

	if uploadURL != "" {
		args = append(args, "--upload-url", uploadURL)
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

	// On Linux, host.docker.internal is not available by default — add it.
	// This allows the container to reach services running on the host,
	// e.g. when --upload-url points to a local upload server.
	if uploadURL != "" {
		runOptions.ExtraHosts = []string{"host.docker.internal:host-gateway"}
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
