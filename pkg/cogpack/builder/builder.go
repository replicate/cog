package builder

import (
	"context"

	"github.com/replicate/cog/pkg/cogpack/plan"
	"github.com/replicate/cog/pkg/cogpack/project"
	"github.com/replicate/cog/pkg/docker/command"
)

// Builder executes a generated Plan and produces a container image (or other
// artifact). For the MVP we only implement a BuildKit‐based builder that
// materialises the image locally. Future builders (remote cache, tarball,
// etc.) can satisfy the same interface.
//
// buildContextDir should point at the directory that will be mounted as the
// BuildKit Local("context") source. The tag is the resulting image reference
// (e.g. "my-image:latest").
//
// Build MUST be idempotent: invoking it twice with the same Plan must produce
// the same image bits (BuildKit will handle caching internally).
//
// Implementations are expected to respect the Platform specified on the Plan.
type Builder interface {
	Build(ctx context.Context, p *plan.Plan, buildConfig *BuildConfig) error
}

func NewBuilder(dockerCmd command.Command) Builder {
	return NewBuildKitBuilder(dockerCmd)
}

type BuildConfig struct {
	Source *project.SourceInfo

	ContextDir string
	Tag        string
}
