package testenv

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/docker/dockertest"
)

type TestDaemon struct {
	env *DockerTestEnv
}

func (d *TestDaemon) InspectImage(ref name.Reference) image.InspectResponse {
	t := d.env.t
	t.Helper()

	resp, err := d.env.dindClient.ImageInspect(t.Context(), ref.Name())
	require.NoError(t, err)
	return resp
}

func (d *TestDaemon) PushImage(ref name.Reference) {
	t := d.env.t
	t.Helper()

	client := d.env.dindClient

	opts := image.PushOptions{
		RegistryAuth: d.env.Registry().EncodedRegistryAuth(),
	}

	r, err := client.ImagePush(t.Context(), ref.Name(), opts)
	require.NoError(t, err, "failed to push image")

	err = jsonmessage.DisplayJSONMessagesStream(r, os.Stdout, 0, false, nil)
	require.NoError(t, err, "failed to read push stream")
}

func (d *TestDaemon) PullImage(ref name.Reference, options *image.PullOptions) {
	t := d.env.t
	t.Helper()

	client := d.env.dindClient

	if options == nil {
		options = &image.PullOptions{}
	}

	if options.RegistryAuth == "" {
		options.RegistryAuth = d.env.Registry().EncodedRegistryAuth()
	}

	if options.Platform == "" {
		options.Platform = "linux/amd64"
	}

	r, err := client.ImagePull(t.Context(), ref.Name(), *options)
	require.NoError(t, err, "failed to pull image")

	err = jsonmessage.DisplayJSONMessagesStream(r, os.Stdout, 0, false, nil)
	require.NoError(t, err, "failed to read pull stream")
}

func (d *TestDaemon) TagImage(source, tag name.Reference) {
	t := d.env.t
	t.Helper()

	client := d.env.dindClient

	err := client.ImageTag(t.Context(), source.Name(), tag.Name())
	require.NoError(t, err, "failed to tag image")
}

// BuildImage builds an image in the test environment
func (d *TestDaemon) BuildImage(buildContext io.Reader, opts ...BuildOption) (name.Reference, image.InspectResponse) {
	t := d.env.t
	t.Helper()

	client := d.env.dindClient

	tagForTest := dockertest.NewRandomRef(t)

	options := build.ImageBuildOptions{
		Version:  build.BuilderBuildKit,
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

	resp, err := client.ImageBuild(t.Context(), buildContext, options)
	require.NoError(t, err, "failed to build image")
	defer resp.Body.Close()

	// grab the digest (image id) from the stream
	var digest string
	err = jsonmessage.DisplayJSONMessagesStream(resp.Body, os.Stdout, 0, false, func(msg jsonmessage.JSONMessage) {
		if msg.ID == "moby.image.id" {
			var aux map[string]any
			err := json.Unmarshal(*msg.Aux, &aux)
			require.NoError(t, err)
			if aux["ID"] != nil {
				digest = aux["ID"].(string)
			}

		}
	})
	require.NoError(t, err, "failed to read build stream")

	id := strings.Split(digest, ":")[1]

	inspectResp, err := client.ImageInspect(t.Context(), id)
	require.NoError(t, err, "failed to inspect image")

	return tagForTest.Ref(), inspectResp
}

// BuildOption configures image building
type BuildOption func(*build.ImageBuildOptions)

// WithPlatform sets the target platform for the build
func WithPlatform(platform string) BuildOption {
	return func(o *build.ImageBuildOptions) {
		o.Platform = platform
	}
}

// WithTags sets the tags for the built image
func WithTags(tags ...string) BuildOption {
	return func(o *build.ImageBuildOptions) {
		o.Tags = tags
	}
}

// NewContextFromFS creates a build context from an fs.FS
func NewContextFromFS(t *testing.T, filesystem fs.FS) io.Reader {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	err := tw.AddFS(filesystem)
	require.NoError(t, err)
	tw.Close()

	return &buf
}
