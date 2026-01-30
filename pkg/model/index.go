// pkg/model/index.go
package model

// ModelFormat indicates the OCI structure of a model.
type ModelFormat string

const (
	// ModelFormatImage is a traditional single OCI image (v1).
	ModelFormatImage ModelFormat = "image"
	// ModelFormatIndex is an OCI Image Index containing image + weights (v2).
	ModelFormatIndex ModelFormat = "index"
)

// Index represents an OCI Image Index containing multiple manifests.
type Index struct {
	// Digest is the index digest (sha256:...).
	Digest string
	// Reference is the full image reference.
	Reference string
	// MediaType is typically application/vnd.oci.image.index.v1+json.
	MediaType string
	// Manifests are the child manifests in this index.
	Manifests []IndexManifest
}

// IndexManifest represents a single manifest within an index.
type IndexManifest struct {
	// Digest is the manifest digest.
	Digest string
	// MediaType is the manifest media type.
	MediaType string
	// Size is the manifest size in bytes.
	Size int64
	// Platform is the target platform (nil for artifacts).
	Platform *Platform
	// Annotations are OCI annotations on this manifest.
	Annotations map[string]string
	// Type is derived from platform/annotations (image or weights).
	Type ManifestType
}

// ManifestType identifies the type of manifest in an index.
type ManifestType string

const (
	// ManifestTypeImage is a runnable container image.
	ManifestTypeImage ManifestType = "image"
	// ManifestTypeWeights is a weights artifact.
	ManifestTypeWeights ManifestType = "weights"
)

// Annotation keys for weights manifests.
const (
	AnnotationReferenceType   = "vnd.cog.reference.type"
	AnnotationReferenceDigest = "vnd.cog.reference.digest"
)
