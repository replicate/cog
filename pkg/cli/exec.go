package cli

import (
	"errors"
	"fmt"
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
	addDockerfileFlag(cmd)
	addUseCudaBaseImageFlag(cmd)
	addUseCogBaseImageFlag(cmd)
	addGpusFlag(cmd)
	addConfigFlag(cmd)

	flags := cmd.Flags()
	// Flags after first argument are considered args and passed to command

	// This is called `publish` for consistency with `docker run`
	cmd.Flags().StringArrayVarP(&execPorts, "publish", "p", []string{}, "Publish a container's port to the host, e.g. -p 8000 or -p 0.0.0.0:8000")
	cmd.Flags().StringArrayVarP(&envFlags, "env", "e", []string{}, "Environment variables, in the form name=value")

	flags.SetInterspersed(false)

	return cmd
}

// parsePublishFlags parses the values passed to `cog exec -p`. Each value may
// be either a port number ("8000") or a host:port pair ("0.0.0.0:8000" or
// "[::1]:8000"). When no host is given, the port is bound to
// command.DefaultHostIP.
func parsePublishFlags(values []string) ([]command.Port, error) {
	ports := make([]command.Port, 0, len(values))
	for _, portString := range values {
		hostIP, portStr, err := splitPublishFlag(portString)
		if err != nil {
			return nil, err
		}

		port, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("invalid port %q: %w", portString, err)
		}
		if port < 1 || port > 65535 {
			return nil, fmt.Errorf("invalid port %q: port must be between 1 and 65535", portString)
		}

		ports = append(ports, command.Port{HostPort: port, ContainerPort: port, HostIP: hostIP})
	}
	return ports, nil
}

// splitPublishFlag splits a publish flag value into host and port parts. It
// supports plain ports ("8000"), IPv4 host:port ("0.0.0.0:8000"), bare IPv6
// ("::1:8000"), and bracketed IPv6 ("[::1]:8000").
func splitPublishFlag(value string) (host, port string, err error) {
	host = command.DefaultHostIP
	port = value

	if value == "" {
		return "", "", fmt.Errorf("invalid port %q: value cannot be empty", value)
	}

	// Bracketed IPv6 form: [::1]:8000
	if strings.HasPrefix(value, "[") {
		end := strings.Index(value, "]")
		if end == -1 {
			return "", "", fmt.Errorf("invalid port %q: missing closing bracket for IPv6 address", value)
		}
		if end == len(value)-1 {
			return "", "", fmt.Errorf("invalid port %q: port is required after IPv6 address", value)
		}
		if value[end+1] != ':' {
			return "", "", fmt.Errorf("invalid port %q: expected ':' after ']'", value)
		}
		host = value[1:end]
		port = value[end+2:]
		if host == "" {
			return "", "", fmt.Errorf("invalid port %q: host cannot be empty", value)
		}
		return host, port, nil
	}

	// Standard host:port form, splitting on the last colon to tolerate IPv6.
	if idx := strings.LastIndex(value, ":"); idx != -1 {
		host = value[:idx]
		port = value[idx+1:]
		if host == "" {
			return "", "", fmt.Errorf("invalid port %q: host cannot be empty", value)
		}
	}

	return host, port, nil
}

func execCmd(cmd *cobra.Command, args []string) error {
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

	resolver := model.NewResolver(dockerClient, registry.NewRegistryClient())

	console.Info("Building Docker image from environment in cog.yaml...")
	console.Info("")
	opts := serveBuildOptions(cmd)
	opts.SkipSchemaValidation = true
	m, err := resolver.Build(ctx, src, opts)
	if err != nil {
		return err
	}

	gpus := ""
	if gpusFlag != "" {
		gpus = gpusFlag
	} else if m.HasGPU() {
		gpus = "all"
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

	ports, err := parsePublishFlags(execPorts)
	if err != nil {
		return err
	}
	runOptions.Ports = ports

	console.Info("")
	console.Infof("Running %s in Docker with the current directory mounted as a volume...", console.Bold(strings.Join(args, " ")))
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
