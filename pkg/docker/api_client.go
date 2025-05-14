package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/client"
	dc "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/google/go-containerregistry/pkg/name"

	"github.com/replicate/go/types/ptr"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/util/console"
)

func NewAPIClient(ctx context.Context, opts ...Option) (*apiClient, error) {
	clientOptions := &clientOptions{
		authConfigs: make(map[string]registry.AuthConfig),
	}
	for _, opt := range opts {
		opt(clientOptions)
	}

	// TODO[md]: we create a client at the top of each cli invocation, the sdk client hits an api which
	// adds (a tiny biy of) overead. swap this with a handle that'll lazily initialize a client and ping for health.
	// ditto for fetching registry credentials.
	client, err := dc.NewClientWithOpts(dc.FromEnv, dc.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("error creating docker client: %w", err)
	}

	if _, err := client.Ping(ctx); err != nil {
		return nil, fmt.Errorf("error pinging docker daemon: %w", err)
	}

	authConfig := make(map[string]registry.AuthConfig)
	userInfo, err := loadUserInformation(ctx, "r8.im")
	if err != nil {
		return nil, fmt.Errorf("error loading user information: %w", err)
	}
	authConfig["r8.im"] = registry.AuthConfig{
		Username:      userInfo.Username,
		Password:      userInfo.Token,
		ServerAddress: "r8.im",
	}

	for _, opt := range clientOptions.authConfigs {
		authConfig[opt.ServerAddress] = opt
	}

	return &apiClient{client, authConfig}, nil
}

type apiClient struct {
	client     *dc.Client
	authConfig map[string]registry.AuthConfig
}

func (c *apiClient) Pull(ctx context.Context, imageRef string, force bool) (*image.InspectResponse, error) {
	console.Debugf("=== APIClient.Pull %s force:%t", imageRef, force)

	if !force {
		inspect, err := c.Inspect(ctx, imageRef)
		if err == nil {
			return inspect, nil
		} else if !command.IsNotFoundError(err) {
			// Log a warning if inspect fails for any reason other than not found.
			// It's likely that pull will fail as well, but it's better to return that error
			// so the caller can handle it appropriately than to fail silently here.
			console.Warnf("failed to inspect image before pulling %q: %s", imageRef, err)
		}
	}

	output, err := c.client.ImagePull(ctx, imageRef, image.PullOptions{
		// force image to linux/amd64 to match production
		Platform: "linux/amd64",
	})
	if err != nil {
		if client.IsErrNotFound(err) {
			return nil, &command.NotFoundError{Ref: imageRef, Object: "image"}
		}
		return nil, fmt.Errorf("failed to pull image %q: %w", imageRef, err)
	}
	defer output.Close()
	_, err = io.Copy(os.Stderr, output)
	if err != nil {
		return nil, fmt.Errorf("failed to copy pull output: %w", err)
	}

	// pull succeeded, inspect the image again and return
	inspect, err := c.Inspect(ctx, imageRef)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect image after pulling %q: %w", imageRef, err)
	}
	return inspect, nil
}

func (c *apiClient) ContainerStop(ctx context.Context, containerID string) error {
	console.Debugf("=== APIClient.ContainerStop %s", containerID)

	err := c.client.ContainerStop(ctx, containerID, container.StopOptions{
		Timeout: ptr.To(3),
	})
	if err != nil {
		if client.IsErrNotFound(err) {
			return &command.NotFoundError{Ref: containerID, Object: "container"}
		}
		return fmt.Errorf("failed to stop container %q: %w", containerID, err)
	}
	return nil
}

func (c *apiClient) ContainerInspect(ctx context.Context, containerID string) (*container.InspectResponse, error) {
	console.Debugf("=== APIClient.ContainerInspect %s", containerID)

	resp, err := c.client.ContainerInspect(ctx, containerID)
	if err != nil {
		if client.IsErrNotFound(err) {
			return nil, &command.NotFoundError{Ref: containerID, Object: "container"}
		}
		return nil, fmt.Errorf("failed to inspect container %q: %w", containerID, err)
	}
	return &resp, nil
}

func (c *apiClient) ContainerLogs(ctx context.Context, containerID string, w io.Writer) error {
	console.Debugf("=== APIClient.ContainerLogs %s", containerID)

	// First inspect the container to check if it has TTY enabled
	inspect, err := c.ContainerInspect(ctx, containerID)
	if err != nil {
		return fmt.Errorf("failed to inspect container %q: %w", containerID, err)
	}

	logs, err := c.client.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		if client.IsErrNotFound(err) {
			return &command.NotFoundError{Ref: containerID, Object: "container"}
		}
		return fmt.Errorf("failed to get container logs for %q: %w", containerID, err)
	}
	defer logs.Close()

	// If TTY is enabled, we can just copy the logs directly
	if inspect.Config.Tty {
		if _, err = io.Copy(w, logs); err != nil {
			return fmt.Errorf("failed to copy logs: %w", err)
		}
		return nil
	}

	// For non-TTY containers, use StdCopy to demultiplex stdout and stderr
	if _, err = stdcopy.StdCopy(w, w, logs); err != nil {
		return fmt.Errorf("failed to copy logs: %w", err)
	}
	return nil
}

func (c *apiClient) Push(ctx context.Context, imageRef string) error {
	console.Debugf("=== APIClient.Push %s", imageRef)

	parsedName, err := name.ParseReference(imageRef)
	if err != nil {
		return fmt.Errorf("failed to parse image reference: %w", err)
	}

	console.Debugf("fully qualified image ref: %s", parsedName)

	// eagerly set auth config, or do it async
	var authConfig registry.AuthConfig
	if auth, ok := c.authConfig[parsedName.Context().RegistryStr()]; ok {
		authConfig = auth
	} else {
		console.Warnf("no auth config found for registry %s", parsedName.Context().RegistryStr())
	}

	var opts image.PushOptions
	encodedAuth, err := registry.EncodeAuthConfig(authConfig)
	if err != nil {
		return fmt.Errorf("failed to encode auth config: %w", err)
	}
	opts.RegistryAuth = encodedAuth

	output, err := c.client.ImagePush(ctx, imageRef, opts)
	if err != nil {
		return fmt.Errorf("failed to push image: %w", err)
	}
	defer output.Close()

	// output is a json stream, so we need to parse it, handle errors, and write progress to stderr
	isTTY := console.IsTTY(os.Stderr)
	if err := jsonmessage.DisplayJSONMessagesStream(output, os.Stderr, os.Stderr.Fd(), isTTY, nil); err != nil {
		var streamErr *jsonmessage.JSONError
		if errors.As(err, &streamErr) {
			if strings.Contains(streamErr.Message, "tag does not exist") {
				return &command.NotFoundError{Ref: imageRef, Object: "tag"}
			}
			if strings.Contains(streamErr.Message, "authorization failed") {
				return command.ErrAuthorizationFailed
			}
			if strings.Contains(streamErr.Message, "401 Unauthorized") {
				return command.ErrAuthorizationFailed
			}
		}
		return fmt.Errorf("error during image push: %w", err)
	}

	return nil
}

func (c *apiClient) LoadUserInformation(ctx context.Context, registryHost string) (*command.UserInfo, error) {
	console.Debugf("=== APIClient.LoadUserInformation %s", registryHost)
	panic("not implemented")
}

func (c *apiClient) CreateTarFile(ctx context.Context, ref string, tmpDir string, tarFile string, folder string) (string, error) {
	panic("not implemented")
}

func (c *apiClient) CreateAptTarFile(ctx context.Context, tmpDir string, aptTarFile string, packages ...string) (string, error) {
	panic("not implemented")
}

func (c *apiClient) Inspect(ctx context.Context, ref string) (*image.InspectResponse, error) {
	console.Debugf("=== APIClient.Inspect %s", ref)

	// TODO[md]: platform requires engine 1.49+, and it's not widly available as of 2025-05.
	// platform := ocispec.Platform{OS: "linux", Architecture: "amd64"}
	//  client.ImageInspectWithPlatform(&platform),
	inspect, err := c.client.ImageInspect(ctx, ref)

	if err != nil {
		if client.IsErrNotFound(err) {
			return nil, &command.NotFoundError{Ref: ref, Object: "image"}
		}
		return nil, fmt.Errorf("error inspecting image: %w", err)
	}

	return &inspect, nil
}

func (c *apiClient) ImageExists(ctx context.Context, ref string) (bool, error) {
	console.Debugf("=== APIClient.ImageExists %s", ref)

	_, err := c.Inspect(ctx, ref)
	if err != nil {
		if command.IsNotFoundError(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (c *apiClient) ImageBuild(ctx context.Context, options command.ImageBuildOptions) error {
	panic("not implemented")
}

func (c *apiClient) Run(ctx context.Context, options command.RunOptions) error {
	panic("not implemented")
}

func (c *apiClient) ContainerStart(ctx context.Context, options command.RunOptions) (string, error) {
	panic("not implemented")
}

func (c *apiClient) ContainerRemove(ctx context.Context, containerID string) error {
	panic("not implemented")
}
