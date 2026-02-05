package model

import "context"

// Builder builds an artifact from a spec.
// Each builder handles one artifact type (image, weight, etc.).
type Builder interface {
	Build(ctx context.Context, spec ArtifactSpec) (Artifact, error)
}
