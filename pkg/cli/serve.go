package cli

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/weights"
)

var (
	serveHost = command.DefaultHostIP
	port      = 8393
	uploadURL = ""
)

func newServeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run an HTTP server",
		Long: `Run an HTTP server.

Builds the model and starts an HTTP server that exposes the model's inputs
and outputs as a REST API. Compatible with the Cog HTTP protocol.

By default the container port is published on 127.0.0.1 (localhost), so the
server is only reachable from your local machine. The server process inside
the container binds to 0.0.0.0; use --host to control which host interface
the Docker port mapping is published on.`,
		Example: `  # Start the server on the default port (8393)
  cog serve

  # Start on a custom port
  cog serve -p 5000

  # Listen on all interfaces (e.g. to expose to the network)
  cog serve --host 0.0.0.0

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

	cmd.Flags().StringVar(&serveHost, "host", serveHost, "Host IP to publish the container port on. Use 0.0.0.0 to allow connections from other machines.")
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

// displayHostForServe returns the host string to show in the "Serving at" URL.
// Loopback bindings are displayed as "localhost" for clarity; any other
// address is returned as-is so the URL reflects the actual binding.
func displayHostForServe(host string) string {
	if host == command.DefaultHostIP || host == "::1" {
		return "localhost"
	}
	return host
}

// formatServeURL builds the "Serving at" URL for the given bind host and port.
// When bound to all interfaces (0.0.0.0), it also shows the usable localhost
// URL since 0.0.0.0 is not a navigable address.
func formatServeURL(host string, port int) string {
	url := fmt.Sprintf("http://%s", net.JoinHostPort(displayHostForServe(host), strconv.Itoa(port)))
	if host == "0.0.0.0" {
		localhostURL := fmt.Sprintf("http://%s", net.JoinHostPort("localhost", strconv.Itoa(port)))
		url = fmt.Sprintf("%s (%s)", url, localhostURL)
	}
	return url
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
	defer src.Close()

	if err := weights.CheckDrift(src.ProjectDir, src.Config.Weights); err != nil {
		return err
	}

	console.Info("Building Docker image from environment in cog.yaml...")
	console.Info("")
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

	// Use human-readable log format for local development
	env := make([]string, len(envFlags))
	copy(env, envFlags)
	env = append(env, "LOG_FORMAT=console")

	// Automatically propagate RUST_LOG for Rust coglet debugging
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

	wm, err := newWeightManager(src)
	if err != nil {
		return err
	}
	mounts, err := wm.Prepare(ctx)
	if err != nil {
		return fmt.Errorf("prepare weights: %w", err)
	}
	defer func() {
		if err := mounts.Release(); err != nil {
			console.Warnf("Failed to clean up weight mounts: %s", err)
		}
	}()
	for _, spec := range mounts.Specs {
		runOptions.Volumes = append(runOptions.Volumes, command.Volume{
			Source:      spec.Source,
			Destination: spec.Target,
			ReadOnly:    true,
		})
	}

	// On Linux, host.docker.internal is not available by default — add it.
	// This allows the container to reach services running on the host,
	// e.g. when --upload-url points to a local upload server.
	if uploadURL != "" {
		runOptions.ExtraHosts = []string{"host.docker.internal:host-gateway"}
	}

	runOptions.Ports = append(runOptions.Ports, command.Port{HostPort: port, ContainerPort: 5000, HostIP: serveHost})

	serveURL := formatServeURL(serveHost, port)

	if isRemote, dockerHost, err := docker.IsRemoteDockerHost(); err == nil && isRemote {
		console.Warnf("Using Docker daemon at %s; the server will bind to %s on that host, not this machine.", dockerHost, serveHost)
	}

	console.Info("")
	console.Infof("Running %[1]s in Docker with the current directory mounted as a volume...", console.Bold(strings.Join(args, " ")))
	console.Info("")
	console.Infof("Serving at %s", console.Bold(serveURL))
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
