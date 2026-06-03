package cli

import (
	"context"
	"errors"
	"fmt"
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
		Short: "Run an HTTP server",
		Long: `Run an HTTP server.

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

// RuntimeBuildOptions holds the parser-independent options shared by the
// runtime commands (serve, exec) that build a local image and run it with the
// project directory volume-mounted.
type RuntimeBuildOptions struct {
	ConfigFilename   string
	ProgressOutput   string
	UseCudaBaseImage string
	UseCogBaseImage  *bool
	GPUs             string
	Env              []string
}

// ServeBuildOptions creates BuildOptions for cog serve and cog exec.
// Same build path as cog build, but with ExcludeSource so COPY . /src is
// skipped — source is volume-mounted at runtime instead. All other layers
// (wheels, apt, etc.) share Docker layer cache with cog build.
func (o RuntimeBuildOptions) ServeBuildOptions() model.BuildOptions {
	return model.BuildOptions{
		UseCudaBaseImage: o.UseCudaBaseImage,
		UseCogBaseImage:  o.UseCogBaseImage,
		ProgressOutput:   o.ProgressOutput,
		ExcludeSource:    true,
		SkipLabels:       true,
	}
}

// runtimeGPUs resolves the GPU spec: an explicit request wins, otherwise
// "all" if the model declares GPU usage, otherwise empty.
func runtimeGPUs(requested string, m *model.Model) string {
	if requested != "" {
		return requested
	}
	if m.HasGPU() {
		return "all"
	}
	return ""
}

// runtimeEnv appends RUST_LOG from the host environment (for Rust coglet
// debugging) to the provided env list without mutating the input slice.
func runtimeEnv(env []string) []string {
	out := append([]string{}, env...)
	if rustLog := os.Getenv("RUST_LOG"); rustLog != "" {
		out = append(out, "RUST_LOG="+rustLog)
	}
	return out
}

// ServeCommandOptions holds everything RunServe needs that is independent of
// the argument parser.
type ServeCommandOptions struct {
	RuntimeBuildOptions
	Port      int
	UploadURL string
}

func cmdServe(cmd *cobra.Command, arg []string) error {
	ctx := cmd.Context()

	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		return err
	}

	return RunServe(ctx, dockerClient, registry.NewRegistryClient(), ServeCommandOptions{
		RuntimeBuildOptions: RuntimeBuildOptions{
			ConfigFilename:   configFilename,
			ProgressOutput:   buildProgressOutput,
			UseCudaBaseImage: buildUseCudaBaseImage,
			UseCogBaseImage:  DetermineUseCogBaseImage(cmd),
			GPUs:             gpusFlag,
			Env:              envFlags,
		},
		Port:      port,
		UploadURL: uploadURL,
	})
}

// RunServe builds the model image and runs the Cog HTTP server. It is shared by
// both the Cobra and Kong serve commands.
func RunServe(ctx context.Context, dockerClient command.Command, regClient registry.Client, opts ServeCommandOptions) error {
	src, err := model.NewSource(opts.ConfigFilename)
	if err != nil {
		return err
	}
	defer src.Close()

	console.Info("Building Docker image from environment in cog.yaml...")
	console.Info("")
	resolver := model.NewResolver(dockerClient, regClient)
	m, err := resolver.Build(ctx, src, opts.ServeBuildOptions())
	if err != nil {
		return err
	}

	args := []string{
		"python",
		"--check-hash-based-pycs", "never",
		"-m", "cog.server.http",
		"--await-explicit-shutdown", "true",
	}

	if opts.UploadURL != "" {
		args = append(args, "--upload-url", opts.UploadURL)
	}

	runOptions := command.RunOptions{
		Args:    args,
		Env:     runtimeEnv(opts.Env),
		GPUs:    runtimeGPUs(opts.GPUs, m),
		Image:   m.ImageRef(),
		Volumes: []command.Volume{{Source: src.ProjectDir, Destination: "/src"}},
		Workdir: "/src",
		Ports:   []command.Port{{HostPort: opts.Port, ContainerPort: 5000}},
	}

	// On Linux, host.docker.internal is not available by default — add it.
	// This allows the container to reach services running on the host,
	// e.g. when --upload-url points to a local upload server.
	if opts.UploadURL != "" {
		runOptions.ExtraHosts = []string{"host.docker.internal:host-gateway"}
	}

	console.Info("")
	console.Infof("Running %[1]s in Docker with the current directory mounted as a volume...", console.Bold(strings.Join(args, " ")))
	console.Info("")
	console.Infof("Serving at %s", console.Bold(fmt.Sprintf("http://127.0.0.1:%v", opts.Port)))
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
