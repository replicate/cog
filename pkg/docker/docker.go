package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/client"
	dc "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/mattn/go-isatty"
	buildkitclient "github.com/moby/buildkit/client"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	"github.com/moby/term"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/sync/errgroup"

	"github.com/replicate/go/types/ptr"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/util/console"
)

func NewClient(ctx context.Context, opts ...Option) (*apiClient, error) {
	clientOptions := &clientOptions{
		authConfigs: make(map[string]registry.AuthConfig),
	}
	for _, opt := range opts {
		opt(clientOptions)
	}

	if clientOptions.host == "" {
		host, err := determineDockerHost()
		if err != nil {
			return nil, fmt.Errorf("error determining docker host: %w", err)
		}
		clientOptions.host = host
	}

	// TODO[md]: we create a client at the top of each cli invocation, the sdk client hits an api which
	// adds (a tiny biy of) overead. swap this with a handle that'll lazily initialize a client and ping for health.
	// ditto for fetching registry credentials.

	dockerClientOpts := []dc.Opt{
		dc.WithTLSClientConfigFromEnv(),
		dc.WithVersionFromEnv(),
		dc.WithAPIVersionNegotiation(),
		dc.WithHost(clientOptions.host),
	}

	client, err := dc.NewClientWithOpts(dockerClientOpts...)
	if err != nil {
		return nil, fmt.Errorf("error creating docker client: %w", err)
	}

	if _, err := client.Ping(ctx); err != nil {
		return nil, fmt.Errorf("error pinging docker daemon: %w", err)
	}

	// Load authentication for configured registry and any other registries that might be needed
	authConfig, err := loadRegistryAuths(ctx, global.ReplicateRegistryHost)
	if err != nil {
		return nil, fmt.Errorf("error loading user information: %w, you may need to authenticate using cog login", err)
	}

	// Add any additional auth configs passed via options
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
	registryHost := parsedName.Context().RegistryStr()
	if auth, ok := c.authConfig[registryHost]; ok {
		authConfig = auth
	} else {
		// Dynamically load authentication for this registry if not already loaded
		authConfigs, err := loadRegistryAuths(ctx, registryHost)
		if err == nil {
			if auth, ok := authConfigs[registryHost]; ok {
				authConfig = auth
				// Cache the auth config for future use
				c.authConfig[registryHost] = auth
			}
		}
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
			if isTagNotFoundError(err) {
				return &command.NotFoundError{Ref: imageRef, Object: "tag"}
			}
			if isAuthorizationFailedError(err) {
				return command.ErrAuthorizationFailed
			}
		}
		return fmt.Errorf("error during image push: %w", err)
	}

	return nil
}

// TODO[md]: this doesn't need to be on the interface, move to auth handler
func (c *apiClient) LoadUserInformation(ctx context.Context, registryHost string) (*command.UserInfo, error) {
	console.Debugf("=== APIClient.LoadUserInformation %s", registryHost)

	return loadUserInformation(ctx, registryHost)
}

func (c *apiClient) Inspect(ctx context.Context, ref string) (*image.InspectResponse, error) {
	console.Debugf("=== APIClient.Inspect %s", ref)

	// TODO[md]: platform requires engine 1.49+, and it's not widly available as of 2025-05.
	// 	platform := ocispec.Platform{OS: "linux", Architecture: "amd64"}
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
	console.Debugf("=== APIClient.ImageBuild %s", options.ImageName)

	buildDir, err := os.MkdirTemp("", "cog-build")
	if err != nil {
		return err
	}
	defer os.RemoveAll(buildDir)

	bc, err := buildkitclient.New(ctx, "",
		// Connect to Docker Engine's embedded Buildkit.
		buildkitclient.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return c.client.DialHijack(ctx, "/grpc", "h2c", map[string][]string{})
		}),
	)
	if err != nil {
		return err
	}

	statusCh := make(chan *buildkitclient.SolveStatus)
	var res *buildkitclient.SolveResponse

	// Determine display mode: options.ProgressOutput > env > 'auto'
	displayMode := options.ProgressOutput
	if displayMode == "" {
		displayMode = os.Getenv("BUILDKIT_PROGRESS")
	}
	if displayMode == "" {
		displayMode = "auto"
	}

	// Build the image.
	eg, ctx := errgroup.WithContext(ctx)

	// run the build in a goroutine
	eg.Go(func() error {
		options, err := solveOptFromImageOptions(buildDir, options)
		if err != nil {
			return err
		}

		// run the display in a goroutine _after_ we've built SolveOpt
		eg.Go(newDisplay(statusCh, displayMode))

		res, err = bc.Solve(ctx, nil, options, statusCh)
		if err != nil {
			return err
		}
		return nil
	})
	err = eg.Wait()

	if err != nil {
		return err
	}

	console.Debugf("image digest %s", res.ExporterResponse[exptypes.ExporterImageDigestKey])

	// TODO[md]: return the image id on success
	return nil
}

func (c *apiClient) containerRun(ctx context.Context, options command.RunOptions) (string, error) {
	console.Debugf("=== APIClient.containerRun %s", options.Image)

	var attachStdin, tty, attachStderr, attachStdout bool
	if !options.Detach {
		// Determine if we should attach stdin (file, pipe, interactive stdin, etc)
		attachStdin, tty = shouldAttachStdin(options.Stdin)
		attachStdout = options.Stdout != nil
		attachStderr = options.Stderr != nil
	}

	containerCfg := &container.Config{
		Image:        options.Image,
		Cmd:          options.Args,
		Env:          options.Env,
		AttachStdin:  attachStdin,
		AttachStdout: attachStdout,
		AttachStderr: attachStderr,
		OpenStdin:    attachStdin,
		StdinOnce:    attachStdin,
		Tty:          tty,
	}

	// Set working directory if specified
	if options.Workdir != "" {
		containerCfg.WorkingDir = options.Workdir
	}

	if len(options.Ports) > 0 {
		containerCfg.ExposedPorts = make(nat.PortSet)
		for _, port := range options.Ports {
			containerPort := nat.Port(fmt.Sprintf("%d/tcp", port.ContainerPort))
			containerCfg.ExposedPorts[containerPort] = struct{}{}
		}
	}

	hostCfg := &container.HostConfig{
		// always remove container after it exits
		AutoRemove: true,
		// https://github.com/pytorch/pytorch/issues/2244
		// https://github.com/replicate/cog/issues/1293
		ShmSize:   6 * 1024 * 1024 * 1024, // 6GB
		Resources: container.Resources{},
	}

	if options.GPUs != "" {
		deviceRequest, err := parseGPURequest(options)
		if err != nil {
			return "", err
		}
		hostCfg.Resources.DeviceRequests = []container.DeviceRequest{deviceRequest}
	}

	// Configure port bindings
	if len(options.Ports) > 0 {
		hostCfg.PortBindings = make(nat.PortMap)
		for _, port := range options.Ports {
			containerPort := nat.Port(fmt.Sprintf("%d/tcp", port.ContainerPort))
			hostCfg.PortBindings[containerPort] = []nat.PortBinding{
				{
					HostIP:   "", // use empty string to bind to all interfaces
					HostPort: strconv.Itoa(port.HostPort),
				},
			}
		}
	}

	// Configure volume bindings
	if len(options.Volumes) > 0 {
		hostCfg.Binds = make([]string, len(options.Volumes))
		for i, volume := range options.Volumes {
			hostCfg.Binds[i] = fmt.Sprintf("%s:%s", volume.Source, volume.Destination)
		}
	}

	networkingCfg := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{},
	}

	platform := &ocispec.Platform{
		// force platform to linux/amd64
		Architecture: "amd64",
		OS:           "linux",
	}

	runContainer, err := c.client.ContainerCreate(ctx,
		containerCfg,
		hostCfg,
		networkingCfg,
		platform,
		"")
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}
	// TODO[md]: ensure the container is removed if start & auto-remove fails

	console.Debugf("container id: %s", runContainer.ID)

	// Create error group for stream copying
	var eg *errgroup.Group
	var stream types.HijackedResponse

	// Attach to container streams if we have any writers and not detached
	if attachStderr || attachStdout || attachStdin {
		attachOpts := container.AttachOptions{
			Stream: true,
			Stdin:  attachStdin,
			Stdout: attachStdout,
			Stderr: attachStderr,
		}

		var err error
		stream, err = c.client.ContainerAttach(ctx, runContainer.ID, attachOpts)
		if err != nil {
			return "", fmt.Errorf("failed to attach to container: %w", err)
		}
		defer stream.Close()

		// Start copying streams in the background
		eg, _ = errgroup.WithContext(ctx)
		if attachStdout || attachStderr {
			eg.Go(func() (err error) {
				if containerCfg.Tty {
					w := options.Stdout
					if w == nil {
						w = options.Stderr
					}
					_, err = io.Copy(w, stream.Reader)
				} else {
					_, err = stdcopy.StdCopy(options.Stdout, options.Stderr, stream.Reader)
				}
				return err
			})
		}
		if attachStdin {
			// if we're in a TTY we need to set the terminal to raw mode, and restore it when we're done
			if tty {
				// TODO[md]: handle terminal resize events, see: github.com/containerd/console
				state, err := term.SetRawTerminal(os.Stdin.Fd())
				if err != nil {
					console.Warnf("error setting raw terminal on stdin: %s", err)
				}
				defer func() {
					if err := term.RestoreTerminal(os.Stdin.Fd(), state); err != nil {
						console.Warnf("error restoring terminal on stdin: %s", err)
					}
				}()
			}

			go func() {
				_, err := io.Copy(stream.Conn, options.Stdin)
				// Close the stdin stream to signal EOF to the container
				if err := errors.Join(err, stream.CloseWrite()); err != nil {
					console.Errorf("error copying and closing stdin stream: %s", err)
				}
			}()
		}
	}

	// Start the container
	if err := c.client.ContainerStart(ctx, runContainer.ID, container.StartOptions{}); err != nil {
		if isMissingDeviceDriverError(err) {
			return "", ErrMissingDeviceDriver
		}
		return "", fmt.Errorf("failed to start container: %w", err)
	}

	// If detached, wait for container to be running before returning
	if options.Detach {
		return runContainer.ID, nil
	}

	// Wait for the container to exit
	statusCh, errCh := c.client.ContainerWait(ctx, runContainer.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		return "", fmt.Errorf("error waiting for container: %w", err)
	case status := <-statusCh:
		if status.StatusCode != 0 {
			return "", fmt.Errorf("container exited with status %d", status.StatusCode)
		}
	}

	// container is gone, close the attached streams so stdin is released, ignore the error
	_ = stream.CloseWrite()

	// Wait for stream copying to complete
	if eg != nil {
		if err := eg.Wait(); err != nil {
			return "", fmt.Errorf("error copying streams: %w", err)
		}
	}

	return runContainer.ID, nil
}

func (c *apiClient) Run(ctx context.Context, options command.RunOptions) error {
	console.Debugf("=== APIClient.Run %s", options.Image)

	if options.Stdout == nil {
		options.Stdout = os.Stdout
	}
	if options.Stderr == nil {
		options.Stderr = os.Stderr
	}

	_, err := c.containerRun(ctx, options)
	return err
}

func (c *apiClient) ContainerStart(ctx context.Context, options command.RunOptions) (string, error) {
	console.Debugf("=== APIClient.ContainerStart %s", options.Image)

	options.Detach = true
	id, err := c.containerRun(ctx, options)
	return id, err
}

// parseGPURequest converts a Docker CLI --gpus string into a DeviceRequest slice
func parseGPURequest(opts command.RunOptions) (container.DeviceRequest, error) {
	if opts.GPUs == "" {
		return container.DeviceRequest{}, nil
	}

	deviceRequest := container.DeviceRequest{
		Driver:       "nvidia",
		Capabilities: [][]string{{"gpu"}},
	}

	// Parse the GPUs string
	switch opts.GPUs {
	case "all":
		deviceRequest.Count = -1 // Use all available GPUs
	default:
		// Check if it's a number
		if count, err := strconv.Atoi(opts.GPUs); err == nil {
			deviceRequest.Count = count
		} else if strings.HasPrefix(opts.GPUs, "device=") {
			// Handle device=0,1 format
			devices := strings.TrimPrefix(opts.GPUs, "device=")
			deviceRequest.DeviceIDs = strings.Split(devices, ",")
		} else {
			// Invalid GPU specification, return nil to indicate no GPU access
			return container.DeviceRequest{}, fmt.Errorf("invalid GPU specification: %q", opts.GPUs)
		}
	}

	return deviceRequest, nil
}

// shouldAttachStdin determines if we should attach stdin to the container
// We should attach stdin only if:
//   - stdin is not os.Stdin (explicit input like pipe/file/buffer)
//   - OR stdin is os.Stdin but it's not a TTY (piped input)
func shouldAttachStdin(stdin io.Reader) (attach bool, tty bool) {
	if stdin == nil {
		return false, false
	}

	// If it's not a file, it's probably a buffer/pipe with actual data
	f, ok := stdin.(*os.File)
	if !ok {
		return true, false
	}

	tty = isatty.IsTerminal(f.Fd())

	// If it's not os.Stdin, it's an explicit file, so attach it
	if f != os.Stdin {
		return true, tty
	}

	// If it's os.Stdin but not a TTY, it's probably piped input
	if !tty {
		return true, false
	}

	// If it's os.Stdin and a TTY, attach by default. if this becomes a problem for some
	// reason we need to add a flag to the run command similar to `docker run -i` that instructs
	// the container to attach stdin and keep open
	return true, true
}
