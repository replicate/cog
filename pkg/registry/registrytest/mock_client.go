package registrytest

import (
	"context"

	v1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/replicate/cog/pkg/registry"
)

type MockRegistryClient struct {
	mockImages map[string]bool
}

func NewMockRegistryClient() *MockRegistryClient {
	return &MockRegistryClient{
		mockImages: map[string]bool{},
	}
}

func (c *MockRegistryClient) Exists(ctx context.Context, imageRef string) (bool, error) {
	_, exists := c.mockImages[imageRef]
	return exists, nil
}

func (c *MockRegistryClient) GetImage(ctx context.Context, imageRef string, platform *registry.Platform) (v1.Image, error) {
	return nil, nil
}

func (c *MockRegistryClient) Inspect(ctx context.Context, imageRef string, platform *registry.Platform) (*registry.ManifestResult, error) {
	return nil, nil
}

func (c *MockRegistryClient) AddMockImage(imageRef string) {
	c.mockImages[imageRef] = true
}

func (c *MockRegistryClient) PushImage(ctx context.Context, ref string, img v1.Image) error {
	c.mockImages[ref] = true
	return nil
}

func (c *MockRegistryClient) PushIndex(ctx context.Context, ref string, idx v1.ImageIndex) error {
	c.mockImages[ref] = true
	return nil
}

func (c *MockRegistryClient) WriteLayer(ctx context.Context, repo string, layer v1.Layer) error {
	return nil
}

func (c *MockRegistryClient) WriteLayerWithProgress(ctx context.Context, repo string, layer v1.Layer, progressCh chan<- v1.Update) error {
	return nil
}
