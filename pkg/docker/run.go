package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/docker/go-connections/nat"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/weights"
)

var ErrMissingDeviceDriver = errors.New("Docker is missing required device driver")

func Run(ctx context.Context, dockerClient command.Command, options command.RunOptions) error {
	return RunWithIO(ctx, dockerClient, options, os.Stdin, os.Stdout, os.Stderr)
}

func RunWithIO(ctx context.Context, dockerClient command.Command, options command.RunOptions, stdin io.Reader, stdout, stderr io.Writer) error {
	options.Stdin = stdin
	options.Stdout = stdout
	options.Stderr = stderr
	// TODO[md]: we're gonna stop passing the entire host env to the container by default, if users indeed rely on that behavior we can uncomment this line:
	// options.Env = append(os.Environ(), options.Env...)
	return dockerClient.Run(ctx, options)
}

func RunDaemon(ctx context.Context, dockerClient command.Command, options command.RunOptions, stderr io.Writer) (string, error) {
	options.Stderr = stderr
	return dockerClient.ContainerStart(ctx, options)
}

func GetHostPortForContainer(ctx context.Context, dockerCommand command.Command, containerID string, containerPort int) (int, error) {
	console.Debugf("=== DockerCommand.GetPort %s/%d", containerID, containerPort)

	inspect, err := dockerCommand.ContainerInspect(ctx, containerID)
	if err != nil {
		return 0, fmt.Errorf("failed to inspect container %q: %w", containerID, err)
	}

	if inspect.ContainerJSONBase == nil || inspect.State == nil || !inspect.State.Running {
		return 0, fmt.Errorf("container %s is not running", containerID)
	}

	targetPort, err := nat.NewPort("tcp", strconv.Itoa(containerPort))
	if err != nil {
		return 0, fmt.Errorf("failed to create target port: %w", err)
	}

	if inspect.NetworkSettings == nil || inspect.NetworkSettings.Ports == nil {
		return 0, fmt.Errorf("container %s does not have expected network configuration", containerID)
	}

	for _, portBinding := range inspect.NetworkSettings.Ports[targetPort] {
		// TODO[md]: this should not be hardcoded since docker may be bound to a different address
		if portBinding.HostIP != "0.0.0.0" {
			continue
		}
		hostPort, err := nat.ParsePort(portBinding.HostPort)
		if err != nil {
			return 0, fmt.Errorf("failed to parse host port: %w", err)
		}
		return hostPort, nil
	}

	return 0, fmt.Errorf("container %s does not have a port bound to 0.0.0.0", containerID)
}

func FillInWeightsManifestVolumes(ctx context.Context, dockerCommand command.Command, runOptions command.RunOptions) (command.RunOptions, error) {
	// Check if the image has a weights manifest
	manifest, err := dockerCommand.Inspect(ctx, runOptions.Image)
	if err != nil {
		return runOptions, err
	}
	weightsManifest, ok := manifest.Config.Labels[command.CogWeightsManifestLabelKey]
	if ok {
		var weightsPaths []weights.WeightManifest
		err = json.Unmarshal([]byte(weightsManifest), &weightsPaths)
		if err != nil {
			return runOptions, err
		}
		for _, weightPath := range weightsPaths {
			runOptions.Volumes = append(runOptions.Volumes, command.Volume{
				Source:      weightPath.Source,
				Destination: "/src/" + weightPath.Destination,
			})
		}
	}

	return runOptions, nil
}
