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

	// ImageFormat specifies the desired OCI format for the built image.
	// FormatStandalone (default): Traditional single OCI image
	// FormatBundle: OCI Image Index with weights artifact (requires weights.lock)
	ImageFormat ModelImageFormat

	// WeightsLockPath is the path to weights.lock file.
	// Only used when ImageFormat == FormatBundle.
	// Default: weights.lock in project directory.
	WeightsLockPath string
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

// BuildBaseOptions contains settings for building a base image (dev mode).
// Base images don't copy /src - the source is mounted as a volume at runtime.
type BuildBaseOptions struct {
	// UseCudaBaseImage controls CUDA base image usage: "auto", "true", or "false".
	UseCudaBaseImage string

	// UseCogBaseImage controls cog base image usage. nil means auto-detect.
	UseCogBaseImage *bool

	// ProgressOutput controls build output format: "auto", "plain", or "tty".
	ProgressOutput string

	// RequiresCog indicates whether the build requires cog to be installed.
	RequiresCog bool
}

// WithDefaults returns a copy of BuildBaseOptions with defaults applied.
func (o BuildBaseOptions) WithDefaults() BuildBaseOptions {
	if o.ProgressOutput == "" {
		o.ProgressOutput = "auto"
	}
	return o
}
