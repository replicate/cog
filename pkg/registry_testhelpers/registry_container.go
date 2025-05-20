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
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/registry"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/crypto/bcrypt"

	dockerregistry "github.com/docker/docker/api/types/registry"

	"github.com/replicate/cog/pkg/util"
)

// StartTestRegistry starts a test registry container on a random local port populated
// with image data from the testdata/docker directory. It returns a RegistryContainer
// that can be used to inspect the registry and generate absolute image references. It will
// automatically be cleaned when the test finishes.
// This is safe to run concurrently across multiple tests.
func StartTestRegistry(t *testing.T, opts ...Option) *RegistryContainer {
	t.Helper()

	options := &options{}
	for _, opt := range opts {
		opt(options)
	}

	_, filename, _, _ := runtime.Caller(0)
	testdataDir := filepath.Join(filepath.Dir(filename), "testdata", "docker")

	containerCustomizers := []testcontainers.ContainerCustomizer{
		testcontainers.WithFiles(testcontainers.ContainerFile{
			HostFilePath:      testdataDir,
			ContainerFilePath: "/var/lib/registry/",
			FileMode:          0o755,
		}),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/").WithPort("5000/tcp").
				WithStartupTimeout(10 * time.Second),
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
	}

	if options.auth != nil {
		htpasswd, err := generateHtpasswd(options.auth.Username, options.auth.Password)
		require.NoError(t, err)
		containerCustomizers = append(containerCustomizers,
			registry.WithHtpasswd(htpasswd),
		)
	}

	registryContainer, err := registry.Run(
		t.Context(),
		"registry:3",
		containerCustomizers...,
	)
	defer testcontainers.CleanupContainer(t, registryContainer)
	require.NoError(t, err, "Failed to start registry container")

	return &RegistryContainer{
		Container: registryContainer,
		options:   options,
	}
}

type RegistryContainer struct {
	Container *registry.RegistryContainer
	options   *options
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

func (c *RegistryContainer) CloneRepo(t *testing.T, existingRepo, newRepo string) string {
	existingRepo = c.ImageRef(existingRepo)
	newRepo = c.ImageRef(newRepo)

	err := crane.CopyRepository(existingRepo, newRepo)
	require.NoError(t, err, "Failed to clone repo %q to %q", existingRepo, newRepo)
	return newRepo
}

func (c *RegistryContainer) CloneRepoForTest(t *testing.T, repo string) string {
	return c.CloneRepo(t, repo, strings.ToLower(t.Name()))
}

func (c *RegistryContainer) ImageExists(t *testing.T, ref string) error {
	parsedRef, err := name.ParseReference(ref, name.WithDefaultRegistry(c.RegistryHost()))
	require.NoError(t, err)

	var opts []remote.Option

	if c.options.auth != nil {
		opts = append(opts, remote.WithAuth(authn.FromConfig(authn.AuthConfig{
			Username: c.options.auth.Username,
			Password: c.options.auth.Password,
		})))
	}
	_, err = remote.Head(parsedRef, opts...)
	return err
}

func (c *RegistryContainer) RegistryHost() string {
	return c.Container.RegistryName
}

type Option func(*options)

func WithAuth(username, password string) func(*options) {
	return func(o *options) {
		o.auth = &dockerregistry.AuthConfig{
			Username: username,
			Password: password,
		}
	}
}

type options struct {
	auth *dockerregistry.AuthConfig
}

func generateHtpasswd(username, password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%s", username, string(hash)), nil
}
