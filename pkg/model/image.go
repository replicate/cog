package model

import "github.com/replicate/cog/pkg/global"

// ImageSource indicates where an image was loaded from.
type ImageSource string

const (
	ImageSourceLocal  ImageSource = "local"  // Docker daemon
	ImageSourceRemote ImageSource = "remote" // Registry
	ImageSourceBuild  ImageSource = "build"  // Just built
)

// Image represents an OCI image that may contain a Cog model.
type Image struct {
	Reference string            // Full image reference
	Digest    string            // Content-addressable digest (sha256:...)
	Labels    map[string]string // Image labels
	Platform  *Platform         // OS/architecture
	Source    ImageSource       // Where loaded from
}

// Platform describes the OS and architecture of an image.
type Platform struct {
	OS           string
	Architecture string
}

// Label keys for Cog-specific metadata stored in image labels.
var (
	LabelConfig            = global.LabelNamespace + "config"
	LabelVersion           = global.LabelNamespace + "version"
	LabelOpenAPISchema     = global.LabelNamespace + "openapi_schema"
	LabelWeightsManifest   = global.LabelNamespace + "r8_weights_manifest"
	LabelModelDependencies = global.LabelNamespace + "r8_model_dependencies"
)

// IsCogModel returns true if this image has Cog labels indicating it's a Cog model.
func (i *Image) IsCogModel() bool {
	if i.Labels == nil {
		return false
	}
	_, ok := i.Labels[LabelConfig]
	return ok
}

// CogVersion returns the Cog version that built this image, or empty string if not set.
func (i *Image) CogVersion() string {
	if i.Labels == nil {
		return ""
	}
	return i.Labels[LabelVersion]
}

// Config returns the raw cog.yaml config stored in image labels, or empty string if not set.
func (i *Image) Config() string {
	if i.Labels == nil {
		return ""
	}
	return i.Labels[LabelConfig]
}

// OpenAPISchema returns the OpenAPI schema stored in image labels, or empty string if not set.
func (i *Image) OpenAPISchema() string {
	if i.Labels == nil {
		return ""
	}
	return i.Labels[LabelOpenAPISchema]
}
