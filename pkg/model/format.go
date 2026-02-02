// pkg/model/format.go
package model

import "os"

// ModelImageFormat describes the OCI structure of a model.
type ModelImageFormat string

const (
	// FormatStandalone is a traditional single OCI image.
	// All code, dependencies, and weights are baked into one image.
	FormatStandalone ModelImageFormat = "standalone"

	// FormatBundle is an OCI Image Index containing multiple manifests:
	// - A runnable image (code, dependencies)
	// - A weights artifact (model weights, fetched separately)
	FormatBundle ModelImageFormat = "bundle"
)

// String returns the string representation of the format.
func (f ModelImageFormat) String() string {
	return string(f)
}

// IsValid returns true if the format is a known value.
func (f ModelImageFormat) IsValid() bool {
	return f == FormatStandalone || f == FormatBundle
}

// ImageFormatFromEnv returns the image format based on the COG_OCI_INDEX environment variable.
// Returns FormatBundle if COG_OCI_INDEX=1, otherwise FormatStandalone.
func ImageFormatFromEnv() ModelImageFormat {
	if os.Getenv("COG_OCI_INDEX") == "1" {
		return FormatBundle
	}
	return FormatStandalone
}
