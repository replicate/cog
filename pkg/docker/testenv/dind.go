package testenv

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/stretchr/testify/assert"
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

// FileExists checks if a file exists in the given image
func (d *TestDaemon) FileExists(imageRef name.Reference, filePath string) bool {
	t := d.env.t
	t.Helper()

	client := d.env.dindClient
	ctx := t.Context()

	// Create container (don't start it)
	// Use a dummy command that would work on any image
	resp, err := client.ContainerCreate(ctx, &container.Config{
		Image: imageRef.Name(),
		Cmd:   []string{"true"}, // dummy command, won't be executed
	}, nil, nil, nil, "")
	require.NoError(t, err, "failed to create container")

	// Ensure cleanup
	defer func() {
		err := client.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		require.NoError(t, err, "failed to remove container")
	}()

	// Try to stat the file using docker cp API
	_, err = client.ContainerStatPath(ctx, resp.ID, filePath)
	return err == nil
}

// FileContent retrieves the content of a file from the given image
func (d *TestDaemon) FileContent(imageRef name.Reference, filePath string) ([]byte, error) {
	t := d.env.t
	t.Helper()

	client := d.env.dindClient
	ctx := t.Context()

	// Create container (don't start it)
	// Use a dummy command that would work on any image
	resp, err := client.ContainerCreate(ctx, &container.Config{
		Image: imageRef.Name(),
		Cmd:   []string{"true"}, // dummy command, won't be executed
	}, nil, nil, nil, "")
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	// Ensure cleanup
	defer func() {
		_ = client.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
	}()

	// Copy file from container
	reader, _, err := client.CopyFromContainer(ctx, resp.ID, filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to copy file from container: %w", err)
	}
	defer reader.Close()

	// The response is a tar stream, we need to extract the file content
	tr := tar.NewReader(reader)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read tar stream: %w", err)
		}

		// The tar will contain the file we requested
		if header.Typeflag == tar.TypeReg {
			content, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("failed to read file content: %w", err)
			}
			return content, nil
		}
	}

	return nil, fmt.Errorf("file not found in tar stream")
}

// FileInfo retrieves file information including permissions from the given image
func (d *TestDaemon) FileInfo(imageRef name.Reference, filePath string) (*tar.Header, error) {
	t := d.env.t
	t.Helper()

	client := d.env.dindClient
	ctx := t.Context()

	// Create container (don't start it)
	// Use a dummy command that would work on any image
	resp, err := client.ContainerCreate(ctx, &container.Config{
		Image: imageRef.Name(),
		Cmd:   []string{"true"}, // dummy command, won't be executed
	}, nil, nil, nil, "")
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	// Ensure cleanup
	defer func() {
		_ = client.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
	}()

	// Copy file from container
	reader, _, err := client.CopyFromContainer(ctx, resp.ID, filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to copy file from container: %w", err)
	}
	defer reader.Close()

	// The response is a tar stream, we need to extract the file header
	tr := tar.NewReader(reader)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read tar stream: %w", err)
		}

		// Return the first regular file header we find
		if header.Typeflag == tar.TypeReg {
			return header, nil
		}
	}

	return nil, fmt.Errorf("file not found in tar stream")
}

// AssertFileExists asserts that a file exists in the given image
func (d *TestDaemon) AssertFileExists(t *testing.T, imageRef name.Reference, filePath string) {
	t.Helper()
	if !d.FileExists(imageRef, filePath) {
		assert.Fail(t, "expected file %s to exist in image %s, but it does not", filePath, imageRef.Name())
	}
}

// AssertFileNotExists asserts that a file does not exist in the given image
func (d *TestDaemon) AssertFileNotExists(t *testing.T, imageRef name.Reference, filePath string) {
	t.Helper()
	if d.FileExists(imageRef, filePath) {
		assert.Fail(t, "expected file %s to not exist in image %s, but it does", filePath, imageRef.Name())
	}
}
