package model

// WeightFile represents a single weight file entry in a weights lockfile or manifest.
// The Name field is an identifier/handle (like a Docker volume name), not a filename.
type WeightFile struct {
	// Name is the identifier/handle for this weight (e.g., "personaplex-7b-v1", "model-v42.5").
	// This is a logical name that maps to deployment blob metadata, not a file path.
	Name string `json:"name"`
	// Dest is the mount path in the container (e.g., /cache/model.safetensors).
	Dest string `json:"dest"`
	// DigestOriginal is the SHA256 of the uncompressed file (canonical ID).
	DigestOriginal string `json:"digestOriginal"`
	// Digest is the SHA256 of the compressed blob (OCI layer ID).
	Digest string `json:"digest"`
	// Size is the compressed size in bytes.
	Size int64 `json:"size"`
	// SizeUncompressed is the original size in bytes.
	SizeUncompressed int64 `json:"sizeUncompressed"`
	// MediaType is the OCI layer media type (e.g., application/vnd.cog.weight.layer.v1+gzip).
	MediaType string `json:"mediaType"`
	// ContentType is the file's MIME type (e.g., application/octet-stream).
	ContentType string `json:"contentType,omitempty"`
}
