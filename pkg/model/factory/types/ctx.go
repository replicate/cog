//go:build ignore

package types

import (
	"context"

	gatewayClient "github.com/moby/buildkit/frontend/gateway/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/replicate/cog/pkg/config"
)

type Context struct {
	context.Context

	Config     *config.Config
	WorkingDir string
	Platform   ocispec.Platform
	Client     gatewayClient.Client
}
