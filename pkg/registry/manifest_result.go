package registry

import "github.com/google/go-containerregistry/pkg/v1/types"

type ManifestResult struct {
	SchemaVersion int64
	MediaType     string

	Manifests []PlatformManifest
	Layers    []string
	Config    string
	Labels    map[string]string
}

func (m *ManifestResult) IsIndex() bool {
	return m.MediaType == string(types.OCIImageIndex) || m.MediaType == string(types.DockerManifestList)
}

func (m *ManifestResult) IsSinglePlatform() bool {
	return !m.IsIndex()
}
