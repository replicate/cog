package dind

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/dind"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/docker/dockertest"
	"github.com/replicate/cog/pkg/util"
)

func NewDind(t *testing.T) *Dind {
	ctx := t.Context()

	dindContainer, err := dind.Run(ctx, "docker:dind", testcontainers.WithImagePlatform("linux/amd64"))
	t.Cleanup(func() {
		testcontainers.CleanupContainer(t, dindContainer)
	})
	require.NoError(t, err, "failed to start dind container")

	dockerHost, err := dindContainer.Host(ctx)
	require.NoError(t, err)
	dockerHost = strings.Replace(dockerHost, "http://", "tcp://", 1)

	client, err := docker.NewAPIClient(ctx, docker.WithHost(dockerHost))
	require.NoError(t, err)

	return &Dind{
		t:         t,
		container: dindContainer,
		client:    client,
	}
}

type Dind struct {
	t         *testing.T
	container testcontainers.Container
	client    command.Command
}

func (d *Dind) Provider() command.Command {
	return d.client
}

func (d *Dind) DockerClient() client.APIClient {
	dockerClient, err := d.client.DockerClient()
	require.NoError(d.t, err)
	return dockerClient
}

type BuildOpt func(o *build.ImageBuildOptions)

func WithPlatform(platform string) BuildOpt {
	return func(o *build.ImageBuildOptions) {
		o.Platform = platform
	}
}

func (d *Dind) BuildTestImage(t *testing.T, buildCtx io.Reader, opts ...BuildOpt) (name.Reference, image.InspectResponse) {
	tagForTest := dockertest.NewRandomRef(t)

	options := build.ImageBuildOptions{
		Tags:     []string{tagForTest.String()},
		Platform: "linux/amd64",
		Outputs: []build.ImageBuildOutput{
			{
				Type: "moby",
				Attrs: map[string]string{
					"name": tagForTest.String(),
				},
			},
		},
	}

	for _, opt := range opts {
		opt(&options)
	}

	fmt.Printf("building image with options %+v\n", options)

	c, err := d.Provider().DockerClient()
	require.NoError(t, err)
	resp, err := c.ImageBuild(t.Context(), buildCtx, options)
	require.NoError(t, err)
	defer resp.Body.Close()

	var digest string

	err = jsonmessage.DisplayJSONMessagesStream(resp.Body, os.Stdout, 0, false, func(msg jsonmessage.JSONMessage) {
		var aux map[string]interface{}
		err := json.Unmarshal(*msg.Aux, &aux)
		require.NoError(t, err)
		if aux["ID"] != nil {
			digest = aux["ID"].(string)
		}
	})
	require.NoError(t, err)

	fmt.Println("digest", digest)
	id := strings.Split(digest, ":")[1]

	// platform := &ocispec.Platform{OS: "linux", Architecture: "s390x"}

	// digestRef := tagForTest.WithDigest(digest)

	// digestRef, err := name.NewDigest(digest)
	// require.NoError(t, err)

	fmt.Println("BEFORE INSPECT", id)
	xyz, err := name.ParseReference(id)
	require.NoError(t, err)
	fmt.Println("xyz", xyz.String())
	// , client.ImageInspectWithPlatform(platform)

	inspectResp, err := c.ImageInspect(t.Context(), id)
	require.NoError(t, err)

	fmt.Println("AFTER INSPECT")
	util.JSONPrettyPrint(inspectResp)

	return tagForTest.Ref(), inspectResp
}
