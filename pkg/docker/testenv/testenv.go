package testenv

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/dind"
	tcnetwork "github.com/testcontainers/testcontainers-go/network"
)

// DockerTestEnv provides an isolated Docker environment for testing
// with DIND, a local registry, and helper methods.
type DockerTestEnv struct {
	t testing.TB

	daemonHelper   *TestDaemon
	registryHelper *TestRegistry

	dindContainer testcontainers.Container
	dindClient    client.APIClient
	dockerHost    string
	envID         string

	registryContainerID  string
	internalRegistryHost string
	externalRegistryHost string
}

// config holds configuration options for DockerTestEnv
type config struct {
	registryData fs.FS
}

// Option configures a DockerTestEnv
type Option func(*config)

func WithRegistryData(fs fs.FS) Option {
	return func(c *config) {
		c.registryData = fs
	}
}

// New creates a new isolated Docker test environment
func New(t testing.TB, opts ...Option) *DockerTestEnv {
	t.Helper()

	// Get the directory of this source file so we can load registry fixtures in this directory not the calling test dir
	_, filename, _, _ := runtime.Caller(0)
	sourceDir := filepath.Dir(filename)

	cfg := &config{
		registryData: os.DirFS(filepath.Join(sourceDir, "testdata", "local_registry")),
	}

	for _, opt := range opts {
		opt(cfg)
	}

	env := &DockerTestEnv{
		t:     t,
		envID: uuid.New().String()[:8],
	}

	env.daemonHelper = &TestDaemon{
		env: env,
	}
	env.registryHelper = &TestRegistry{
		env: env,
	}

	env.startDind(t)
	env.startRegistry(t, cfg)

	return env
}

// ScopeT returns a new DockerTestEnv scoped to the provided test
func (env *DockerTestEnv) ScopeT(t testing.TB) *DockerTestEnv {
	t.Helper()

	return &DockerTestEnv{
		t:                    t,
		envID:                env.envID,
		daemonHelper:         env.daemonHelper,
		registryHelper:       env.registryHelper,
		internalRegistryHost: env.internalRegistryHost,
		externalRegistryHost: env.externalRegistryHost,
		dindContainer:        env.dindContainer,
		dindClient:           env.dindClient,
		dockerHost:           env.dockerHost,
		registryContainerID:  env.registryContainerID,
	}
}

func (env *DockerTestEnv) Daemon() *TestDaemon {
	return env.daemonHelper
}

func (env *DockerTestEnv) Registry() *TestRegistry {
	return env.registryHelper
}

// DockerClient returns the DIND Docker client
func (env *DockerTestEnv) DockerClient() client.APIClient {
	return env.dindClient
}

func (env *DockerTestEnv) DockerHost() string {
	return env.dockerHost
}

// startDind starts the Docker-in-Docker container
func (env *DockerTestEnv) startDind(t testing.TB) {
	ctx := t.Context()

	testNetwork, err := tcnetwork.New(ctx)
	testcontainers.CleanupNetwork(t, testNetwork)
	require.NoError(t, err)

	netAlias := fmt.Sprintf("dind-test-network-%s", env.envID)

	// Configure DIND with proper privileges
	dindOpts := []testcontainers.ContainerCustomizer{
		testcontainers.WithImagePlatform("linux/amd64"),
		testcontainers.WithCmdArgs("--insecure-registry=localhost:5000"),
		testcontainers.WithEnv(map[string]string{
			// Always disable TLS for simplicity in tests
			"DOCKER_TLS_CERTDIR": "",
		}),
		tcnetwork.WithNetwork([]string{netAlias}, testNetwork),
		testcontainers.WithExposedPorts("5000/tcp"),
	}

	dindContainer, err := dind.Run(ctx, "docker:dind", dindOpts...)
	testcontainers.CleanupContainer(t, dindContainer)
	require.NoError(t, err, "failed to start dind container")

	// set DOCKER_HOST to the dind container
	dockerHost, err := dindContainer.Host(ctx)
	require.NoError(t, err)
	dockerHost = strings.Replace(dockerHost, "http://", "tcp://", 1)
	env.dockerHost = dockerHost
	t.Setenv("DOCKER_HOST", dockerHost)

	// create a docker client for the dind container
	client, err := client.NewClientWithOpts(client.WithHost(dockerHost))
	require.NoError(t, err)

	env.dindContainer = dindContainer
	env.dindClient = client

	// get the port the registry is mapped to
	port, err := dindContainer.MappedPort(ctx, "5000/tcp")
	require.NoError(t, err)

	// set the registry host to the internal and external ports
	env.internalRegistryHost = net.JoinHostPort("localhost", "5000")
	env.externalRegistryHost = net.JoinHostPort("localhost", port.Port())
}

func (env *DockerTestEnv) startRegistry(t testing.TB, cfg *config) {
	ctx := t.Context()

	registryContainerName := "test-registry"

	client := env.DockerClient()

	// Pull the registry image first
	reader, err := client.ImagePull(ctx, "docker.io/library/registry:3", image.PullOptions{})
	require.NoError(t, err)
	defer reader.Close()

	// Wait for pull to complete
	_, err = io.Copy(io.Discard, reader)
	require.NoError(t, err)

	// Create the registry container
	resp, err := client.ContainerCreate(ctx, &container.Config{
		Image: "registry:3",
		Env: []string{
			"REGISTRY_HTTP_ADDR=:5000",
		},
		ExposedPorts: nat.PortSet{
			"5000/tcp": struct{}{},
		},
	}, &container.HostConfig{
		PortBindings: nat.PortMap{
			"5000/tcp": []nat.PortBinding{{HostPort: "5000"}},
		},
	}, nil, nil, registryContainerName)
	require.NoError(t, err)

	// Start the container
	err = client.ContainerStart(ctx, resp.ID, container.StartOptions{})
	require.NoError(t, err)

	// copy the registry data to the container
	if cfg.registryData != nil {
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)

		err = tw.AddFS(cfg.registryData)
		require.NoError(t, err)
		err = tw.Close()
		require.NoError(t, err)

		err = client.CopyToContainer(ctx, resp.ID, "/var/lib/registry/", &buf, container.CopyToContainerOptions{})
		require.NoError(t, err)
	}

	waitForReady := func() error {
		ctxWithTimeout, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		for {
			select {
			case <-ctxWithTimeout.Done():
				return ctxWithTimeout.Err()
			case <-time.After(100 * time.Millisecond):
				execResp, err := client.ContainerExecCreate(ctxWithTimeout, resp.ID, container.ExecOptions{
					Cmd: []string{"wget", "--spider", "-q", "http://localhost:5000/v2/"},
				})
				if err == nil {
					err = client.ContainerExecStart(ctxWithTimeout, execResp.ID, container.ExecStartOptions{})
					if err == nil {
						execInspect, err := client.ContainerExecInspect(ctxWithTimeout, execResp.ID)
						if err == nil && execInspect.ExitCode == 0 {
							// Registry is ready
							return nil
						}
					}
				}
			}
		}
	}

	require.NoError(t, waitForReady())

	// Store container info for cleanup
	env.registryContainerID = resp.ID

	// note, ignore cleanup since everything vanishes when dind is stopped
}
