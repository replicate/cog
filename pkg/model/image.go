package model

import (
	"encoding/json"

	"github.com/getkin/kin-openapi/openapi3"

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
	Variant      string
}

// Label keys for Cog-specific metadata stored in image labels.
var (
	LabelConfig          = global.LabelNamespace + "config"
	LabelVersion         = global.LabelNamespace + "version"
	LabelOpenAPISchema   = global.LabelNamespace + "openapi_schema"
	LabelWeightsManifest = global.LabelNamespace + "r8_weights_manifest"
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

// ParsedConfig returns the parsed cog.yaml config from image labels.
// Returns nil without error if no config label is present.
// Returns error if the label contains invalid JSON.
func (i *Image) ParsedConfig() (*config.Config, error) {
	raw := i.Config()
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
func (i *Image) ParsedOpenAPISchema() (*openapi3.T, error) {
	raw := i.OpenAPISchema()
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

// ToModel converts the Image to a Model by parsing its labels.
// Returns error if the image is not a valid Cog model or if labels contain invalid JSON.
func (i *Image) ToModel() (*Model, error) {
	if !i.IsCogModel() {
		return nil, ErrNotCogModel
	}

	cfg, err := i.ParsedConfig()
	if err != nil {
		return nil, err
	}

	schema, err := i.ParsedOpenAPISchema()
	if err != nil {
		return nil, err
	}

	return &Model{
		Image:      i,
		Config:     cfg,
		Schema:     schema,
		CogVersion: i.CogVersion(),
	}, nil
}
