package registry_testhelpers

import (
	"fmt"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/registry"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/replicate/cog/pkg/util"
)

// StartTestRegistry starts a test registry container on a random local port populated
// with image data from the testdata/docker directory. It returns a RegistryContainer
// that can be used to inspect the registry and generate absolute image references. It will
// automatically be cleaned when the test finishes.
// This is safe to run concurrently across multiple tests.
func StartTestRegistry(t *testing.T) *RegistryContainer {
	t.Helper()

	_, filename, _, _ := runtime.Caller(0)
	testdataDir := filepath.Join(filepath.Dir(filename), "testdata", "docker")

	registryContainer, err := registry.Run(
		t.Context(),
		"registry:3",
		testcontainers.WithFiles(testcontainers.ContainerFile{
			HostFilePath:      testdataDir,
			ContainerFilePath: "/var/lib/registry/",
			FileMode:          0o755,
		}),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/v2/").WithPort("5000/tcp").
				WithStartupTimeout(10*time.Second),
		),
		testcontainers.WithHostConfigModifier(func(hostConfig *container.HostConfig) {
			// docker only considers localhost:1 through localhost:9999 as insecure. testcontainers
			// picks higher ports by default, so we need to pick one ourselves to allow insecure access
			// without modifying the daemon config.
			port, err := util.PickFreePort(1024, 9999)
			require.NoError(t, err, "Failed to pick free port")
			hostConfig.PortBindings = map[nat.Port][]nat.PortBinding{
				nat.Port("5000/tcp"): {{HostIP: "0.0.0.0", HostPort: strconv.Itoa(port)}},
			}
		}),
	)
	defer testcontainers.CleanupContainer(t, registryContainer)
	require.NoError(t, err, "Failed to start registry container")

	return &RegistryContainer{Container: registryContainer}
}

type RegistryContainer struct {
	Container *registry.RegistryContainer
}

func (c *RegistryContainer) ImageRef(ref string) string {
	return path.Join(c.Container.RegistryName, ref)
}

func (c *RegistryContainer) ImageRefForTest(t *testing.T, label string) string {
	if label == "" {
		label = fmt.Sprintf("test-%d", time.Now().Unix())
	}
	repo := strings.ToLower(t.Name())
	return c.ImageRef(fmt.Sprintf("%s:%s", repo, label))
}
