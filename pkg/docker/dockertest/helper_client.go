package dockertest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/client"
	"github.com/stretchr/testify/require"
)

// NewHelperClient returns a Docker client for testing.
// It skips the test if Docker is not available.
func NewHelperClient(t testing.TB) *HelperClient {
	t.Helper()

	// Check if we should skip integration tests
	if os.Getenv("SKIP_INTEGRATION_TESTS") == "1" {
		t.Skip("Skipping integration tests")
	}

	// Create Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}

	// Verify Docker daemon is running
	_, err = cli.Ping(t.Context())
	if err != nil {
		t.Skip("Docker daemon is not running")
	}

	helper := &HelperClient{
		Client:   cli,
		fixtures: make(map[string]*imageFixture),
		mu:       &sync.Mutex{},
	}

	t.Cleanup(func() {
		for _, img := range helper.fixtures {
			_, err := helper.Client.ImageRemove(context.Background(), img.imageID, image.RemoveOptions{Force: true, PruneChildren: true})
			if err != nil {
				t.Logf("Warning: Failed to remove image %q: %v", img.imageID, err)
			}
		}

		if err := cli.Close(); err != nil {
			t.Fatalf("Failed to close Docker client: %v", err)
		}
	})

	return helper
}

type HelperClient struct {
	Client *client.Client

	mu       *sync.Mutex
	fixtures map[string]*imageFixture
}

func (c *HelperClient) Close() error {
	return c.Client.Close()
}

func (c *HelperClient) PullImage(t testing.TB, ref string) error {
	t.Helper()
	out, err := c.Client.ImagePull(t.Context(), ref, image.PullOptions{})
	if err != nil {
		return err
	}
	defer out.Close()

	t.Cleanup(func() {
		t.Logf("Removing image %q", ref)

		opts := image.RemoveOptions{
			Force: true,
		}

		// use a background context because t.Context() is already closed when cleanup functions are called
		if _, err := c.Client.ImageRemove(context.Background(), ref, opts); err != nil {
			t.Logf("Warning: Failed to remove image %q: %v", ref, err)
		}
	})

	_, err = io.Copy(os.Stderr, out)
	return err
}

func (c *HelperClient) MustPullImage(t testing.TB, ref string) {
	t.Helper()
	require.NoError(t, c.PullImage(t, ref), "Failed to pull image %q", ref)
}

func (c *HelperClient) PushImage(t testing.TB, ref string) error {
	t.Helper()

	// Create auth config for the registry
	authConfig := registry.AuthConfig{
		// "username": "testuser",
		// "password": "testpassword",
	}
	authBytes, err := json.Marshal(authConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal auth config: %w", err)
	}
	authStr := base64.URLEncoding.EncodeToString(authBytes)

	out, err := c.Client.ImagePush(t.Context(), ref, image.PushOptions{
		RegistryAuth: authStr,
	})
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(os.Stdout, out)
	return err
}

func (c *HelperClient) MustPushImage(t testing.TB, ref string) {
	t.Helper()
	require.NoError(t, c.PushImage(t, ref), "Failed to push image %q", ref)
}

func (c *HelperClient) RunContainer(t testing.TB, imageName string) string {
	t.Helper()

	containerConfig := &container.Config{
		Image: imageName,
		Cmd:   []string{"sleep", "60"}, // Run a long sleep to keep container alive
	}
	hostConfig := &container.HostConfig{
		AutoRemove: true,
	}

	resp, err := c.Client.ContainerCreate(t.Context(), containerConfig, hostConfig, nil, nil, "")
	require.NoError(t, err, "Failed to create container")
	containerID := resp.ID
	t.Cleanup(func() {
		t.Logf("Removing container %q", containerID)
		_ = c.Client.ContainerRemove(context.Background(), containerID, container.RemoveOptions{
			RemoveVolumes: true,
			RemoveLinks:   false,
			Force:         true,
		})
	})

	t.Logf("Created container %q", containerID)
	if len(resp.Warnings) > 0 {
		t.Logf("Warnings: %v", resp.Warnings)
	}

	if err := c.Client.ContainerStart(t.Context(), containerID, container.StartOptions{}); err != nil {
		require.NoErrorf(t, err, "Failed to start container")
		t.Cleanup(func() {
			t.Logf("Stopping container %q", containerID)
			_ = c.Client.ContainerStop(context.Background(), containerID, container.StopOptions{
				Timeout: new(int),
			})
		})
	}

	return resp.ID
}

func (c *HelperClient) StopContainer(t testing.TB, containerID string) {
	t.Helper()

	err := c.Client.ContainerStop(t.Context(), containerID, container.StopOptions{
		// set timeout to 0 to force immediate stop
		Timeout: new(int),
	})
	require.NoErrorf(t, err, "Failed to stop container %q", containerID)
}

func (c *HelperClient) InspectImage(t testing.TB, imageRef string) *image.InspectResponse {
	t.Helper()

	img, err := c.Client.ImageInspect(t.Context(), imageRef)
	require.NoError(t, err, "Failed to inspect image %q", imageRef)

	return &img
}

func (c *HelperClient) ImageExists(t testing.TB, imageRef string) bool {
	t.Helper()

	_, err := c.Client.ImageInspect(t.Context(), imageRef)
	return err == nil
}

func (c *HelperClient) DeleteImage(t testing.TB, imageRef string) error {
	t.Helper()

	_, err := c.Client.ImageRemove(t.Context(), imageRef, image.RemoveOptions{
		Force:         true,
		PruneChildren: true,
	})
	return err
}

func (c *HelperClient) MustDeleteImage(t testing.TB, imageRef string) {
	t.Helper()

	_, err := c.Client.ImageRemove(t.Context(), imageRef, image.RemoveOptions{
		Force:         true,
		PruneChildren: true,
	})
	require.NoError(t, err, "Failed to delete image %q", imageRef)
}

func (c *HelperClient) CleanupImage(t testing.TB, imageRef string) {
	t.Helper()

	t.Cleanup(func() {
		_, err := c.Client.ImageRemove(context.Background(), imageRef, image.RemoveOptions{
			Force:         true,
			PruneChildren: true,
		})
		if err != nil {
			t.Logf("Warning: Failed to remove image %q: %v", imageRef, err)
		}
	})
}

func (c *HelperClient) CleanupImages(t testing.TB) {
	t.Helper()

	existingImages, err := c.Client.ImageList(t.Context(), image.ListOptions{})
	require.NoError(t, err, "Failed to list images")

	imageIDs := make([]string, len(existingImages))
	for i, image := range existingImages {
		imageIDs[i] = image.ID
	}

	fmt.Println("existing imageIDs", imageIDs)

	t.Cleanup(func() {
		newImages, err := c.Client.ImageList(context.Background(), image.ListOptions{})
		if err != nil {
			t.Logf("Warning: Failed to list images: %v", err)
			return
		}

		for _, image := range newImages {
			fmt.Println("new image", image.ID)
			if !slices.Contains(imageIDs, image.ID) {
				c.CleanupImage(t, image.ID)
			}
		}
	})
}

func (c *HelperClient) InspectContainer(t testing.TB, containerID string) *container.InspectResponse {
	t.Helper()

	inspect, err := c.Client.ContainerInspect(t.Context(), containerID)
	require.NoError(t, err, "Failed to inspect container %q", containerID)

	return &inspect
}

func (c *HelperClient) ImageFixture(t testing.TB, name string, tag string) {
	t.Helper()
	fixture := c.loadImageFixture(t, name)

	t.Logf("Tagging image fixture %q with %q", fixture.ref, tag)
	if err := c.Client.ImageTag(t.Context(), fixture.imageID, tag); err != nil {
		require.NoError(t, err, "Failed to tag image %q with %q: %v", fixture.ref, tag, err)
	}
	// remove the image when the test is done
	t.Cleanup(func() {
		_, _ = c.Client.ImageRemove(context.Background(), tag, image.RemoveOptions{Force: true})
	})
}

func (c *HelperClient) loadImageFixture(t testing.TB, name string) *imageFixture {
	t.Helper()

	c.mu.Lock()
	defer c.mu.Unlock()

	ref := fmt.Sprintf("cog-test-fixture:%s", name)

	if fixture, ok := c.fixtures[ref]; ok {
		return fixture
	}

	// Get the path of the current file
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("Could not get current file path")
	}

	// Get the directory of the current file
	dir := filepath.Dir(filename)

	// Construct the path to the fixture
	fixturePath := filepath.Join(dir, "testdata", name+".tar")

	t.Logf("Loading image fixture %q from %s", ref, fixturePath)

	f, err := os.Open(fixturePath)
	require.NoError(t, err, "Failed to open fixture %q", name)
	defer f.Close()

	l, err := c.Client.ImageLoad(t.Context(), f)
	require.NoError(t, err, "Failed to load fixture %q", name)
	defer l.Body.Close()
	_, err = io.Copy(os.Stderr, l.Body)
	require.NoError(t, err, "Failed to copy fixture %q", name)

	inspect, err := c.Client.ImageInspect(t.Context(), ref)
	require.NoError(t, err, "Failed to inspect image %q", ref)

	fixture := &imageFixture{
		ref:     ref,
		imageID: inspect.ID,
	}

	c.fixtures[ref] = fixture

	return fixture
}

type imageFixture struct {
	imageID string
	ref     string
}
