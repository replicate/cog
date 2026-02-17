package model

import (
	"encoding/json"

	"github.com/getkin/kin-openapi/openapi3"
	v1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/global"
)

// ImageSource indicates where an image was loaded from.
type ImageSource string

const (
	ImageSourceLocal  ImageSource = "local"  // Docker daemon
	ImageSourceRemote ImageSource = "remote" // Registry
	ImageSourceBuild  ImageSource = "build"  // Just built
)

// Platform describes the OS and architecture of an image.
type Platform struct {
	OS           string
	Architecture string
	Variant      string
}

// Label keys for Cog-specific metadata stored in image labels.
var (
	LabelConfig          = global.LabelNamespace + "config"
	LabelVersion         = global.LabelNamespace + "version"
	LabelOpenAPISchema   = global.LabelNamespace + "openapi_schema"
	LabelWeightsManifest = global.LabelNamespace + "r8_weights_manifest"
)

// =============================================================================
// ImageSpec
// =============================================================================

// ImageSpecOption configures optional fields on ImageSpec.
type ImageSpecOption func(*ImageSpec)

// WithImageSecrets sets build-time secrets for the image build.
func WithImageSecrets(secrets []string) ImageSpecOption {
	return func(s *ImageSpec) {
		s.Secrets = secrets
	}
}

// WithImageNoCache disables build cache for the image build.
func WithImageNoCache(noCache bool) ImageSpecOption {
	return func(s *ImageSpec) {
		s.NoCache = noCache
	}
}

// ImageSpec declares an image to be built.
// It implements ArtifactSpec.
//
// TODO: ImageBuilder currently reads build options from BuildOptions (passed at
// construction) rather than from ImageSpec fields. When the build pipeline fully
// migrates to specs, ImageName/Secrets/NoCache should be the source of truth.
type ImageSpec struct {
	name      string
	ImageName string
	Secrets   []string
	NoCache   bool
}

// NewImageSpec creates an ImageSpec with the given name and image name.
// Optional configuration can be provided via ImageSpecOption functions.
func NewImageSpec(name, imageName string, opts ...ImageSpecOption) *ImageSpec {
	s := &ImageSpec{
		name:      name,
		ImageName: imageName,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Type returns ArtifactTypeImage.
func (s *ImageSpec) Type() ArtifactType { return ArtifactTypeImage }

// Name returns the spec's logical name.
func (s *ImageSpec) Name() string { return s.name }

// =============================================================================
// ImageArtifact
// =============================================================================

// ImageArtifact represents an OCI container image.
// It serves as both the build artifact (in Model.Artifacts) and the general-purpose
// image metadata type throughout the codebase.
// It implements the Artifact interface.
type ImageArtifact struct {
	// Artifact fields (set when used as a build artifact)
	name       string
	descriptor v1.Descriptor

	// Image metadata
	Reference string            // Full image reference (e.g., "r8.im/user/model:latest")
	Digest    string            // Content-addressable digest (sha256:...)
	Labels    map[string]string // Docker/OCI image labels
	Platform  *Platform         // OS/architecture
	Source    ImageSource       // Where loaded from (local/remote/build)
}

// NewImageArtifact creates an ImageArtifact from a build result.
func NewImageArtifact(name string, desc v1.Descriptor, reference string) *ImageArtifact {
	return &ImageArtifact{
		name:       name,
		descriptor: desc,
		Reference:  reference,
	}
}

// Type returns ArtifactTypeImage.
func (a *ImageArtifact) Type() ArtifactType { return ArtifactTypeImage }

// Name returns the artifact's logical name.
func (a *ImageArtifact) Name() string { return a.name }

// Descriptor returns the OCI descriptor for this image.
func (a *ImageArtifact) Descriptor() v1.Descriptor { return a.descriptor }

// =============================================================================
// Image metadata methods (formerly on *Image)
// =============================================================================

// IsCogModel returns true if this image has Cog labels indicating it's a Cog model.
func (a *ImageArtifact) IsCogModel() bool {
	if a.Labels == nil {
		return false
	}
	_, ok := a.Labels[LabelConfig]
	return ok
}

// CogVersion returns the Cog version that built this image, or empty string if not set.
func (a *ImageArtifact) CogVersion() string {
	if a.Labels == nil {
		return ""
	}
	return a.Labels[LabelVersion]
}

// Config returns the raw cog.yaml config stored in image labels, or empty string if not set.
func (a *ImageArtifact) Config() string {
	if a.Labels == nil {
		return ""
	}
	return a.Labels[LabelConfig]
}

// OpenAPISchema returns the OpenAPI schema stored in image labels, or empty string if not set.
func (a *ImageArtifact) OpenAPISchema() string {
	if a.Labels == nil {
		return ""
	}
	return a.Labels[LabelOpenAPISchema]
}

// ParsedConfig returns the parsed cog.yaml config from image labels.
// Returns nil without error if no config label is present.
// Returns error if the label contains invalid JSON.
func (a *ImageArtifact) ParsedConfig() (*config.Config, error) {
	raw := a.Config()
	if raw == "" {
		return nil, nil
	}

	cfg := new(config.Config)
	if err := json.Unmarshal([]byte(raw), cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// ParsedOpenAPISchema returns the parsed OpenAPI schema from image labels.
// Returns nil without error if no schema label is present.
// Returns error if the label contains invalid JSON.
func (a *ImageArtifact) ParsedOpenAPISchema() (*openapi3.T, error) {
	raw := a.OpenAPISchema()
	if raw == "" {
		return nil, nil
	}

	loader := openapi3.NewLoader()
	schema, err := loader.LoadFromData([]byte(raw))
	if err != nil {
		return nil, err
	}
	return schema, nil
}

// ToModel converts the ImageArtifact to a Model by parsing its labels.
// Returns error if the image is not a valid Cog model or if labels contain invalid JSON.
func (a *ImageArtifact) ToModel() (*Model, error) {
	if !a.IsCogModel() {
		return nil, ErrNotCogModel
	}

	cfg, err := a.ParsedConfig()
	if err != nil {
		return nil, err
	}

	schema, err := a.ParsedOpenAPISchema()
	if err != nil {
		return nil, err
	}

	return &Model{
		Image:      a,
		Config:     cfg,
		Schema:     schema,
		CogVersion: a.CogVersion(),
	}, nil
}
