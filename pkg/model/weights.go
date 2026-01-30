// pkg/model/weights.go
package model

import "time"

// Media types for weights artifacts.
const (
	// MediaTypeWeightsManifest is the artifactType for weights manifests.
	MediaTypeWeightsManifest = "application/vnd.cog.weights.v1"
	// MediaTypeWeightsLayerGzip is for gzip-compressed weight layers.
	MediaTypeWeightsLayerGzip = "application/vnd.cog.weights.layer.v1+gzip"
	// MediaTypeWeightsLayerZstd is for zstd-compressed weight layers (future).
	MediaTypeWeightsLayerZstd = "application/vnd.cog.weights.layer.v1+zstd"
	// MediaTypeWeightsLayer is for uncompressed weight layers.
	MediaTypeWeightsLayer = "application/vnd.cog.weights.layer.v1"
)

// Annotation keys for weight file layers.
const (
	AnnotationWeightsName             = "vnd.cog.weights.name"
	AnnotationWeightsDest             = "vnd.cog.weights.dest"
	AnnotationWeightsSource           = "vnd.cog.weights.source"
	AnnotationWeightsDigestOriginal   = "vnd.cog.weights.digest.original"
	AnnotationWeightsSizeUncompressed = "vnd.cog.weights.size.uncompressed"
)

// WeightsManifest contains metadata about weight files in a model.
type WeightsManifest struct {
	// Digest is the manifest digest (sha256:...).
	Digest string
	// ArtifactType is the OCI artifact type (application/vnd.cog.weights.v1).
	ArtifactType string
	// Created is when the manifest was created.
	Created time.Time
	// Files are the individual weight files.
	Files []WeightFile
}

// TotalSize returns the sum of compressed sizes of all files.
func (wm *WeightsManifest) TotalSize() int64 {
	var total int64
	for _, f := range wm.Files {
		total += f.Size
	}
	return total
}

// TotalSizeUncompressed returns the sum of uncompressed sizes of all files.
func (wm *WeightsManifest) TotalSizeUncompressed() int64 {
	var total int64
	for _, f := range wm.Files {
		total += f.SizeUncompressed
	}
	return total
}

// FindByDest returns the WeightFile with the given destination path, or nil.
func (wm *WeightsManifest) FindByDest(dest string) *WeightFile {
	for i := range wm.Files {
		if wm.Files[i].Dest == dest {
			return &wm.Files[i]
		}
	}
	return nil
}

// WeightFile represents a single weight file entry.
type WeightFile struct {
	// Name is the original filename.
	Name string `json:"name"`
	// Dest is the mount path in the container (e.g., /cache/model.safetensors).
	Dest string `json:"dest"`
	// Source is the origin URL (e.g., hf://..., file://...).
	Source string `json:"source,omitempty"`
	// DigestOriginal is the SHA256 of the uncompressed file (canonical ID).
	DigestOriginal string `json:"digestOriginal"`
	// Digest is the SHA256 of the compressed blob (OCI layer ID).
	Digest string `json:"digest"`
	// Size is the compressed size in bytes.
	Size int64 `json:"size"`
	// SizeUncompressed is the original size in bytes.
	SizeUncompressed int64 `json:"sizeUncompressed"`
	// MediaType is the OCI layer media type (e.g., application/vnd.cog.weights.layer.v1+gzip).
	MediaType string `json:"mediaType"`
	// ContentType is the file's MIME type (e.g., application/octet-stream).
	// TODO: Determine content type handling based on declarative weights implementation.
	ContentType string `json:"contentType,omitempty"`
}
