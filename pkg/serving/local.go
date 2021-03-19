package serving

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/shell"
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

func (p *LocalDockerPlatform) Deploy(mod *model.Model, target model.Target, logWriter func(string)) (Deployment, error) {
	// TODO(andreas): output container logs

	artifact, ok := mod.ArtifactFor(target)
	if !ok {
		return nil, fmt.Errorf("Model has no %s target", target)
	}
	imageTag := artifact.URI

	logWriter(fmt.Sprintf("Deploying container for target %s", artifact.Target))

	if err := docker.Pull(imageTag, logWriter); err != nil {
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

	if err := p.waitForContainerReady(hostPort, containerID, logWriter); err != nil {
		logs, err2 := getContainerLogs(p.client, containerID)
		if err2 != nil {
			return nil, err2
		}
		logWriter(logs)
		return nil, err
	}

	return &LocalDockerDeployment{
		containerID: containerID,
		client:      p.client,
		port:        hostPort,
	}, nil
}

func (p *LocalDockerPlatform) waitForContainerReady(hostPort int, containerID string, logWriter func(string)) error {
	url := fmt.Sprintf("http://localhost:%d/ping", hostPort)

	start := time.Now()
	logWriter("Waiting for model container to become accessible")
	for {
		now := time.Now()
		if now.Sub(start) > global.StartupTimeout {
			return fmt.Errorf("Timed out")
		}

		time.Sleep(100 * time.Millisecond)

		cont, err := p.client.ContainerInspect(context.Background(), containerID)
		if err != nil {
			return fmt.Errorf("Failed to get container status: %w", err)
		}
		if cont.State != nil && (cont.State.Status == "exited" || cont.State.Status == "dead") {
			return fmt.Errorf("Container exited unexpectedly")
		}

		resp, err := http.Get(url)
		if err != nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			continue
		}
		logWriter("Got successful ping response from container")
		return nil
	}
}

func (d *LocalDockerDeployment) Undeploy() error {
	if err := d.client.ContainerStop(context.Background(), d.containerID, nil); err != nil {
		return fmt.Errorf("Failed to stop Docker container %s: %w", d.containerID, err)
	}
	return nil
}

func (d *LocalDockerDeployment) RunInference(input *Example, logWriter func(string)) (*Result, error) {
	form := url.Values{}
	for key, val := range input.Values {
		form.Set(key, val)
	}
	resp, err := http.PostForm(fmt.Sprintf("http://localhost:%d/infer", d.port), form)
	if err != nil {
		logs, err2 := getContainerLogs(d.client, d.containerID)
		if err2 != nil {
			return nil, err2
		}
		logWriter(logs)
		return nil, fmt.Errorf("Failed to run inference: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logs, err2 := getContainerLogs(d.client, d.containerID)
		if err2 != nil {
			return nil, err2
		}
		logWriter(logs)
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

func (d *LocalDockerDeployment) Help(logWriter func(string)) (*HelpResponse, error) {
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/help", d.port))
	if err != nil {
		logs, err2 := getContainerLogs(d.client, d.containerID)
		if err2 != nil {
			return nil, err2
		}
		logWriter(logs)
		return nil, fmt.Errorf("Failed to GET /help: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logs, err2 := getContainerLogs(d.client, d.containerID)
		if err2 != nil {
			return nil, err2
		}
		logWriter(logs)
		return nil, fmt.Errorf("/help call returned status %d", resp.StatusCode)
	}

	help := new(HelpResponse)
	if err := json.NewDecoder(resp.Body).Decode(help); err != nil {
		logs, err2 := getContainerLogs(d.client, d.containerID)
		if err2 != nil {
			return nil, err2
		}
		logWriter(logs)
		return nil, fmt.Errorf("Failed to parse /help body: %w", err)
	}

	return help, nil
}

func getContainerLogs(c *client.Client, containerID string) (string, error) {
	opts := types.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	}
	reader, err := c.ContainerLogs(context.Background(), containerID, opts)
	if err != nil {
		return "", err
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
