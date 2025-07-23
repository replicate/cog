package testenv

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/cogpack/testhelpers"
)

func TestIntegration_DockerTestEnv(t *testing.T) {
	testhelpers.RequireIntegrationSuite(t)

	// setup the test environment and run some basic checks
	env := New(t)

	// make sure DOCKER_HOST is set to the dind container in this test scope
	require.Equal(t, env.DockerHost(), os.Getenv("DOCKER_HOST"))

	// Verify we can get a docker client
	client := env.DockerClient()
	require.NotNil(t, client)

	// verify we're in the containerized docker-in-docker environment
	info, err := client.Info(t.Context())
	require.NoError(t, err)
	require.Contains(t, info.OperatingSystem, "(containerized)")

	// verify we have a registry container running
	containers, err := client.ContainerList(t.Context(), container.ListOptions{})
	require.NoError(t, err)
	require.Len(t, containers, 1, "Expected 1 container (the registry), got %d", len(containers))
	require.ElementsMatch(t, containers[0].Names, []string{"/test-registry"})

	t.Run("docker-in-docker", func(t *testing.T) {
		env := env.ScopeT(t)

		t.Run("pull from registry", func(t *testing.T) {
			env := env.ScopeT(t)

			client := env.DockerClient()

			ref := env.Registry().ParseReference("local-alpine:latest")

			env.Daemon().PullImage(ref, &image.PullOptions{})

			_, err = client.ImageInspect(t.Context(), ref.Name())
			require.NoError(t, err)
		})

		t.Run("build with registry base image", func(t *testing.T) {
			env := env.ScopeT(t)

			registryRef := env.Registry().ParseReference("local-alpine:latest")

			dockerfile := fstest.MapFS{
				"Dockerfile": &fstest.MapFile{
					Data: []byte(strings.Join([]string{
						"FROM " + registryRef.Name(),
						"LABEL source=registry",
						"ENV TEST=value",
					}, "\n")),
				},
			}

			ref, inspect := env.Daemon().BuildImage(NewContextFromFS(t, dockerfile))
			assert.NotEmpty(t, ref.String())
			assert.Equal(t, "registry", inspect.Config.Labels["source"])
			assert.Contains(t, inspect.Config.Env, "TEST=value")
		})

		t.Run("build image and push to registry", func(t *testing.T) {
			env := env.ScopeT(t)

			dockerfile := fstest.MapFS{
				"Dockerfile": &fstest.MapFile{
					Data: []byte("FROM scratch\nLABEL test=basic"),
				},
			}

			ref, inspect := env.Daemon().BuildImage(NewContextFromFS(t, dockerfile))
			assert.NotEmpty(t, ref.String())
			assert.Equal(t, "basic", inspect.Config.Labels["test"])

			refToPush := env.Registry().ParseReference(ref.Name())

			env.Daemon().TagImage(ref, refToPush)
			env.Daemon().PushImage(refToPush)

			assert.True(t, env.Registry().RegistryImageExists(refToPush))
		})

		t.Run("build a multi-arch image", func(t *testing.T) {
			env := env.ScopeT(t)

			dockerfile := fstest.MapFS{
				"Dockerfile": &fstest.MapFile{
					Data: []byte(strings.Join([]string{
						"FROM scratch",
						"LABEL test=multi-arch",
					}, "\n")),
				},
			}

			ref, inspect := env.Daemon().BuildImage(NewContextFromFS(t, dockerfile), WithPlatform("linux/s390x"))
			assert.NotEmpty(t, ref.String())
			assert.Equal(t, "multi-arch", inspect.Config.Labels["test"])
			assert.Equal(t, "s390x", inspect.Architecture)
			assert.Equal(t, "linux", inspect.Os)

			remoteTag := env.Registry().ParseReference(ref.Name())

			env.Daemon().TagImage(ref, remoteTag)
			env.Daemon().PushImage(remoteTag)

			assert.True(t, env.Registry().RegistryImageExists(remoteTag))
		})

	})

	t.Run("registry", func(t *testing.T) {
		env := env.ScopeT(t)

		t.Run("resolves internal and external references", func(t *testing.T) {
			env := env.ScopeT(t)

			// parse a reference from a string
			ref := env.Registry().ParseReference("local-alpine:latest")
			assert.Equal(t, env.internalRegistryHost, ref.Context().RegistryStr())
			assert.Equal(t, "local-alpine", ref.Context().RepositoryStr())
			assert.Equal(t, "latest", ref.Identifier())
		})

		t.Run("can inspect from daemon and external client", func(t *testing.T) {
			env := env.ScopeT(t)

			// parse a reference from a string, returns an internal reference
			ref := env.Registry().ParseReference("local-alpine:latest")

			// registry client can see the image (automatically converting to the external ref)
			externalDescriptor := env.Registry().RegistryGetDescriptor(ref)

			// docker daemon can inspect the image (using the internal ref)
			encodedAuth := env.Registry().EncodedRegistryAuth()
			internalDescriptor, err := env.DockerClient().DistributionInspect(t.Context(), ref.Name(), encodedAuth)
			require.NoError(t, err)

			// descriptors point to the same thing
			assert.Equal(t, string(externalDescriptor.Descriptor.MediaType), internalDescriptor.Descriptor.MediaType)
			assert.Equal(t, externalDescriptor.Descriptor.Digest.String(), internalDescriptor.Descriptor.Digest.String())
		})

		t.Run("multi-arch images work", func(t *testing.T) {
			env := env.ScopeT(t)

			ref := env.Registry().ParseReference("local-alpine:latest")
			assert.True(t, env.Registry().RegistryImageExists(ref))

			expectedPlatforms := []v1.Platform{
				{Architecture: "amd64", OS: "linux"},
				{Architecture: "arm64", OS: "linux"},
				{Architecture: "s390x", OS: "linux"},
			}

			for _, platform := range expectedPlatforms {
				t.Run(platform.String(), func(t *testing.T) {
					assert.True(t, env.Registry().RegistryImageExists(ref, remote.WithPlatform(platform)), "expected image to exist")

					img := env.Registry().RegistryGetImage(ref, remote.WithPlatform(platform))
					cfg, err := img.ConfigFile()
					require.NoError(t, err)

					assert.Equal(t, platform.OS, cfg.Platform().OS)
					assert.Equal(t, platform.Architecture, cfg.Platform().Architecture)
				})
			}
		})
	})
}
