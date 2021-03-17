package serving

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	log "github.com/sirupsen/logrus"

	"strconv"

	"github.com/replicate/modelserver/pkg/docker"
	"github.com/replicate/modelserver/pkg/global"
	"github.com/replicate/modelserver/pkg/model"
	"github.com/replicate/modelserver/pkg/shell"
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

/*
func main() {

	var client *http.Client
	var remoteURL string

	//prepare the reader instances to encode
	values := map[string]io.Reader{
		"file":  mustOpen("main.go"), // lets assume its this file
		"other": strings.NewReader("hello world!"),
	}
	err := Upload(client, remoteURL, values)
	if err != nil {
		panic(err)
	}
}

func Upload(client *http.Client, url string, values map[string]io.Reader) (err error) {
	// Prepare a form that you will submit to that URL.
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for key, r := range values {
		var fw io.Writer
		if x, ok := r.(io.Closer); ok {
			defer x.Close()
		}
		// Add an image file
		if x, ok := r.(*os.File); ok {
			if fw, err = w.CreateFormFile(key, x.Name()); err != nil {
				return
			}
		} else {
			// Add other fields
			if fw, err = w.CreateFormField(key); err != nil {
				return
			}
		}
		if _, err = io.Copy(fw, r); err != nil {
			return err
		}

	}
	// Don't forget to close the multipart writer.
	// If you don't close it, your request will be missing the terminating boundary.
	w.Close()

	// Now that you have a form, you can submit it to your handler.
	req, err := http.NewRequest("POST", url, &b)
	if err != nil {
		return
	}
	// Don't forget to set the content type, this will contain the boundary.
	req.Header.Set("Content-Type", w.FormDataContentType())

	// Submit the request
	res, err := client.Do(req)
	if err != nil {
		return
	}

	// Check the response
	if res.StatusCode != http.StatusOK {
		err = fmt.Errorf("bad status: %s", res.Status)
	}
	return
}

func mustOpen(f string) *os.File {
	r, err := os.Open(f)
	if err != nil {
		panic(err)
	}
	return r
	}
*/

func (d *LocalDockerDeployment) Help() (*HelpResponse, error) {
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/help", d.port))
	if err != nil {
		return nil, fmt.Errorf("Failed to GET /help: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("/help call returned status %d", resp.StatusCode)
	}

	help := new(HelpResponse)
	if err := json.NewDecoder(resp.Body).Decode(help); err != nil {
		return nil, fmt.Errorf("Failed to parse /help body: %w", err)
	}

	return help, nil
}
