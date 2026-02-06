package model

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/docker/dockertest"
	"github.com/replicate/cog/pkg/registry/registrytest"
)

func TestDockerfileFactory_Name(t *testing.T) {
	docker := dockertest.NewMockCommand()
	registry := registrytest.NewMockRegistryClient()

	factory := NewDockerfileFactory(docker, registry)

	require.Equal(t, "dockerfile", factory.Name())
}

func TestDockerfileFactory_ImplementsInterface(t *testing.T) {
	docker := dockertest.NewMockCommand()
	registry := registrytest.NewMockRegistryClient()

	// Verify that DockerfileFactory implements the Factory interface
	var _ = NewDockerfileFactory(docker, registry)
}

func TestDefaultFactory_ReturnsDockerfileFactory(t *testing.T) {
	docker := dockertest.NewMockCommand()
	registry := registrytest.NewMockRegistryClient()

	factory := DefaultFactory(docker, registry)

	require.Equal(t, "dockerfile", factory.Name())
}
