//go:build ignore

package types

import (
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/replicate/cog/pkg/config"
)

// BuildEnv holds the build context and state for operations
type BuildEnv struct {
	// BuildKitClient client.Client
	Config     *config.Config
	WorkingDir string
	Platform   ocispec.Platform
}
