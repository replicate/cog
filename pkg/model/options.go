package model

import "github.com/replicate/cog/pkg/config"

// BuildOptions contains all settings for building a Cog image.
// This consolidates the many parameters previously passed to image.Build().
type BuildOptions struct {
	// ImageName is the output image name (required).
	ImageName string

	// NoCache disables build cache.
	NoCache bool

	// SeparateWeights builds weights as a separate layer.
	SeparateWeights bool

	// Strip removes debug symbols from binaries.
	Strip bool

	// Precompile precompiles Python bytecode.
	Precompile bool

	// UseCudaBaseImage controls CUDA base image usage: "auto", "true", or "false".
	UseCudaBaseImage string

	// UseCogBaseImage controls cog base image usage. nil means auto-detect.
	UseCogBaseImage *bool

	// Secrets are build-time secrets to pass to the build.
	Secrets []string

	// ProgressOutput controls build output format: "auto", "plain", or "tty".
	ProgressOutput string

	// Annotations are extra labels to add to the image.
	Annotations map[string]string

	// SchemaFile is a custom OpenAPI schema file path.
	SchemaFile string

	// DockerfileFile is a custom Dockerfile path.
	DockerfileFile string

	// WeightsLockPath is the path to weights.lock file.
	// Default: weights.lock in project directory.
	WeightsLockPath string

	// TODO(md): OCIIndex is a temporary gate. When true, builds produce weight
	// artifacts and pushes create an OCI Image Index. Set via COG_OCI_INDEX=1.
	// Remove this field once index pushes are validated with all registries.
	OCIIndex bool

	// ExcludeSource skips the COPY . /src step in the generated Dockerfile.
	// Used by `cog serve` to produce an image identical to `cog build` minus
	// the source copy — the source directory is volume-mounted at runtime.
	// All other layers (wheel installs, apt, etc.) are shared with `cog build`
	// via Docker layer caching.
	ExcludeSource bool
}

// WithDefaults returns a copy of BuildOptions with defaults applied from Source.
// This fills in sensible defaults for any unset fields.
func (o BuildOptions) WithDefaults(src *Source) BuildOptions {
	// Default image name from project directory
	if o.ImageName == "" {
		o.ImageName = config.DockerImageName(src.ProjectDir)
	}

	// Default progress output
	if o.ProgressOutput == "" {
		o.ProgressOutput = "auto"
	}

	return o
}
