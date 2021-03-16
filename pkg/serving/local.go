package serving

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	log "github.com/sirupsen/logrus"

	"github.com/replicate/modelserver/pkg/docker"
	"github.com/replicate/modelserver/pkg/global"
	"github.com/replicate/modelserver/pkg/model"
	"github.com/replicate/modelserver/pkg/shell"
	"strconv"
)

type LocalDockerPlatform struct {
	client *client.Client
}

type LocalDockerDeployment struct {
	containerID string
	client      *client.Client
	port        int
}

func NewLocalDockerPlatform() (*LocalDockerPlatform, error) {
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("Failed to connect to Docker client: %w", err)
	}
	return &LocalDockerPlatform{
		client: dockerClient,
	}, nil
}

func (p *LocalDockerPlatform) Deploy(mod *model.Model, target model.Target) (Deployment, error) {
	artifact, ok := mod.ArtifactFor(target)
	if !ok {
		return nil, fmt.Errorf("Model has no %s target", target)
	}
	imageTag := artifact.URI

	log.Debugf("Deploying %s for %s", artifact.URI, artifact.Target)

	if err := docker.Pull(imageTag); err != nil {
		return nil, fmt.Errorf("Failed to pull image %s: %w", imageTag, err)
	}

	ctx := context.Background()
	/* requires auth, therefore we just shell out for now
	_, err := p.client.ImagePull(ctx, imageTag, types.ImagePullOptions{})
	if err != nil {
		return nil, fmt.Errorf("Failed to pull Docker image %s: %w", imageTag, err)
	}
	*/

	hostPort, err := shell.NextFreePort(5000)
	if err != nil {
		return nil, err
	}

	jidPort := 5000
	hostBinding := nat.PortBinding{
		HostIP:   "0.0.0.0",
		HostPort: strconv.Itoa(hostPort),
	}
	containerPort, err := nat.NewPort("tcp", strconv.Itoa(jidPort))
	if err != nil {
		return nil, err
	}
	portBindings := nat.PortMap{containerPort: []nat.PortBinding{hostBinding}}

	containerConfig := &container.Config{
		Image: imageTag,
		ExposedPorts: nat.PortSet{
			nat.Port(fmt.Sprintf("%d/tcp", jidPort)): struct{}{},
		},
	}
	hostConfig := &container.HostConfig{
		PortBindings: portBindings,
	}
	resp, err := p.client.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, "")
	if err != nil {
		return nil, fmt.Errorf("Failed to create Docker container for image %s: %w", imageTag, err)
	}

	containerID := resp.ID

	if err := p.client.ContainerStart(ctx, containerID, types.ContainerStartOptions{}); err != nil {
		return nil, fmt.Errorf("Failed to start Docker container for image %s: %w", imageTag, err)
	}

	if err := shell.WaitForHTTPOK(fmt.Sprintf("http://localhost:%d/ping", hostPort), global.StartupTimeout); err != nil {
		return nil, err
	}

	return &LocalDockerDeployment{
		containerID: containerID,
		client:      p.client,
		port:        hostPort,
	}, nil
}

func (d *LocalDockerDeployment) Undeploy() error {
	if err := d.client.ContainerStop(context.Background(), d.containerID, nil); err != nil {
		return fmt.Errorf("Failed to stop Docker container %s: %w", d.containerID, err)
	}
	return nil
}

func (d *LocalDockerDeployment) RunInference(input *Example) (*Result, error) {
	form := url.Values{}
	for key, val := range input.Values {
		form.Set(key, val)
	}
	resp, err := http.PostForm(fmt.Sprintf("http://localhost:%d/infer", d.port), form)
	if err != nil {
		return nil, fmt.Errorf("Failed to run inference: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("/infer call returned status %d", resp.StatusCode)
	}

	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, resp.Body); err != nil {
		return nil, fmt.Errorf("Failed to read response: %w", err)
	}

	result := &Result{
		Values: map[string]string{
			"output": buf.String(),
		},
	}
	return result, nil
}
