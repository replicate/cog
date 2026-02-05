package model

import v1 "github.com/google/go-containerregistry/pkg/v1"

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

// ImageArtifact is a built container image.
// It implements Artifact.
type ImageArtifact struct {
	name       string
	descriptor v1.Descriptor

	// Reference is the docker image reference (for pushing via docker/containerd).
	Reference string
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
